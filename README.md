# PosternDB

Tu propio servicio de bases de datos PostgreSQL. Instálalo en tu computadora o servidor, y tú (o la gente que invites) crea bases de datos desde un panel web. Se conectan desde internet sin que tengas que abrir puertos.

- **Tú eres el dueño** y ves todo: bases, usuarios, respaldos, actividad.
- **Tus usuarios se registran solos**, crean sus bases y solo ven las suyas.
- **Cada base se borra sola si nadie la usa** durante un tiempo. Tú decides cuánto, para todos o por usuario.

---

## 1. Instalar

Necesitas [Docker](https://www.docker.com/products/docker-desktop/) y un dominio en [Cloudflare](https://dash.cloudflare.com) (el plan gratis alcanza).

```bash
git clone git@github.com:Tecsharp/PosternDB.git
```

```bash
cd PosternDB && ./setup.sh
```

En Windows usa `powershell -ExecutionPolicy Bypass -File .\setup.ps1`.

La primera vez, el script crea el archivo `.env`, **te muestra el usuario y la contraseña de tu panel** y te dice qué falta llenar. Después de llenarlo, vuelve a ejecutar `./setup.sh`. Puedes ejecutarlo cuantas veces quieras: actualiza y enciende todo sin perder datos.

## 2. Llenar el `.env`

Solo hay cuatro datos obligatorios. El archivo `.env` trae cada uno explicado.

| Variable | Qué es | Ejemplo |
|---|---|---|
| `BASE_DOMAIN` | Tu dominio | `posterndb.com` |
| `CLOUDFLARE_API_TOKEN` | Permiso para que PosternDB cree los nombres de las bases | ver abajo |
| `CLOUDFLARE_ZONE_ID` | Identificador de tu dominio en Cloudflare | 32 letras y números |
| `CLOUDFLARE_TUNNEL_TOKEN` | La llave del túnel que conecta tu servidor con internet | empieza con `eyJ` |

### Cómo obtener los datos de Cloudflare

1. **`CLOUDFLARE_ZONE_ID`**: en [Cloudflare](https://dash.cloudflare.com) elige tu dominio. En la página principal, a la derecha, está **Zone ID**.
2. **`CLOUDFLARE_API_TOKEN`**: perfil → **API Tokens** → **Create Token** → **Create Custom Token**. Dale dos permisos: *Zone → DNS → Edit* y *Account → Cloudflare Tunnel → Edit*. Copia el token, solo se muestra una vez.
3. **`CLOUDFLARE_TUNNEL_TOKEN`**: en [Zero Trust](https://one.dash.cloudflare.com) → **Networks → Tunnels → Create a tunnel → Cloudflared**. Ponle un nombre y copia el token largo que aparece en el paso de instalación (empieza con `eyJ`). **No agregues nada más ahí**: PosternDB lo hace solo.

### Elegir tus direcciones

Con `BASE_DOMAIN=posterndb.com` y estas dos líneas:

```
CLIENT_HOST_PREFIX=
PANEL_HOSTNAME=app.posterndb.com
```

- El panel queda en **`app.posterndb.com`**.
- Cada base queda en **`nombre.posterndb.com`**.
- `posterndb.com` queda libre para tu página.

### Entrar con Google (opcional)

Con esto tus usuarios entran con un clic y, si olvidan su contraseña, la recuperan con Google: sin servidor de correo que configurar. Tú decides con `AUTH_MODE` si usan Google, contraseña o las dos formas. El administrador siempre entra con su usuario y contraseña.

1. En [Google Cloud Console](https://console.cloud.google.com) crea un proyecto. Ve a **APIs y servicios → Pantalla de consentimiento de OAuth**, elige *Externo* y llena el nombre y tu correo.
2. En **Credenciales → Crear credenciales → ID de cliente de OAuth**, elige *Aplicación web*.
3. En **URI de redireccionamiento autorizados** agrega exactamente `https://app.posterndb.com/api/auth/google/callback` (cambia `app.posterndb.com` por tu `PANEL_HOSTNAME`).
4. Copia el *ID de cliente* y el *secreto* en `GOOGLE_CLIENT_ID` y `GOOGLE_CLIENT_SECRET` del `.env`, pon `AUTH_MODE=both` (o `google`) y ejecuta `./setup.sh`.

Si dejas esos dos vacíos, el botón no aparece y todo sigue funcionando con correo y contraseña. Google solo sirve para usuarios: nunca abre el panel de administración.

Una cuenta que ya existe con contraseña **no** se abre con Google solo por tener el mismo correo: su dueño la vincula desde *Seguridad*. Así nadie entra a una cuenta ajena.

Si compartes el dominio con otras cosas, usa `CLIENT_HOST_PREFIX=postern-` y las bases quedan como `postern-nombre.tudominio.com`.

Otras opciones útiles del `.env`:

| Variable | Para qué sirve | Por defecto |
|---|---|---|
| `DB_EXPIRATION_DAYS` | Días sin uso antes de que una base expire | 30 |
| `LANDING_URL` | Tu página con la política de privacidad (`/privacy`) y las condiciones (`/terms`); el acceso al panel enlaza ahí | vacío (sin enlaces) |
| `ALLOW_SELF_REGISTER` | `true` deja que cualquiera cree su cuenta | true |
| `AUTH_MODE` | Cómo entran los usuarios: `password`, `google` o `both` | `password` (o `both` si llenas Google) |
| `GOOGLE_CLIENT_ID` y `GOOGLE_CLIENT_SECRET` | Activan "Continuar con Google" (ver abajo) | vacío (desactivado) |
| `MAX_DATABASES_PER_CLIENT` | Bases que puede crear cada usuario | 3 |
| `LOCAL_MODE` | `true` para probar sin Cloudflare, solo en tu máquina | false |

## 3. Usar el panel (dueño)

Entra a tu panel con el usuario y contraseña que mostró el setup (también están en `.env`).

- **Bases de datos**: crea, mira y borra bases; ve sus tablas y **edita los datos en vivo**, ve el esquema como diagrama y las **conexiones activas**.
- **Usuarios**: las personas que se registraron (nombre y correo). Puedes agregar o quitar usuarios y generarles una contraseña nueva.
- **Expiración**: define cuánto tarda una base sin uso en expirar. Se cambia **para todos** o **por usuario** (por ejemplo, "nunca expira" para un amigo). Un usuario puede tener además una excepción en una base concreta.
- **Plantillas**: bases de ejemplo listas para usar (tareas, blog, tienda, citas, inventario). Marca cuáles ven tus usuarios.
- **Respaldos**: manuales, descarga y restauración. Antes de borrar una base expirada se guarda una copia.
- **Administradores**: suma gente de confianza con permisos de propietario, administrador o solo lectura.

## 4. Usar PosternDB (quien se registra)

1. Entra a la dirección del panel y pulsa **Crear cuenta**. (Si entras con Google, la primera vez eliges tu nombre de usuario). Escribe tu nombre completo, tu correo y una contraseña de 12 caracteres o más, y acepta cómo se usará tu correo (para entrar y para recuperar tu cuenta con Google; no se comparte ni se manda publicidad). Si el dueño activó Google, también puedes pulsar **Continuar con Google**.
2. Pulsa **Nueva base de datos**, ponle un nombre y, si quieres, empieza con una plantilla de muestra. Tu base se llamará con **tu código personal** al inicio (por ejemplo `k7x9qa-tienda`), y su dirección será `k7x9qa-tienda.posterndb.com`. Así dos personas pueden usar el mismo nombre sin chocar. Tu código lo ves en *Ajustes*.
3. **Guarda la contraseña que aparece**: solo se muestra una vez (si la pierdes, genera otra en *Ajustes*).
4. Desde el panel puedes ver y editar tus tablas, ver quién está conectado y cuándo vence tu base.

Solo ves tus propias bases. Conectarte a una base reinicia su contador de inactividad.

Tu panel tiene **Mis bases de datos**, **Respaldos**, **Actividad**, **Seguridad** (nombre de usuario, contraseña y Google) y **Ajustes**. Marca **Recordarme** al entrar para no volver a escribir tus datos cada vez.

**¿Olvidaste tu contraseña?** Entra con Google (si ya vinculaste tu cuenta en *Seguridad*) y crea una nueva. Si no hay Google, pídele al dueño una nueva en *Usuarios → Nueva contraseña*.

### Conectar tu aplicación

Tu base vive en internet detrás de Cloudflare. Para usarla desde tu computadora necesitas **cloudflared**, un programa gratuito que abre un puente seguro. El panel tiene un botón **Ver tutorial** al crear la base, y la misma guía vive en `/help` de tu panel. Aquí va resumida.

**1. Instala cloudflared** ([todas las descargas](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/)):

| Sistema | Comando |
|---|---|
| Windows (PowerShell) | `winget install --id Cloudflare.cloudflared` |
| macOS (Terminal) | `brew install cloudflared` |
| Linux (Debian/Ubuntu) | `curl -L --output cloudflared.deb https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-amd64.deb && sudo dpkg -i cloudflared.deb` |

Cierra la terminal y ábrela otra vez. Comprueba con `cloudflared --version`.

**2. Si dice que no encuentra el comando**, falta agregarlo al PATH (la lista de carpetas donde tu sistema busca programas):

- **Windows:** el programa queda en `C:\Program Files (x86)\cloudflared`. En PowerShell: `[Environment]::SetEnvironmentVariable("Path", $env:Path + ";C:\Program Files (x86)\cloudflared", "User")`, y abre una PowerShell nueva. También puedes hacerlo con `Windows + R` → `sysdm.cpl` → Opciones avanzadas → Variables de entorno → Path → Editar → Nuevo.
- **macOS (Apple Silicon):** `echo 'eval "$(/opt/homebrew/bin/brew shellenv)"' >> ~/.zprofile` y abre una Terminal nueva.
- **Linux:** instálalo en `/usr/local/bin` (ya está en el PATH) o agrega tu carpeta con `echo 'export PATH="$HOME/bin:$PATH"' >> ~/.bashrc`.

**3. Abre el puente** y deja esa ventana abierta:

```bash
cloudflared access tcp --hostname nombre.posterndb.com --url localhost:5432
```

Si el puerto 5432 está ocupado, usa otro, por ejemplo `--url localhost:15432`, y ponlo igual en tu aplicación.

Luego conecta tu app como si la base estuviera en tu propia máquina. Ejemplo con Spring Boot:

```properties
spring.datasource.url=jdbc:postgresql://localhost:5432/pdb_nombre
spring.datasource.username=pdb_nombre
spring.datasource.password=LA_CONTRASEÑA_DEL_PANEL
```

La pestaña **Conexión** de tu base trae estos datos y ejemplos para Node, Python, .NET y más.

## Si algo no funciona

- **El panel no abre**: espera uno o dos minutos a que Cloudflare propague el nombre. Mientras tanto abre `http://127.0.0.1:28437` en la misma máquina del servidor.
- **"ya existe un registro DNS"**: ese nombre ya lo usa otra cosa en Cloudflare. Bórralo en *DNS → Registros* o elige otro `PANEL_HOSTNAME`.
- **Ver qué pasa**: `docker compose logs -f app`.

Más detalle técnico (seguridad, pruebas, estructura): [docs/DESARROLLO.md](docs/DESARROLLO.md).

## Licencia

Ver [LICENSE](LICENSE).
