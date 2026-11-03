import { useEffect, useState, type FormEvent, type ReactNode } from 'react'
import { api, type PublicConfig, type Session } from '../api'
import { Logo } from '../components/Shell'
import { Button, ErrorNote, errorMessage, PasswordInput } from '../components/ui'

const oauthMessages: Record<string, string> = {
  cancelled: 'Cancelaste el acceso con Google. Puedes intentarlo de nuevo.',
  failed: 'No pudimos entrar con Google. Inténtalo de nuevo.',
  disabled: 'El registro de cuentas nuevas está desactivado.',
  unverified: 'Google no confirmó que ese correo sea tuyo.',
  mismatch: 'Ese correo ya está vinculado a otra cuenta de Google.',
  notlinked: 'Ese correo ya tiene una cuenta. Entra con tu contraseña y vincula Google desde Seguridad.',
  taken: 'Esa cuenta de Google ya está vinculada a otro usuario.',
}

const EMAIL_USE =
  'Tu correo es tu usuario para entrar y la forma de recuperar tu cuenta con Google. No lo compartimos con nadie ni te enviamos publicidad.'

function GoogleMark() {
  return (
    <svg viewBox="0 0 48 48" width="18" height="18" aria-hidden="true">
      <path fill="#EA4335" d="M24 9.5c3.54 0 6.71 1.22 9.21 3.6l6.85-6.85C35.9 2.38 30.47 0 24 0 14.62 0 6.51 5.38 2.56 13.22l7.98 6.19C12.43 13.72 17.74 9.5 24 9.5z" />
      <path fill="#4285F4" d="M46.98 24.55c0-1.57-.15-3.09-.38-4.55H24v9.02h12.94c-.58 2.96-2.26 5.48-4.78 7.18l7.73 6c4.51-4.18 7.09-10.36 7.09-17.65z" />
      <path fill="#FBBC05" d="M10.53 28.59c-.48-1.45-.76-2.99-.76-4.59s.27-3.14.76-4.59l-7.98-6.19C.92 16.46 0 20.12 0 24c0 3.88.92 7.54 2.56 10.78l7.97-6.19z" />
      <path fill="#34A853" d="M24 48c6.48 0 11.93-2.13 15.89-5.81l-7.73-6c-2.15 1.45-4.92 2.3-8.16 2.3-6.26 0-11.57-4.22-13.47-9.91l-7.98 6.19C6.51 42.62 14.62 48 24 48z" />
    </svg>
  )
}

function LegalLink({ to, children }: { to: string; children: ReactNode }) {
  return (
    <a href={to} target="_blank" rel="noopener noreferrer" className="underline decoration-ink-3/40 underline-offset-2 hover:text-ink">
      {children}
    </a>
  )
}

/** Google redirects back to "/?oauth=<reason>" when something went wrong. */
function takeOAuthError(): string {
  const params = new URLSearchParams(window.location.search)
  const reason = params.get('oauth')
  if (!reason) return ''
  window.history.replaceState(null, '', window.location.pathname)
  return oauthMessages[reason] ?? oauthMessages.failed
}

export function Login({ onLoggedIn }: { onLoggedIn: (s: Session) => void }) {
  const [mode, setMode] = useState<'login' | 'register'>('login')
  const [login, setLogin] = useState('')
  const [fullName, setFullName] = useState('')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [accept, setAccept] = useState(false)
  const [remember, setRemember] = useState(false)
  const [adminForm, setAdminForm] = useState(false)
  const [error, setError] = useState(takeOAuthError)
  const [busy, setBusy] = useState(false)
  const [config, setConfig] = useState<PublicConfig>({ allowSelfRegister: true, googleEnabled: false, passwordEnabled: true, landingUrl: '' })
  const isRegister = mode === 'register'

  useEffect(() => {
    api
      .publicConfig()
      .then(setConfig)
      .catch(() => undefined)
  }, [])

  // With AUTH_MODE=google, registered users only use Google; the password form
  // stays available for the administrator, behind a small link.
  const showForm = config.passwordEnabled || adminForm
  const canRegister = config.passwordEnabled && config.allowSelfRegister

  async function submit(e: FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      if (isRegister) await api.register({ email, fullName, password, acceptEmailUse: accept })
      else await api.login(login, password, remember)
      onLoggedIn(await api.session())
    } catch (err) {
      setError(errorMessage(err))
      setBusy(false)
    }
  }

  function switchMode() {
    setMode(isRegister ? 'login' : 'register')
    setError('')
    setPassword('')
  }

  return (
    <main className="grid min-h-dvh place-items-center px-4 py-8">
      <div className="w-full max-w-[26rem]">
        <div className="mb-8 text-center">
          <Logo size="lg" />
          <p className="mt-3 text-[0.95rem] text-ink-2">Bases de datos PostgreSQL publicadas por Cloudflare Tunnel.</p>
        </div>
        <form onSubmit={submit} className="group-box px-6 py-6" aria-labelledby="login-title">
          <h1 id="login-title" className="text-[1.15rem] font-semibold">
            {isRegister ? 'Crear cuenta' : adminForm ? 'Administrador' : 'Iniciar sesión'}
          </h1>

          {config.googleEnabled && !adminForm && (
            <>
              <a
                href={`/api/auth/google/start${remember && !isRegister ? '?remember=1' : ''}`}
                className="mt-5 inline-flex h-10 w-full items-center justify-center gap-2.5 rounded-[10px] bg-surface-3 text-[0.95rem] font-medium text-ink hover:brightness-110"
              >
                <GoogleMark />
                Continuar con Google
              </a>
              {!isRegister && <p className="mt-2 text-center text-[0.8rem] text-ink-3">{EMAIL_USE}</p>}
              {config.passwordEnabled && (
                <div className="my-4 flex items-center gap-3 text-[0.82rem] text-ink-3" aria-hidden="true">
                  <span className="h-px flex-1 bg-hairline" />o con tu correo<span className="h-px flex-1 bg-hairline" />
                </div>
              )}
            </>
          )}

          {showForm && (
            <div className={`space-y-4 ${config.googleEnabled && config.passwordEnabled && !adminForm ? '' : 'mt-5'}`}>
              {isRegister ? (
                <>
                  <label className="block">
                    <span className="label">Nombre completo</span>
                    <input className="field" autoComplete="name" required minLength={2} maxLength={80} value={fullName} onChange={(e) => setFullName(e.target.value)} />
                  </label>
                  <label className="block">
                    <span className="label">Correo</span>
                    <input className="field" type="email" autoComplete="email" autoCapitalize="none" spellCheck={false} required value={email} onChange={(e) => setEmail(e.target.value)} />
                  </label>
                </>
              ) : (
                <label className="block">
                  <span className="label">{adminForm ? 'Usuario' : 'Correo o usuario'}</span>
                  <input
                    className="field"
                    autoComplete="username"
                    autoCapitalize="none"
                    autoCorrect="off"
                    spellCheck={false}
                    autoFocus
                    required
                    value={login}
                    onChange={(e) => setLogin(e.target.value)}
                  />
                </label>
              )}
              <label className="block">
                <span className="label">Contraseña</span>
                <PasswordInput
                  autoComplete={isRegister ? 'new-password' : 'current-password'}
                  minLength={isRegister ? 12 : undefined}
                  required
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                />
                {isRegister && <span className="mt-1.5 block text-[0.85rem] text-ink-2">Mínimo 12 caracteres.</span>}
              </label>

              {!isRegister && (
                <label className="flex cursor-pointer items-center gap-2.5 text-[0.9rem] font-normal text-ink">
                  <input type="checkbox" className="size-4 accent-[var(--accent)]" checked={remember} onChange={(e) => setRemember(e.target.checked)} />
                  Recordarme
                </label>
              )}

              {isRegister && (
                <div className="rounded-[10px] bg-surface-2 px-3.5 py-3">
                  <p className="text-[0.85rem] font-medium">Cómo usaremos tu correo</p>
                  <p className="mt-1 text-[0.85rem] text-ink-2">{EMAIL_USE}</p>
                  <label className="mt-3 flex cursor-pointer items-start gap-2.5 text-[0.9rem] font-normal text-ink">
                    <input type="checkbox" className="mt-1 accent-[var(--accent)]" required checked={accept} onChange={(e) => setAccept(e.target.checked)} />
                    Entiendo y acepto cómo se usará mi correo
                  </label>
                </div>
              )}
            </div>
          )}

          {error && (
            <div className="mt-4">
              <ErrorNote message={error} />
            </div>
          )}

          {isRegister && config.landingUrl && (
            <p className="mt-4 text-[0.82rem] text-ink-3">
              Al crear tu cuenta aceptas las <LegalLink to={`${config.landingUrl}/terms`}>Condiciones del servicio</LegalLink> y la <LegalLink to={`${config.landingUrl}/privacy`}>Política de privacidad</LegalLink>.
            </p>
          )}

          {showForm && (
            <Button type="submit" variant="primary" className="mt-6 w-full" disabled={busy}>
              {busy ? (isRegister ? 'Creando cuenta…' : 'Entrando…') : isRegister ? 'Crear cuenta' : 'Entrar'}
            </Button>
          )}

          {!isRegister && config.googleEnabled && config.passwordEnabled && (
            <p className="mt-4 text-center text-[0.82rem] text-ink-3">¿Olvidaste tu contraseña? Recomendamos el acceso con Google.</p>
          )}
          {(canRegister || isRegister) && (
            <button type="button" className="mt-4 block w-full text-center text-[0.9rem] font-medium text-accent-text hover:underline" onClick={switchMode}>
              {isRegister ? '¿Ya tienes cuenta? Inicia sesión' : '¿Eres nuevo? Crea tu cuenta'}
            </button>
          )}
          {!config.passwordEnabled && (
            <button
              type="button"
              className="mt-4 block w-full text-center text-[0.82rem] text-ink-3 hover:text-ink hover:underline"
              onClick={() => {
                setAdminForm(!adminForm)
                setError('')
              }}
            >
              {adminForm ? 'Volver al acceso con Google' : 'Entrar como administrador'}
            </button>
          )}
        </form>
        {config.landingUrl && (
          <p className="mt-5 text-center text-[0.82rem] text-ink-3">
            <LegalLink to={`${config.landingUrl}/privacy`}>Política de privacidad</LegalLink>
            <span aria-hidden="true"> · </span>
            <LegalLink to={`${config.landingUrl}/terms`}>Condiciones del servicio</LegalLink>
          </p>
        )}
      </div>
    </main>
  )
}
