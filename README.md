# PosternDB

Plataforma de bases de datos como servicio (DBaaS) autoalojable. Levanta PostgreSQL en Docker, crea una base de datos por cliente desde un panel minimalista y la publica a través de **Cloudflare Tunnel**, sin abrir puertos en tu servidor.

- Una base de datos y un rol de mínimo privilegio por cliente, más usuarios extra de solo lectura o lectura/escritura.
- Un subdominio por cliente, creado automáticamente en Cloudflare (DNS + regla del túnel), o **modo local** sin Cloudflare.
- Explorador de esquemas: árbol de tablas y columnas, **diagrama entidad-relación** (al estilo MySQL Workbench), conteo de registros y vista previa.
- Consola SQL de solo lectura, métricas por base (tamaño, conexiones, sesiones, tablas más grandes).
- Respaldos manuales, descarga, restauración como base nueva, clonación y plantillas SQL.
- Expiración por inactividad configurable por base (nunca expira, días propios, prórroga) con avisos previos por webhook (Discord/Slack) o email.
- Varios administradores con roles: propietario, administrador, lector.
- Panel con la identidad de PachisLab, modo claro/oscuro y accesible por teclado (WCAG 2.2 AA).
- Un solo binario Go (imagen distroless) que sirve la API y el panel.

---

## Índice

1. [Requisitos](#requisitos)
2. [Despliegue en 3 pasos](#despliegue-en-3-pasos)
3. [Obtener los valores de Cloudflare](#obtener-los-valores-de-cloudflare)
4. [Esquema de hostnames y migración de dominio](#esquema-de-hostnames-y-migración-de-dominio)
5. [Conectar tu base de datos](#conectar-tu-base-de-datos)
6. [Funciones del panel](#funciones-del-panel)
7. [Modo local](#modo-local)
8. [Seguridad](#seguridad)
9. [Expiración por inactividad y respaldos](#expiración-por-inactividad-y-respaldos)
10. [Tests](#tests)
11. [SonarQube](#sonarqube)
12. [Estructura del proyecto](#estructura-del-proyecto)

---

## Requisitos

- Docker 24+ con Docker Compose v2 (Docker Desktop en macOS/Windows).
- Un dominio gestionado por Cloudflare (plan gratuito suficiente).
- `curl` (macOS/Linux) para que el setup compruebe `/health`.

## Despliegue en 3 pasos

```bash
git clone git@github.com:Tecsharp/PosternDB.git
```

```bash
cd PosternDB && ./setup.sh
```

En Windows (PowerShell):

```powershell
powershell -ExecutionPolicy Bypass -File .\setup.ps1
```

1. La primera ejecución crea `.env`, genera `ADMIN_USER`, `ADMIN_PASSWORD`, `JWT_SECRET` y `POSTGRES_PASSWORD`, **muestra el usuario y la contraseña del panel** y te dice qué falta.
2. Rellena en `.env` las cuatro variables obligatorias:

   | Variable | Ejemplo |
   |---|---|
   | `BASE_DOMAIN` | `pachislab.com` |
   | `CLOUDFLARE_API_TOKEN` | token con permisos DNS + Tunnel |
   | `CLOUDFLARE_ZONE_ID` | 32 caracteres hexadecimales |
   | `CLOUDFLARE_TUNNEL_TOKEN` | empieza con `eyJ…` |

3. Vuelve a ejecutar `./setup.sh`. Reconstruye la imagen, enciende `db`, `app`, `tunnel` y `backup`, y espera hasta 120 s a que `/health` responda.

El panel queda en `https://postern.<BASE_DOMAIN>`. Mientras se propaga el DNS también puedes abrirlo en `http://127.0.0.1:28437`, accesible solo desde la propia máquina. El puerto es poco común a propósito para no chocar con otros servicios; si aun así está ocupado, el setup te avisa y puedes cambiarlo con `PANEL_LOCAL_PORT` en `.env`.

### Volver a ejecutar el setup

`./setup.sh` (o `setup.ps1`) se puede ejecutar las veces que quieras. Úsalo para encender, reconstruir tras un `git pull` o aplicar cambios de `.env`. En cada ejecución:

- Nunca sobrescribe valores que ya tengas en `.env`; solo genera los que estén vacíos.
- Añade las variables nuevas que traiga `.env.example` en versiones futuras, con su valor por defecto.
- Convierte `.env` a finales de línea Unix si se editó en Windows.
- Arranca Docker Desktop si está cerrado (macOS y Windows).
- Reconstruye la imagen y recrea los contenedores; los volúmenes (bases de datos y respaldos) no se tocan.
- Si `POSTGRES_PASSWORD` en `.env` ya no coincide con la base existente (por ejemplo, porque borraste `.env`), actualiza la contraseña en Postgres para que coincida.
- Si el puerto del panel lo usa otro programa, se detiene con un mensaje claro en vez de fallar a medias.

> La app valida la configuración al arrancar y, si falta o está mal algo, lista **todos** los problemas en un único mensaje (`docker compose logs app`).

## Obtener los valores de Cloudflare

### `CLOUDFLARE_API_TOKEN`

1. [dash.cloudflare.com](https://dash.cloudflare.com) → icono de perfil → **My Profile** → **API Tokens** → **Create Token** → **Create Custom Token**.
2. Permisos:

   | Ámbito | Recurso | Permiso |
   |---|---|---|
   | Zone | DNS | Edit |
   | Account | Cloudflare Tunnel | Edit |

3. *Zone Resources*: tu dominio. *Account Resources*: tu cuenta.
4. Copia el token (solo se muestra una vez).

### `CLOUDFLARE_ZONE_ID`

Dashboard → selecciona tu dominio → **Overview** → columna derecha, sección **API** → **Zone ID**.

### `CLOUDFLARE_TUNNEL_TOKEN`

1. [one.dash.cloudflare.com](https://one.dash.cloudflare.com) → **Networks** → **Tunnels** → **Create a tunnel** → **Cloudflared**.
2. Ponle un nombre (por ejemplo `posterndb`).
3. En el paso de instalación copia el token largo que aparece después de `cloudflared service install` (empieza con `eyJ`).
4. **No añadas Public Hostnames** en el dashboard: PosternDB los gestiona por API.

El Account ID y el Tunnel ID se leen del propio token, así que no hace falta configurarlos aparte.

## Esquema de hostnames y migración de dominio

Cada base se publica como `<CLIENT_HOST_PREFIX><cliente>.<BASE_DOMAIN>`.

| Escenario | `.env` | Cliente `acme` |
|---|---|---|
| Dominio compartido | `BASE_DOMAIN=pachislab.com`, `CLIENT_HOST_PREFIX=postern-`, `PANEL_HOSTNAME=postern.pachislab.com` | `postern-acme.pachislab.com` |
| Dominio dedicado | `BASE_DOMAIN=posterndb.com`, `CLIENT_HOST_PREFIX=`, `PANEL_HOSTNAME=posterndb.com` | `acme.posterndb.com` |

**¿Por qué un prefijo y no `acme.postern.pachislab.com`?** El certificado gratuito de Cloudflare (Universal SSL) cubre `pachislab.com` y `*.pachislab.com`, que es **un solo nivel**. Un segundo nivel (`*.postern.pachislab.com`) requiere Advanced Certificate Manager, que es de pago. Con prefijo todo queda en un nivel y es gratis.

### Migrar a un dominio propio

1. Añade el nuevo dominio a Cloudflare y crea un API token que tenga acceso a esa zona.
2. Actualiza `BASE_DOMAIN`, `CLIENT_HOST_PREFIX`, `PANEL_HOSTNAME` y `CLOUDFLARE_ZONE_ID` en `.env`.
3. Reinicia la app y revisa qué cambiaría:

```bash
docker compose up -d app
```

```bash
docker compose run --rm app migrate-hostnames --dry-run
```

4. Aplica la migración: publica el host nuevo, actualiza los metadatos y retira el antiguo.

```bash
docker compose run --rm app migrate-hostnames
```

> Cada cliente debe cambiar `--hostname` en su comando `cloudflared access tcp`. Los registros DNS del dominio anterior solo se borran si ambos dominios están en la misma zona; si no, elimínalos a mano desde el dashboard anterior.

## Conectar tu base de datos

Al crear una base, el panel muestra **una sola vez** el host, la base, el usuario, la contraseña y los dos fragmentos de abajo listos para copiar. Ejemplo para un cliente Spring Boot:

### 1. Instalar `cloudflared` en la máquina cliente

macOS:

```bash
brew install cloudflared
```

Debian / Ubuntu:

```bash
curl -L https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-amd64.deb -o cloudflared.deb && sudo dpkg -i cloudflared.deb
```

Windows:

```powershell
winget install --id Cloudflare.cloudflared
```

### 2. Abrir el túnel local

```bash
cloudflared access tcp --hostname cliente1.tudominio.com --url localhost:5432
```

Con el esquema de dominio compartido sería, por ejemplo, `--hostname postern-cliente1.pachislab.com`. Deja el proceso corriendo: expone la base en `localhost:5432` de la máquina cliente. Si ya tienes un PostgreSQL local en ese puerto, usa otro (`--url localhost:15432`) y cámbialo también en la URL JDBC.

### 3. Configurar `application.properties`

```properties
spring.datasource.url=jdbc:postgresql://localhost:5432/pdb_cliente1
spring.datasource.username=pdb_cliente1
spring.datasource.password=LA_CONTRASEÑA_QUE_MOSTRÓ_EL_PANEL
spring.datasource.driver-class-name=org.postgresql.Driver
```

El nombre de la base y del usuario es `pdb_` + el nombre del cliente (los guiones pasan a `_`). Para producción, ejecuta `cloudflared access tcp` como servicio (systemd, launchd o un sidecar en el mismo pod o compose de tu app).

## Funciones del panel

| Sección | Qué permite |
|---|---|
| **Bases de datos** | Crear una base vacía, desde plantilla, clonando otra o restaurando un respaldo. Buscar y ver el tiempo restante antes de expirar. |
| **Detalle → Resumen** | Tamaño, tablas, registros, conexiones abiertas, sesiones, tablas más grandes, ciclo de vida y trabajos recientes. |
| **Detalle → Esquema** | Diagrama entidad-relación con claves primarias y foráneas, o árbol navegable con teclado. Al elegir una tabla: columnas, relaciones, índices, conteo exacto y primeras filas. |
| **Detalle → Consola SQL** | Consultas de solo lectura (ver [Seguridad](#seguridad)), con límite de 10 s y 500 filas. Cada consulta queda en la actividad. |
| **Detalle → Conexión** | Fragmentos listos para copiar: `cloudflared`, Docker Compose sidecar, systemd, LaunchAgent de macOS, Spring Boot, Node.js, Python, .NET y URI. |
| **Detalle → Usuarios** | Usuarios extra con acceso de solo lectura o lectura/escritura (sin poder borrar tablas), incluidos los permisos sobre tablas futuras. |
| **Detalle → Respaldos** | Respaldar ahora, descargar, restaurar como base nueva e historial de trabajos. |
| **Detalle → Ajustes** | Nunca expira, límite propio, extender el plazo, **generar una contraseña nueva** sin tocar los datos y eliminar la base. |
| **Respaldos / Plantillas** | Vista global de respaldos y editor de plantillas SQL. |
| **Actividad** | Últimos 100 eventos con filtros. |
| **Administradores** | Invitar, cambiar rol, regenerar contraseña o quitar acceso (solo propietarios). |
| **Ajustes** | Configuración activa y prueba de notificaciones. |

**Roles:**

- **Lector:** ve bases, métricas, esquemas, respaldos y actividad, pero no datos.
- **Administrador:** además crea, elimina, respalda y consulta bases, y gestiona plantillas.
- **Propietario:** además gestiona administradores.

El `ADMIN_USER` de `.env` siempre es propietario y no se puede modificar desde el panel, así que nadie puede quedarse fuera. Si quitas a un administrador o cambias su rol, el cambio se aplica en la siguiente petición, aunque tenga una sesión abierta.

**Notificaciones:** con `NOTIFY_WEBHOOK_URL` (webhook de Discord o Slack) y/o `SMTP_*` + `NOTIFY_EMAIL_TO`, el auditor avisa una vez cuando una base entra en los últimos `EXPIRING_SOON_DAYS` días y cuando expira. El aviso se rearma si vuelve a haber actividad.

## Modo local

Para desarrollo o una máquina sin dominio, en `.env`:

```env
LOCAL_MODE=true
LOCAL_DB_PORT=25432
```

Con eso no hacen falta las variables de Cloudflare, el túnel no arranca y Postgres se publica solo en `127.0.0.1:25432` de esa máquina. Las credenciales que da el panel apuntan ahí directamente, sin `cloudflared`. Para volver a publicar por Cloudflare, pon `LOCAL_MODE=false`, rellena las variables de Cloudflare, ejecuta `./setup.sh` y luego `migrate-hostnames` para que las bases existentes reciban su host.

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

## Expiración por inactividad y respaldos

Cada `CHECK_INTERVAL_HOURS`, una goroutine interna:

1. Lee `pg_stat_activity` (sesiones abiertas ahora mismo, solo *client backends*) y `pg_stat_database.sessions` (contador acumulado de sesiones).
2. Si hay sesiones abiertas **o** el contador subió desde la revisión anterior, actualiza `last_seen_at`. El contador detecta clientes que se conectaron y se fueron entre dos revisiones, cosa que una foto puntual cada 6 h no vería. Si el contador baja porque se reiniciaron las estadísticas, solo se toma como nueva línea base.
3. Si `now() - last_seen_at > DB_EXPIRATION_DAYS`: retira el host de Cloudflare, cierra las sesiones y marca la base como `expirada`.
4. Si no puede leer las estadísticas, **no expira nada** en esa pasada.

Todo queda en `audit_log` y se ve en la sección **Actividad** del panel.

**Respaldos:** la imagen distroless no incluye `pg_dump`. Por eso todo lo que lo necesita pasa por una cola de trabajos (tabla `jobs`) que ejecuta el servicio `backup`, basado en la imagen oficial de Postgres, cada `BACKUP_POLL_SECONDS`:

| Trabajo | Cuándo |
|---|---|
| `dump` | "Respaldar ahora" en el panel |
| `archive` | Al expirar o eliminar con `BACKUP_BEFORE_DELETE=true`: la app renombra la base y su rol a `pdbarch_<cliente>_<fecha>` y desactiva el login; el sidecar la vuelca y solo después la borra. Si el volcado falla, la base se conserva |
| `restore` | "Restaurar como base nueva" |
| `clone` | Crear una base clonando otra (`pg_dump \| pg_restore`, sin bloquear la original) |
| `delete_file` | Borrar un respaldo |

Los respaldos quedan en el volumen `backups`; el panel los lista y los sirve para descargar (montado en solo lectura en la app). Con `BACKUP_BEFORE_DELETE=false`, al expirar o eliminar la base se borra de inmediato sin respaldo.

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

## Licencia

MIT. Ver [LICENSE](LICENSE).
