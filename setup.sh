#!/usr/bin/env bash
# PosternDB setup for macOS / Linux.
#
# Safe to run as many times as you want. Every run:
#   1. Checks Docker (and starts Docker Desktop on macOS if it is closed).
#   2. Creates .env if missing and keeps it organized in the sections of
#      .env.example, adding new settings without touching your values.
#   3. Generates secrets that are still empty (never overwrites existing ones)
#      and prints the panel credentials the first time they are generated.
#   4. Rebuilds the image and (re)starts every service, keeping all data.
#   5. Re-aligns the database password with .env if they drifted apart.
#   6. Waits until the app answers /health.
set -euo pipefail

cd "$(dirname "$0")"

ENV_FILE=.env
EXAMPLE_FILE=.env.example
DEFAULT_PANEL_PORT=28437
APP_CONTAINER_PORT=8080
HEALTH_TIMEOUT=120
DOCKER_START_TIMEOUT=120
REQUIRED_VARS=(BASE_DOMAIN CLOUDFLARE_API_TOKEN CLOUDFLARE_ZONE_ID CLOUDFLARE_TUNNEL_TOKEN)

bold=$'\033[1m'; green=$'\033[32m'; red=$'\033[31m'; yellow=$'\033[33m'; reset=$'\033[0m'
info()  { printf '%s\n' "${bold}==>${reset} $*"; }
warn()  { printf '%s\n' "${yellow}!${reset}  $*"; }
fail()  { printf '%s\n' "${red}✗${reset}  $*" >&2; exit 1; }

# --- 1. Docker ---------------------------------------------------------------
ensure_docker() {
  command -v docker >/dev/null 2>&1 || fail "Docker is not installed. Get it from https://docs.docker.com/get-docker/"
  docker info >/dev/null 2>&1 && return 0

  if [[ "$(uname -s)" == "Darwin" ]] && open -a Docker 2>/dev/null; then
    info "Docker Desktop was not running; starting it (up to ${DOCKER_START_TIMEOUT}s)…"
    local deadline=$((SECONDS + DOCKER_START_TIMEOUT))
    until docker info >/dev/null 2>&1; do
      (( SECONDS < deadline )) || fail "Docker Desktop did not start in ${DOCKER_START_TIMEOUT}s. Open it manually and rerun ./setup.sh."
      sleep 2
    done
    return 0
  fi
  fail "Docker is installed but not running. Start it (Linux: sudo systemctl start docker) and rerun ./setup.sh."
}

ensure_docker
if docker compose version >/dev/null 2>&1; then
  COMPOSE=(docker compose)
elif command -v docker-compose >/dev/null 2>&1; then
  COMPOSE=(docker-compose)
else
  fail "Docker Compose is not available. Install the Compose plugin: https://docs.docker.com/compose/install/"
fi
command -v curl >/dev/null 2>&1 || fail "curl is required to check /health. Install it and rerun ./setup.sh."

# --- 2. .env -----------------------------------------------------------------
if [[ ! -f "$ENV_FILE" ]]; then
  cp "$EXAMPLE_FILE" "$ENV_FILE"
  info "Created $ENV_FILE from $EXAMPLE_FILE"
fi
chmod 600 "$ENV_FILE" 2>/dev/null || true

# A .env edited on Windows has CRLF endings, which would leak "\r" into every
# value (and into passwords). Normalise to LF; a no-op on later runs.
if grep -q $'\r' "$ENV_FILE"; then
  tmp=$(mktemp); tr -d '\r' <"$ENV_FILE" >"$tmp"; cat "$tmp" >"$ENV_FILE"; rm -f "$tmp"
  info "Converted $ENV_FILE to Unix line endings"
fi

get_var() {
  # Last assignment wins, like docker compose. Strips surrounding quotes.
  local line
  line=$(grep -E "^$1=" "$ENV_FILE" | tail -n 1 || true)
  line=${line#*=}
  # A stray space after the value ("LOCAL_MODE=true ") must not change its meaning.
  line=${line#"${line%%[![:space:]]*}"}; line=${line%"${line##*[![:space:]]}"}
  line=${line%\"}; line=${line#\"}
  printf '%s' "$line"
}

has_var() { grep -qE "^$1=" "$ENV_FILE"; }

set_var() {
  local key=$1 value=$2 tmp
  tmp=$(mktemp)
  if has_var "$key"; then
    awk -v k="$key" -v v="$value" 'BEGIN{FS=OFS="="} $1==k {print k "=" v; next} {print}' "$ENV_FILE" >"$tmp"
  else
    cat "$ENV_FILE" >"$tmp"
    [[ -z "$(tail -c 1 "$ENV_FILE")" ]] || printf '\n' >>"$tmp"
    printf '%s=%s\n' "$key" "$value" >>"$tmp"
  fi
  cat "$tmp" >"$ENV_FILE"   # keep original permissions
  rm -f "$tmp"
}

# The last assignment of a key, exactly as written (no quote stripping).
raw_var() { grep -E "^$1=" "$ENV_FILE" | tail -n 1 | cut -d= -f2-; }

# Rebuilds .env with the layout of .env.example (sections and comments), so
# new settings land in their section instead of piling up at the end. Every
# value you already set is kept byte for byte; settings the example does not
# know go to a final block. The previous file is saved as .env.bak.
organize_env() {
  local tmp line key known=" " extras=()
  tmp=$(mktemp)
  while IFS= read -r line || [[ -n "$line" ]]; do
    if [[ "$line" =~ ^([A-Z][A-Z0-9_]*)= ]]; then
      key=${BASH_REMATCH[1]}
      known+="$key "
      if has_var "$key"; then
        printf '%s=%s\n' "$key" "$(raw_var "$key")" >>"$tmp"
      else
        printf '%s\n' "$line" >>"$tmp"
        info "Added new setting $key to $ENV_FILE"
      fi
    else
      printf '%s\n' "$line" >>"$tmp"
    fi
  done <"$EXAMPLE_FILE"

  while IFS= read -r key; do
    [[ "$known" == *" $key "* ]] || extras+=("$key")
  done < <(grep -oE '^[A-Z][A-Z0-9_]*=' "$ENV_FILE" | tr -d '=' | awk '!seen[$0]++')
  if (( ${#extras[@]} > 0 )); then
    printf '\n# -----------------------------------------------------------------------------\n#  OTRAS VARIABLES (no vienen en .env.example)\n# -----------------------------------------------------------------------------\n' >>"$tmp"
    for key in "${extras[@]}"; do printf '%s=%s\n' "$key" "$(raw_var "$key")" >>"$tmp"; done
  fi

  if ! cmp -s "$tmp" "$ENV_FILE"; then
    cp -p "$ENV_FILE" "$ENV_FILE.bak"
    cat "$tmp" >"$ENV_FILE"   # keep original permissions
    info "Organized $ENV_FILE into sections (previous copy: $ENV_FILE.bak)"
  fi
  rm -f "$tmp"
}
organize_env

random_string() {
  # Reads a fixed number of bytes first so pipefail never trips on SIGPIPE.
  head -c 1024 /dev/urandom | LC_ALL=C tr -dc 'A-Za-z0-9' | cut -c "1-$1"
}

# --- 3. generated secrets (only when empty) -----------------------------------
NEW_CREDENTIALS=false
if [[ -z "$(get_var ADMIN_USER)" ]]; then
  set_var ADMIN_USER "admin-$(random_string 6 | tr 'A-Z' 'a-z')"
  NEW_CREDENTIALS=true
fi
if [[ -z "$(get_var ADMIN_PASSWORD)" ]]; then
  set_var ADMIN_PASSWORD "$(random_string 24)"
  NEW_CREDENTIALS=true
fi
[[ -n "$(get_var JWT_SECRET)" ]] || set_var JWT_SECRET "$(random_string 64)"
[[ -n "$(get_var POSTGRES_PASSWORD)" ]] || set_var POSTGRES_PASSWORD "$(random_string 32)"
[[ -n "$(get_var PANEL_LOCAL_PORT)" ]] || set_var PANEL_LOCAL_PORT "$DEFAULT_PANEL_PORT"

if $NEW_CREDENTIALS; then
  cat <<EOF

${green}${bold}  Panel credentials (also saved in .env)${reset}
  ----------------------------------------------------------
  User:     ${bold}$(get_var ADMIN_USER)${reset}
  Password: ${bold}$(get_var ADMIN_PASSWORD)${reset}
  ----------------------------------------------------------

EOF
fi

# --- 4. required values ------------------------------------------------------
LOCAL_MODE=$(get_var LOCAL_MODE | tr 'A-Z' 'a-z')
if [[ "$LOCAL_MODE" == "true" ]]; then
  # No Cloudflare: publish Postgres on 127.0.0.1 and never start the tunnel.
  REQUIRED_VARS=()
  COMPOSE+=(-f docker-compose.yml -f docker-compose.local.yml)
  SERVICES=(app backup)
  info "Local mode: no Cloudflare; databases on 127.0.0.1:$(get_var LOCAL_DB_PORT)"
else
  SERVICES=(app tunnel backup)
fi

missing=()
for v in ${REQUIRED_VARS[@]+"${REQUIRED_VARS[@]}"}; do
  [[ -n "$(get_var "$v")" ]] || missing+=("$v")
done
if (( ${#missing[@]} > 0 )); then
  warn "Fill in these variables in $ENV_FILE, then run ./setup.sh again:"
  for v in "${missing[@]}"; do printf '     - %s\n' "$v"; done
  warn "Each one is explained (with where to find it) in $ENV_FILE."
  exit 1
fi

PORT=$(get_var PANEL_LOCAL_PORT)
[[ "$PORT" =~ ^[0-9]+$ ]] && (( PORT >= 1024 && PORT <= 65535 )) || fail "PANEL_LOCAL_PORT must be a number between 1024 and 65535 (got '$PORT')."

# --- 5. port check -----------------------------------------------------------
port_is_open() { (exec 3<>"/dev/tcp/127.0.0.1/$1") 2>/dev/null; }

our_app_owns_port() {
  [[ -n "$("${COMPOSE[@]}" ps -q app 2>/dev/null)" ]] &&
    "${COMPOSE[@]}" port app "$APP_CONTAINER_PORT" 2>/dev/null | grep -q ":$1\$"
}

if port_is_open "$PORT" && ! our_app_owns_port "$PORT"; then
  fail "Port $PORT is already used by another program. Set PANEL_LOCAL_PORT in $ENV_FILE to a free port and rerun ./setup.sh."
fi

# --- 6. build and start ------------------------------------------------------
wait_healthy() {
  local service=$1 id status deadline=$((SECONDS + $2))
  while :; do
    id=$("${COMPOSE[@]}" ps -q "$service" 2>/dev/null || true)
    status=$([[ -n "$id" ]] && docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$id" 2>/dev/null || echo starting)
    [[ "$status" == "healthy" ]] && return 0
    (( SECONDS < deadline )) || return 1
    sleep 2
  done
}

db_password_matches() {
  "${COMPOSE[@]}" exec -T -e PGPASSWORD="$(get_var POSTGRES_PASSWORD)" db \
    psql -h 127.0.0.1 -U "$(get_var POSTGRES_USER)" -d postgres -Atqc 'SELECT 1' >/dev/null 2>&1
}

# PostgreSQL applies POSTGRES_USER / POSTGRES_PASSWORD only when the data
# volume is created. If .env was regenerated or edited later, the app would be
# locked out. Single-user mode bypasses authentication (it needs direct
# access to the data directory), so the stored role is updated to match .env.
sync_db_password() {
  db_password_matches && return 0
  warn "The database password differs from .env (was .env regenerated?). Updating the database to match .env…"
  local raw ident lit pass
  raw=$(get_var POSTGRES_USER)
  ident=${raw//\"/\"\"}            # identifier: double the double quotes
  lit=$(printf '%s' "$raw" | sed "s/'/''/g")
  pass=$(get_var POSTGRES_PASSWORD | sed "s/'/''/g")
  "${COMPOSE[@]}" stop db >/dev/null
  # Quoted heredoc: no shell expansion of the $sync$ dollar quotes.
  local fmt
  fmt=$(cat <<'SQL'
DO $sync$ BEGIN IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = '%s') THEN ALTER ROLE "%s" WITH LOGIN SUPERUSER PASSWORD '%s'; ELSE CREATE ROLE "%s" WITH LOGIN SUPERUSER PASSWORD '%s'; END IF; END $sync$;
SQL
)
  # shellcheck disable=SC2059 # fmt is a constant template, values are arguments
  printf "$fmt\n" \
    "$lit" "$ident" "$pass" "$ident" "$pass" |
    "${COMPOSE[@]}" run --rm --no-deps -T -u postgres --entrypoint sh db \
      -c 'exec postgres --single -D "$PGDATA" postgres' >/dev/null
  "${COMPOSE[@]}" up -d db >/dev/null
  wait_healthy db 60 || fail "The database did not come back after updating its password. See: ${COMPOSE[*]} logs db"
  db_password_matches || fail "Could not align the database password with .env. See: ${COMPOSE[*]} logs db"
  info "Database password updated."
}

info "Building the image…"
"${COMPOSE[@]}" build app

info "Starting the database…"
"${COMPOSE[@]}" up -d --remove-orphans db
wait_healthy db 90 || fail "The database did not become healthy. See: ${COMPOSE[*]} logs db"
sync_db_password

info "Starting PosternDB…"
# --force-recreate guarantees every container picks up the current .env and
# the freshly built image; volumes (data, backups) are never touched.
show_app_failure() {
  printf '\n'
  "${COMPOSE[@]}" logs --tail 20 --no-log-prefix app >&2 || true
  fail "PosternDB did not start. The reason is in the log lines above (usually a wrong value in $ENV_FILE). Fix it and rerun ./setup.sh."
}

if [[ "$LOCAL_MODE" == "true" ]]; then
  "${COMPOSE[@]}" rm -sf tunnel >/dev/null 2>&1 || true
fi
"${COMPOSE[@]}" up -d --remove-orphans --force-recreate "${SERVICES[@]}" || show_app_failure

URL="http://127.0.0.1:${PORT}/health"
info "Waiting for $URL (up to ${HEALTH_TIMEOUT}s)…"
deadline=$((SECONDS + HEALTH_TIMEOUT))
until curl -fsS "$URL" >/dev/null 2>&1; do
  (( SECONDS < deadline )) || show_app_failure
  printf '.'
  sleep 2
done
printf '\n'

PANEL=$(get_var PANEL_HOSTNAME); PANEL=${PANEL:-postern.$(get_var BASE_DOMAIN)}
if [[ "$LOCAL_MODE" == "true" ]]; then
  PUBLIC="Panel (via Cloudflare): disabled (LOCAL_MODE=true)"
else
  PUBLIC="Panel (via Cloudflare): ${bold}https://${PANEL}${reset}   (DNS may take a minute)"
fi
cat <<EOF
${green}✓${reset} PosternDB is running.

  ${PUBLIC}
  Panel (this machine):   http://127.0.0.1:${PORT}
  Credentials:            ADMIN_USER / ADMIN_PASSWORD in .env
  Logs:                   ${COMPOSE[*]} logs -f app

  Run ./setup.sh again any time to rebuild and restart; your data is kept.
EOF
