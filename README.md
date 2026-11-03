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

Si compartes el dominio con otras cosas, usa `CLIENT_HOST_PREFIX=postern-` y las bases quedan como `postern-nombre.tudominio.com`.

Otras opciones útiles del `.env`:

| Variable | Para qué sirve | Por defecto |
|---|---|---|
| `DB_EXPIRATION_DAYS` | Días sin uso antes de que una base expire | 30 |
| `ALLOW_SELF_REGISTER` | `true` deja que cualquiera cree su cuenta | true |
| `MAX_DATABASES_PER_CLIENT` | Bases que puede crear cada usuario | 3 |
| `LOCAL_MODE` | `true` para probar sin Cloudflare, solo en tu máquina | false |

## 3. Usar el panel (dueño)

Entra a tu panel con el usuario y contraseña que mostró el setup (también están en `.env`).

- **Bases de datos**: crea, mira y borra bases; ve sus tablas y **edita los datos en vivo**, ve el esquema como diagrama y las **conexiones activas**.
- **Usuarios**: las personas que se registraron. Puedes agregar o quitar usuarios.
- **Expiración**: define cuánto tarda una base sin uso en expirar. Se cambia **para todos** o **por usuario** (por ejemplo, "nunca expira" para un amigo). Un usuario puede tener además una excepción en una base concreta.
- **Plantillas**: bases de ejemplo listas para usar (tareas, blog, tienda, citas, inventario). Marca cuáles ven tus usuarios.
- **Respaldos**: manuales, descarga y restauración. Antes de borrar una base expirada se guarda una copia.
- **Administradores**: suma gente de confianza con permisos de propietario, administrador o solo lectura.

## 4. Usar PosternDB (quien se registra)

1. Entra a la dirección del panel y pulsa **Crear cuenta**. Elige un usuario y una contraseña de 12 caracteres o más.
2. Pulsa **Nueva base de datos**, ponle un nombre y, si quieres, empieza con una plantilla de muestra.
3. **Guarda la contraseña que aparece**: solo se muestra una vez (si la pierdes, genera otra en *Ajustes*).
4. Desde el panel puedes ver y editar tus tablas, ver quién está conectado y cuándo vence tu base.

Solo ves tus propias bases. Conectarte a una base reinicia su contador de inactividad.

### Conectar tu aplicación

En tu computadora o servidor instala [`cloudflared`](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/) y abre el puente (deja esa ventana abierta):

```bash
cloudflared access tcp --hostname nombre.posterndb.com --url localhost:5432
```

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
