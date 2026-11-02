# PosternDB setup for Windows (PowerShell 5.1+ / 7+).
#
# Safe to run as many times as you want. Every run:
#   1. Checks Docker (and starts Docker Desktop if it is closed).
#   2. Creates .env if missing and adds settings introduced by newer versions
#      of .env.example, without touching values you already set.
#   3. Generates secrets that are still empty (never overwrites existing ones)
#      and prints the panel credentials the first time they are generated.
#   4. Rebuilds the image and (re)starts every service, keeping all data.
#   5. Re-aligns the database password with .env if they drifted apart.
#   6. Waits until the app answers /health.
#
# Run:  powershell -ExecutionPolicy Bypass -File .\setup.ps1
$ErrorActionPreference = 'Stop'
Set-Location -Path $PSScriptRoot

$EnvFile = '.env'
$ExampleFile = '.env.example'
$DefaultPanelPort = '28437'
$AppContainerPort = '8080'
$HealthTimeout = 120
$DockerStartTimeout = 120
$RequiredVars = @('BASE_DOMAIN', 'CLOUDFLARE_API_TOKEN', 'CLOUDFLARE_ZONE_ID', 'CLOUDFLARE_TUNNEL_TOKEN')
$Utf8NoBom = New-Object System.Text.UTF8Encoding($false)

function Info($msg) { Write-Host "==> $msg" -ForegroundColor Cyan }
function Warn($msg) { Write-Host "!  $msg" -ForegroundColor Yellow }
function Fail($msg) { Write-Host "x  $msg" -ForegroundColor Red; exit 1 }

# Runs a native command and returns $true when it exits with code 0.
function Test-Native([scriptblock]$Command) {
    $prev = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try { & $Command *> $null; return ($LASTEXITCODE -eq 0) } finally { $ErrorActionPreference = $prev }
}

# --- 1. Docker ---------------------------------------------------------------
if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
    Fail 'Docker is not installed. Get Docker Desktop from https://docs.docker.com/get-docker/'
}
if (-not (Test-Native { docker info })) {
    $desktop = Join-Path $env:ProgramFiles 'Docker\Docker\Docker Desktop.exe'
    if (-not (Test-Path $desktop)) { Fail 'Docker is installed but not running. Start Docker Desktop and rerun setup.ps1.' }
    Info "Docker Desktop was not running; starting it (up to ${DockerStartTimeout}s)..."
    Start-Process -FilePath $desktop | Out-Null
    $deadline = (Get-Date).AddSeconds($DockerStartTimeout)
    while (-not (Test-Native { docker info })) {
        if ((Get-Date) -ge $deadline) { Fail "Docker Desktop did not start in ${DockerStartTimeout}s. Open it manually and rerun setup.ps1." }
        Start-Sleep -Seconds 2
    }
}
if (-not (Test-Native { docker compose version })) { Fail 'Docker Compose v2 is not available. Update Docker Desktop.' }

# --- 2. .env -----------------------------------------------------------------
if (-not (Test-Path $EnvFile)) {
    Copy-Item $ExampleFile $EnvFile
    Info "Created $EnvFile from $ExampleFile"
}
$EnvPath = Join-Path $PSScriptRoot $EnvFile

function Read-EnvLines { return @([System.IO.File]::ReadAllLines($EnvPath) | ForEach-Object { $_.TrimEnd("`r") }) }
function Write-EnvLines([string[]]$Lines) {
    # LF endings and UTF-8 without BOM, exactly what docker compose expects.
    [System.IO.File]::WriteAllText($EnvPath, (($Lines -join "`n") + "`n"), $Utf8NoBom)
}

# Normalise CRLF / BOM once; a no-op on later runs.
$rawEnv = [System.IO.File]::ReadAllText($EnvPath)
if ($rawEnv.Contains("`r") -or $rawEnv.StartsWith([char]0xFEFF)) {
    Write-EnvLines (Read-EnvLines | ForEach-Object { $_.TrimStart([char]0xFEFF) })
    Info "Converted $EnvFile to Unix line endings"
}

function Test-Var([string]$Key) { return [bool](Read-EnvLines | Where-Object { $_ -match "^$Key=" }) }

function Get-Var([string]$Key) {
    $line = Read-EnvLines | Where-Object { $_ -match "^$Key=" } | Select-Object -Last 1
    if (-not $line) { return '' }
    return ($line.Substring($Key.Length + 1)).Trim('"')
}

function Set-Var([string]$Key, [string]$Value) {
    $found = $false
    $out = foreach ($l in Read-EnvLines) {
        if ($l -match "^$Key=") { $found = $true; "$Key=$Value" } else { $l }
    }
    if (-not $found) { $out = @($out) + "$Key=$Value" }
    Write-EnvLines ([string[]]$out)
}

# Settings added to .env.example by newer versions are appended with their
# default value; existing settings are never modified.
foreach ($line in [System.IO.File]::ReadAllLines((Join-Path $PSScriptRoot $ExampleFile))) {
    if ($line -match '^([A-Z][A-Z0-9_]*)=(.*)$' -and -not (Test-Var $Matches[1])) {
        $key = $Matches[1]
        Set-Var $key $Matches[2]
        Info "Added new setting $key to $EnvFile"
    }
}

function New-RandomString([int]$Length) {
    $chars = 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789'.ToCharArray()
    $rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
    $bytes = New-Object byte[] 1
    $sb = New-Object System.Text.StringBuilder
    while ($sb.Length -lt $Length) {
        $rng.GetBytes($bytes)
        # Rejection sampling: 248 = 62 * 4, avoids modulo bias.
        if ($bytes[0] -lt 248) { [void]$sb.Append($chars[$bytes[0] % 62]) }
    }
    return $sb.ToString()
}

# --- 3. generated secrets (only when empty) -----------------------------------
$newCredentials = $false
if (-not (Get-Var 'ADMIN_USER')) { Set-Var 'ADMIN_USER' ("admin-" + (New-RandomString 6).ToLower()); $newCredentials = $true }
if (-not (Get-Var 'ADMIN_PASSWORD')) { Set-Var 'ADMIN_PASSWORD' (New-RandomString 24); $newCredentials = $true }
if (-not (Get-Var 'JWT_SECRET')) { Set-Var 'JWT_SECRET' (New-RandomString 64) }
if (-not (Get-Var 'POSTGRES_PASSWORD')) { Set-Var 'POSTGRES_PASSWORD' (New-RandomString 32) }
if (-not (Get-Var 'PANEL_LOCAL_PORT')) { Set-Var 'PANEL_LOCAL_PORT' $DefaultPanelPort }

if ($newCredentials) {
    Write-Host ''
    Write-Host '  Panel credentials (also saved in .env)' -ForegroundColor Green
    Write-Host '  ----------------------------------------------------------'
    Write-Host ("  User:     " + (Get-Var 'ADMIN_USER'))
    Write-Host ("  Password: " + (Get-Var 'ADMIN_PASSWORD'))
    Write-Host '  ----------------------------------------------------------'
    Write-Host ''
}

# --- 4. required values ------------------------------------------------------
$localMode = (Get-Var 'LOCAL_MODE').ToLower() -eq 'true'
$composeFiles = @()
$services = @('app', 'tunnel', 'backup')
if ($localMode) {
    # No Cloudflare: publish Postgres on 127.0.0.1 and never start the tunnel.
    $RequiredVars = @()
    $composeFiles = @('-f', 'docker-compose.yml', '-f', 'docker-compose.local.yml')
    $services = @('app', 'backup')
    Info ("Local mode: no Cloudflare; databases on 127.0.0.1:" + (Get-Var 'LOCAL_DB_PORT'))
}

$missing = $RequiredVars | Where-Object { -not (Get-Var $_) }
if ($missing) {
    Warn "Fill in these variables in $EnvFile, then run setup.ps1 again:"
    $missing | ForEach-Object { Write-Host "     - $_" }
    Warn "Each one is explained (with where to find it) in $EnvFile."
    exit 1
}

$port = Get-Var 'PANEL_LOCAL_PORT'
$portNumber = 0
if (-not [int]::TryParse($port, [ref]$portNumber) -or $portNumber -lt 1024 -or $portNumber -gt 65535) {
    Fail "PANEL_LOCAL_PORT must be a number between 1024 and 65535 (got '$port')."
}

# --- 5. port check -----------------------------------------------------------
function Test-PortOpen([int]$Port) {
    $client = New-Object System.Net.Sockets.TcpClient
    try {
        $async = $client.BeginConnect('127.0.0.1', $Port, $null, $null)
        return ($async.AsyncWaitHandle.WaitOne(500) -and $client.Connected)
    } catch { return $false } finally { $client.Close() }
}

function Test-OurAppOwnsPort([int]$Port) {
    $prev = $ErrorActionPreference; $ErrorActionPreference = 'Continue'
    try {
        $id = docker compose @composeFiles ps -q app 2>$null
        if (-not $id) { return $false }
        $mapping = docker compose @composeFiles port app $AppContainerPort 2>$null
        return ($mapping -match ":$Port$")
    } finally { $ErrorActionPreference = $prev }
}

if ((Test-PortOpen $portNumber) -and -not (Test-OurAppOwnsPort $portNumber)) {
    Fail "Port $portNumber is already used by another program. Set PANEL_LOCAL_PORT in $EnvFile to a free port and rerun setup.ps1."
}

# --- 6. build and start ------------------------------------------------------
function Wait-Healthy([string]$Service, [int]$TimeoutSeconds) {
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    while ((Get-Date) -lt $deadline) {
        $prev = $ErrorActionPreference; $ErrorActionPreference = 'Continue'
        $id = docker compose @composeFiles ps -q $Service 2>$null
        $status = if ($id) { docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' $id 2>$null } else { 'starting' }
        $ErrorActionPreference = $prev
        if ($status -eq 'healthy') { return $true }
        Start-Sleep -Seconds 2
    }
    return $false
}

function Test-DbPassword {
    $pw = Get-Var 'POSTGRES_PASSWORD'
    $user = Get-Var 'POSTGRES_USER'
    return (Test-Native { docker compose @composeFiles exec -T -e "PGPASSWORD=$pw" db psql -h 127.0.0.1 -U $user -d postgres -Atqc 'SELECT 1' })
}

# PostgreSQL applies POSTGRES_USER / POSTGRES_PASSWORD only when the data
# volume is created. If .env was regenerated or edited later, the app would be
# locked out. Single-user mode bypasses authentication (it needs direct
# access to the data directory), so the stored role is updated to match .env.
function Sync-DbPassword {
    if (Test-DbPassword) { return }
    Warn 'The database password differs from .env (was .env regenerated?). Updating the database to match .env...'
    $raw = Get-Var 'POSTGRES_USER'
    $ident = $raw.Replace('"', '""')
    $lit = $raw.Replace("'", "''")
    $pass = (Get-Var 'POSTGRES_PASSWORD').Replace("'", "''")
    $sql = 'DO $sync$ BEGIN IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = ''{0}'') THEN ALTER ROLE "{1}" WITH LOGIN SUPERUSER PASSWORD ''{2}''; ELSE CREATE ROLE "{1}" WITH LOGIN SUPERUSER PASSWORD ''{2}''; END IF; END $sync$;' -f $lit, $ident, $pass
    docker compose @composeFiles stop db | Out-Null
    $prev = $ErrorActionPreference; $ErrorActionPreference = 'Continue'
    ($sql + "`n") | docker compose @composeFiles run --rm --no-deps -T -u postgres --entrypoint sh db -c 'exec postgres --single -D "$PGDATA" postgres' *> $null
    $ErrorActionPreference = $prev
    docker compose @composeFiles up -d db | Out-Null
    if (-not (Wait-Healthy 'db' 60)) { Fail 'The database did not come back after updating its password. See: docker compose logs db' }
    if (-not (Test-DbPassword)) { Fail 'Could not align the database password with .env. See: docker compose logs db' }
    Info 'Database password updated.'
}

Info 'Building the image...'
docker compose @composeFiles build app
if ($LASTEXITCODE -ne 0) { Fail 'Image build failed.' }

Info 'Starting the database...'
docker compose @composeFiles up -d --remove-orphans db
if ($LASTEXITCODE -ne 0) { Fail 'Could not start the database.' }
if (-not (Wait-Healthy 'db' 90)) { Fail 'The database did not become healthy. See: docker compose logs db' }
Sync-DbPassword

Info 'Starting PosternDB...'
# --force-recreate guarantees every container picks up the current .env and
# the freshly built image; volumes (data, backups) are never touched.
function Show-AppFailure {
    Write-Host ''
    docker compose @composeFiles logs --tail 20 --no-log-prefix app
    Fail "PosternDB did not start. The reason is in the log lines above (usually a wrong value in $EnvFile). Fix it and rerun setup.ps1."
}

if ($localMode) { docker compose @composeFiles rm -sf tunnel *> $null }
docker compose @composeFiles up -d --remove-orphans --force-recreate @services
if ($LASTEXITCODE -ne 0) { Show-AppFailure }

$url = "http://127.0.0.1:$portNumber/health"
Info "Waiting for $url (up to ${HealthTimeout}s)..."
$deadline = (Get-Date).AddSeconds($HealthTimeout)
while ($true) {
    try {
        $r = Invoke-WebRequest -Uri $url -UseBasicParsing -TimeoutSec 3
        if ($r.StatusCode -eq 200) { break }
    } catch { }
    if ((Get-Date) -ge $deadline) { Show-AppFailure }
    Write-Host -NoNewline '.'
    Start-Sleep -Seconds 2
}
Write-Host ''

$panel = Get-Var 'PANEL_HOSTNAME'; if (-not $panel) { $panel = "postern." + (Get-Var 'BASE_DOMAIN') }
Write-Host 'PosternDB is running.' -ForegroundColor Green
if ($localMode) { Write-Host '  Panel (via Cloudflare): disabled (LOCAL_MODE=true)' }
else { Write-Host "  Panel (via Cloudflare): https://$panel   (DNS may take a minute)" }
Write-Host "  Panel (this machine):   http://127.0.0.1:$portNumber"
Write-Host '  Credentials:            ADMIN_USER / ADMIN_PASSWORD in .env'
Write-Host '  Logs:                   docker compose logs -f app'
Write-Host ''
Write-Host '  Run setup.ps1 again any time to rebuild and restart; your data is kept.'
