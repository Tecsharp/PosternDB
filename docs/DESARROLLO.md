# Guía técnica

Detalles para quien desarrolla o audita PosternDB. Para instalarlo y usarlo, lee el [README](../README.md).

## Seguridad

**El puerto 5432 nunca se expone** (salvo en modo local, solo en loopback). El servicio `db` no tiene `ports:` en `docker-compose.yml` y vive en una red Docker `internal: true`, sin salida a internet. Solo lo alcanzan la app, el sidecar de respaldos y `cloudflared`. No hay ninguna regla de firewall que mantener ni un Postgres expuesto a escáneres.

**Por qué el cliente necesita `cloudflared`.** El túnel es saliente: `cloudflared` abre la conexión hacia Cloudflare desde tu servidor. Un cliente no puede abrir un TCP directo a un host de Cloudflare, así que `cloudflared access tcp` en su máquina envuelve el tráfico de PostgreSQL en una conexión TLS hasta el borde de Cloudflare, y el túnel la entrega a `db:5432`. El tráfico va cifrado de punta a punta sin configurar SSL en Postgres.

Otras medidas:

| Medida | Dónde |
|---|---|
| `scram-sha-256` para todas las conexiones, incluido el socket local | `docker-compose.yml` |
| La contraseña del cliente nunca llega al servidor: se envía un verificador SCRAM calculado en Go | `internal/credentials/scram.go` |
| Un rol por cliente: `NOSUPERUSER NOCREATEDB NOCREATEROLE`, límite de 20 conexiones, dueño solo de su base | `internal/store/cluster.go` |
| `REVOKE CONNECT ... FROM PUBLIC` en cada base de cliente y en `postgres`, `template1` y la base de la plataforma: un cliente no puede entrar a la base de otro | `Cluster.Harden`, `CreateDatabase` |
| Nombres validados con lista blanca (`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`), con prefijo fijo y además entrecomillados | `internal/naming` |
| Las contraseñas generadas no se guardan; se muestran una vez con `Cache-Control: no-store` | `httpapi.createDatabase` |
| Sesión JWT HS256 con algoritmo fijado, cookie `HttpOnly` + `SameSite=Strict` (+ `Secure` detrás de Cloudflare) | `internal/auth` |
| Anti-CSRF: cabecera `X-Requested-With` obligatoria en peticiones que modifican estado | `auth.RequireCSRFHeader` |
| Rate limiting del login por IP (5/min) y comparación de credenciales en tiempo constante | `auth/ratelimit.go` |
| CSP estricta (`default-src 'self'`), `X-Frame-Options: DENY`, `nosniff` | `httpapi.securityHeaders` |
| Los tokens de Cloudflare no se registran nunca; `TunnelCredentials.String()` los oculta | `internal/config` |
| Consola SQL con el rol `posterndb_console`, que **no** es superusuario y entra con `SET ROLE` al dueño de la base. Transacción `READ ONLY`, una sentencia por petición (protocolo extendido), `statement_timeout` de 10 s. Los tests de integración verifican que no puede escribir, ni usar `COPY ... TO PROGRAM` o `pg_read_file`, ni volverse superusuario, ni entrar a otra base | `internal/store/inspector.go` |
| La contraseña del rol de consola se genera en cada arranque y solo vive en memoria | `cmd/posterndb/main.go` |
| Abrir una base en el panel no cuenta como actividad: esas sesiones se descuentan del auditor | `auditor.refreshActivity` |
| Administradores con bcrypt (coste 12), roles verificados en cada petición, tiempo constante también para usuarios inexistentes | `internal/admins` |
| Descarga de respaldos solo por id de un trabajo terminado; el nombre de archivo se revalida para que no pueda salir del directorio | `provision.BackupPath` |
| Imagen distroless sin shell, usuario `nonroot` | `Dockerfile` |
| Secretos solo en `.env` (excluido de git y de la imagen) | `.gitignore`, `.dockerignore` |

> Todos los clientes comparten el mismo Postgres y el mismo destino del túnel. El hostname solo enruta; **el aislamiento lo dan los roles y los permisos**. Si necesitas una capa más, añade una política de Cloudflare Access a los hostnames de clientes.

## Tests

No hace falta tener Go instalado; todo puede correr en Docker.

Tests unitarios (lógica pura, handlers con `fakeCloudflareClient`, sin red):

```bash
go test ./...
```

Tests unitarios y de integración (testcontainers levanta PostgreSQL 17 real) con cobertura:

```bash
go test -tags=integration -coverprofile=coverage.out -covermode=atomic ./...
```

Lo mismo sin Go local (macOS/Linux con Docker Desktop):

```bash
docker run --rm -v "$PWD":/src -v /var/run/docker.sock:/var/run/docker.sock -w /src -e TESTCONTAINERS_RYUK_DISABLED=true -e TESTCONTAINERS_HOST_OVERRIDE=host.docker.internal golang:1.27-alpine go test -tags=integration -coverprofile=coverage.out -covermode=atomic ./...
```

Los de integración cubren:

- la creación de base y rol, con login real usando el verificador SCRAM;
- el aislamiento entre clientes;
- el explorador de esquemas;
- la consola, incluidas las pruebas de seguridad;
- los usuarios extra de lectura y escritura;
- el archivado y la eliminación;
- `pg_stat_activity` / `pg_stat_database`;
- todos los repositorios.

La cobertura actual es del 95 %, excluyendo `main.go` y los fakes.

Frontend:

```bash
npm --prefix web run typecheck
```

Panel con datos falsos, sin Postgres ni Cloudflare (usuario `admin`, contraseña `admin`):

```bash
npm --prefix web run dev:mock
```

## SonarQube

`sonar-project.properties` ya apunta a `coverage.out` (`sonar.go.coverage.reportPaths`) y excluye `main.go`, `testsupport` y el build del frontend.

1. Levanta SonarQube:

```bash
docker run -d --name sonarqube -p 9000:9000 sonarqube:community
```

2. Abre `http://localhost:9000`, crea un proyecto local `posterndb` y genera un token.
3. Genera la cobertura (ver [Tests](#tests)) y analiza:

```bash
docker run --rm --network host -e SONAR_HOST_URL=http://localhost:9000 -e SONAR_TOKEN=TU_TOKEN -v "$PWD":/usr/src sonarsource/sonar-scanner-cli
```

## Estructura del proyecto

```
cmd/posterndb/main.go        wiring y subcomandos (serve, healthcheck, migrate-hostnames)
internal/config/             constantes (go:S1192) y carga/validación de .env
internal/naming/             validación de nombres y derivación de identificadores y hostnames
internal/credentials/        contraseñas aleatorias y verificador SCRAM-SHA-256
internal/expiry/             cálculo puro de expiración y estado
internal/store/              migraciones, metadatos, operaciones del clúster e inspector (esquema, consola, permisos)
internal/cloudflare/         DNS CNAME + reglas de ingress del túnel (cloudflare-go)
internal/provision/          orquestador: crear (saga), clonar/restaurar/plantillas, usuarios, expiración, respaldos
internal/auditor/            goroutine de auditoría de inactividad
internal/auth/               JWT, cookie de sesión, roles, CSRF, rate limiting
internal/admins/             administradores del panel (bcrypt, roles)
internal/notify/             avisos por webhook y SMTP
internal/httpapi/            router chi, handlers JSON, SPA embebida
internal/testsupport/        fakes en memoria para los tests
web/                         panel React + Vite + Tailwind (embebido con go:embed)
backup/backup.sh             sidecar: cola de trabajos pg_dump / pg_restore
docker-compose.local.yml     override del modo local (Postgres en loopback, sin túnel)
setup.sh / setup.ps1         primer arranque automatizado
```
