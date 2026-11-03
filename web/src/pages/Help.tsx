import { useState, type ReactNode } from 'react'
import { Logo } from '../components/Shell'
import { ButtonLink, CodeBlock, Notice, segmentClass } from '../components/ui'

type OS = 'windows' | 'mac' | 'linux'

const DOWNLOADS = 'https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/'
const RELEASES = 'https://github.com/cloudflare/cloudflared/releases/latest'
const HOST_PATTERN = /^[a-z0-9]([a-z0-9.-]{0,251}[a-z0-9])?$/

const osNames: Record<OS, string> = { windows: 'Windows', mac: 'macOS', linux: 'Linux' }

function detectOS(): OS {
  const ua = navigator.userAgent
  if (/Windows/i.test(ua)) return 'windows'
  if (/Mac/i.test(ua)) return 'mac'
  return 'linux'
}

/** The host comes in the link from the credentials window; keep it only if it looks like a hostname. */
function initialHost(): string {
  const host = (new URLSearchParams(window.location.search).get('host') ?? '').trim().toLowerCase()
  return HOST_PATTERN.test(host) ? host : ''
}

function Step({ n, title, children }: { n: number; title: string; children: ReactNode }) {
  return (
    <section className="group-box px-5 py-5">
      <h2 className="flex items-center gap-3 text-[1.15rem] font-semibold">
        <span aria-hidden className="grid size-8 shrink-0 place-items-center rounded-full bg-surface-3 text-[0.95rem]">
          {n}
        </span>
        {title}
      </h2>
      <div className="mt-3 space-y-3 text-[0.97rem] leading-relaxed text-ink-2 [&_a]:text-accent-text [&_a]:underline [&_strong]:text-ink">{children}</div>
    </section>
  )
}

function Install({ os }: { os: OS }) {
  if (os === 'windows') {
    return (
      <>
        <p>
          Abre <strong>PowerShell</strong> (botón Inicio, escribe “PowerShell” y Enter) y pega:
        </p>
        <CodeBlock code="winget install --id Cloudflare.cloudflared" label="Instalar cloudflared en Windows" />
        <p>
          Si no tienes <code>winget</code>, descarga el instalador <strong>cloudflared-windows-amd64.msi</strong> desde{' '}
          <a href={RELEASES} target="_blank" rel="noopener noreferrer">
            la página de descargas
          </a>{' '}
          (o la{' '}
          <a href={DOWNLOADS} target="_blank" rel="noopener noreferrer">
            documentación oficial
          </a>
          ) y ábrelo.
        </p>
        <p>
          Al terminar, <strong>cierra PowerShell y ábrelo de nuevo</strong>, para que reconozca el comando.
        </p>
      </>
    )
  }
  if (os === 'mac') {
    return (
      <>
        <p>
          Abre la app <strong>Terminal</strong> y pega (necesitas{' '}
          <a href="https://brew.sh" target="_blank" rel="noopener noreferrer">
            Homebrew
          </a>
          ):
        </p>
        <CodeBlock code="brew install cloudflared" label="Instalar cloudflared en macOS" />
        <p>
          Sin Homebrew, descarga el archivo <strong>cloudflared-darwin-amd64.tgz</strong> (Mac Intel) o <strong>cloudflared-darwin-arm64.tgz</strong> (Apple Silicon) desde{' '}
          <a href={RELEASES} target="_blank" rel="noopener noreferrer">
            la página de descargas
          </a>
          , descomprímelo y mueve el programa a una carpeta del PATH:
        </p>
        <CodeBlock code="sudo mv cloudflared /usr/local/bin/" label="Mover cloudflared al PATH en macOS" />
      </>
    )
  }
  return (
    <>
      <p>Abre una terminal y elige tu distribución. Para Debian, Ubuntu o Raspberry Pi:</p>
      <CodeBlock
        code={`curl -L --output cloudflared.deb https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-amd64.deb
sudo dpkg -i cloudflared.deb`}
        label="Instalar cloudflared en Debian o Ubuntu"
      />
      <p>
        En Raspberry Pi o equipos ARM cambia <code>amd64</code> por <code>arm64</code>. Para Fedora, CentOS o RHEL:
      </p>
      <CodeBlock
        code={`curl -L --output cloudflared.rpm https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-x86_64.rpm
sudo rpm -i cloudflared.rpm`}
        label="Instalar cloudflared en Fedora o RHEL"
      />
      <p>
        Para cualquier otra, descarga el binario (ver{' '}
        <a href={DOWNLOADS} target="_blank" rel="noopener noreferrer">
          todas las opciones
        </a>
        ):
      </p>
      <CodeBlock
        code={`curl -L --output cloudflared https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-amd64
chmod +x cloudflared
sudo mv cloudflared /usr/local/bin/`}
        label="Instalar el binario de cloudflared en Linux"
      />
    </>
  )
}

function PathHelp({ os }: { os: OS }) {
  if (os === 'windows') {
    return (
      <>
        <p>
          Si PowerShell responde <em>“cloudflared no se reconoce como un cmdlet…”</em>, Windows no sabe dónde quedó el programa. Agrégalo al <strong>PATH</strong>. El instalador lo deja normalmente en{' '}
          <code>C:\Program Files (x86)\cloudflared</code>.
        </p>
        <p>
          <strong>Opción rápida</strong>, pega esto en PowerShell y luego ciérralo y ábrelo otra vez:
        </p>
        <CodeBlock
          code={`[Environment]::SetEnvironmentVariable("Path", $env:Path + ";C:\\Program Files (x86)\\cloudflared", "User")`}
          label="Agregar cloudflared al PATH en Windows"
        />
        <p>
          <strong>Opción con ventanas:</strong> presiona <code>Windows + R</code>, escribe <code>sysdm.cpl</code> y Enter → pestaña <strong>Opciones avanzadas</strong> → <strong>Variables de entorno</strong> → en “Variables de usuario” selecciona{' '}
          <strong>Path</strong> → <strong>Editar</strong> → <strong>Nuevo</strong> → pega la carpeta → <strong>Aceptar</strong> en las tres ventanas. Abre una PowerShell nueva.
        </p>
      </>
    )
  }
  if (os === 'mac') {
    return (
      <>
        <p>
          Si la Terminal responde <em>“command not found: cloudflared”</em>, es porque Homebrew no está en tu PATH. En Mac con chip Apple (M1, M2, M3…) está en <code>/opt/homebrew/bin</code>:
        </p>
        <CodeBlock
          code={`echo 'eval "$(/opt/homebrew/bin/brew shellenv)"' >> ~/.zprofile
source ~/.zprofile`}
          label="Agregar Homebrew al PATH en macOS"
        />
        <p>Cierra la Terminal y ábrela otra vez. Si moviste el programa a mano, debe estar en una carpeta como <code>/usr/local/bin</code>.</p>
      </>
    )
  }
  return (
    <>
      <p>
        Si la terminal responde <em>“cloudflared: command not found”</em>, revisa dónde quedó el programa y agrega esa carpeta al PATH. <code>/usr/local/bin</code> ya suele estar. Si lo dejaste en <code>~/bin</code>:
      </p>
      <CodeBlock
        code={`echo 'export PATH="$HOME/bin:$PATH"' >> ~/.bashrc
source ~/.bashrc`}
        label="Agregar una carpeta al PATH en Linux"
      />
      <p>Si usas zsh, escribe en <code>~/.zshrc</code> en lugar de <code>~/.bashrc</code>. Cierra la terminal y ábrela otra vez.</p>
    </>
  )
}

export function Help() {
  const [os, setOs] = useState<OS>(detectOS)
  const [host, setHost] = useState(initialHost)
  const shownHost = host || 'tu-base.tudominio.com'

  return (
    <div className="min-h-dvh">
      <header className="flex h-14 items-center justify-between bg-sidebar px-5">
        <a href="/" aria-label="Ir al panel">
          <Logo />
        </a>
        <ButtonLink size="sm" variant="ghost" href="/">
          Volver al panel
        </ButtonLink>
      </header>

      <main className="mx-auto max-w-3xl space-y-5 px-5 py-8">
        <div>
          <h1 className="text-[1.9rem] font-bold tracking-tight">Cómo conectarte a tu base de datos</h1>
          <p className="mt-2 text-[1rem] leading-relaxed text-ink-2">
            Tu base vive en internet detrás de Cloudflare. Para usarla desde tu computadora o tu aplicación necesitas <strong className="text-ink">cloudflared</strong>, un programa gratuito de Cloudflare que abre un puente seguro hacia ella. Son tres pasos y se hace una sola vez.
          </p>
        </div>

        <section className="group-box px-5 py-4">
          <label className="block">
            <span className="label">Tu dirección (host)</span>
            <input
              className="field max-w-md font-mono"
              placeholder="tu-base.tudominio.com"
              spellCheck={false}
              autoCapitalize="none"
              value={host}
              onChange={(e) => setHost(e.target.value.trim().toLowerCase())}
            />
          </label>
          <p className="mt-1.5 text-[0.85rem] text-ink-3">La ves al crear tu base y en su pestaña Conexión. Al escribirla, los comandos de abajo se llenan solos.</p>
        </section>

        <div role="group" aria-label="Sistema operativo" className="inline-flex rounded-[10px] bg-surface-2 p-0.5">
          {(Object.keys(osNames) as OS[]).map((k) => (
            <button key={k} type="button" aria-pressed={os === k} onClick={() => setOs(k)} className={segmentClass(os === k)}>
              {osNames[k]}
            </button>
          ))}
        </div>

        <Step n={1} title="Instala cloudflared">
          <Install os={os} />
        </Step>

        <Step n={2} title="Comprueba que quedó instalado">
          <p>En una terminal nueva escribe:</p>
          <CodeBlock code="cloudflared --version" label="Comprobar la versión de cloudflared" />
          <p>
            Debe mostrar algo como <code>cloudflared version 2025.x.x</code>. Si dice que el comando no existe, lee lo siguiente.
          </p>
          <PathHelp os={os} />
        </Step>

        <Step n={3} title="Abre el puente">
          <p>Con tu dirección, ejecuta este comando y <strong>deja esa ventana abierta</strong> mientras uses la base:</p>
          <CodeBlock code={`cloudflared access tcp --hostname ${shownHost} --url localhost:5432`} label="Abrir el puente hacia tu base de datos" />
          <p>
            Verás un mensaje como <code>Start Websocket listener</code>: está funcionando. Ahora tu base responde en <code>localhost:5432</code> de tu computadora.
          </p>
          <Notice tone="info">
            Si tienes otro PostgreSQL usando el puerto 5432, usa otro número, por ejemplo <code>--url localhost:15432</code>, y escríbelo igual en tu aplicación.
          </Notice>
        </Step>

        <Step n={4} title="Conecta tu aplicación">
          <p>
            Usa <code>localhost</code> como servidor, el nombre de la base, el usuario y la contraseña que te mostramos al crearla. En la pestaña <strong>Conexión</strong> de tu base hay ejemplos listos para copiar (Spring Boot, Node, Python, .NET y más). Por ejemplo, para Spring Boot:
          </p>
          <CodeBlock
            code={`spring.datasource.url=jdbc:postgresql://localhost:5432/pdb_tu_base
spring.datasource.username=pdb_tu_base
spring.datasource.password=LA_CONTRASEÑA_QUE_GUARDASTE`}
            label="Ejemplo de conexión con Spring Boot"
          />
          <p>
            ¿Perdiste la contraseña? En la pestaña <strong>Ajustes</strong> de tu base puedes generar una nueva.
          </p>
        </Step>

        <section className="group-box px-5 py-5">
          <h2 className="text-[1.15rem] font-semibold">Problemas comunes</h2>
          <dl className="mt-3 space-y-3 text-[0.97rem] leading-relaxed">
            <div>
              <dt className="font-semibold">“cloudflared” no se reconoce o “command not found”</dt>
              <dd className="text-ink-2">Falta agregarlo al PATH: mira el paso 2 y abre una terminal nueva.</dd>
            </div>
            <div>
              <dt className="font-semibold">“connection refused” al conectar tu aplicación</dt>
              <dd className="text-ink-2">El puente del paso 3 no está abierto. Ábrelo y deja la ventana abierta.</dd>
            </div>
            <div>
              <dt className="font-semibold">“address already in use” o el puerto está ocupado</dt>
              <dd className="text-ink-2">Otro programa usa el 5432. Usa otro puerto, por ejemplo 15432, en el comando y en tu aplicación.</dd>
            </div>
            <div>
              <dt className="font-semibold">“password authentication failed”</dt>
              <dd className="text-ink-2">La contraseña no es la correcta. Genera una nueva en Ajustes de tu base.</dd>
            </div>
          </dl>
        </section>
      </main>
    </div>
  )
}
