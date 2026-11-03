import { useEffect, useState, type FormEvent } from 'react'
import { api, type Session } from '../api'
import { Logo } from '../components/Shell'
import { Button, ErrorNote, errorMessage } from '../components/ui'

export function Login({ onLoggedIn }: { onLoggedIn: (s: Session) => void }) {
  const [mode, setMode] = useState<'login' | 'register'>('login')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [canRegister, setCanRegister] = useState(true)
  const isRegister = mode === 'register'

  useEffect(() => {
    api
      .publicConfig()
      .then((c) => setCanRegister(c.allowSelfRegister))
      .catch(() => undefined)
  }, [])

  async function submit(e: FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      if (isRegister) await api.register(username, password)
      else await api.login(username, password)
      onLoggedIn(await api.session())
    } catch (err) {
      setError(errorMessage(err))
      setBusy(false)
    }
  }

  return (
    <main className="grid min-h-dvh place-items-center px-4">
      <div className="w-full max-w-[380px]">
        <div className="mb-8 text-center">
          <Logo size="lg" />
          <p className="mt-3 text-[0.95rem] text-ink-2">Bases de datos PostgreSQL publicadas por Cloudflare Tunnel.</p>
        </div>
        <form onSubmit={submit} className="group-box px-6 py-6" aria-labelledby="login-title">
          <h1 id="login-title" className="text-[1.15rem] font-semibold">
            {isRegister ? 'Crear cuenta' : 'Iniciar sesión'}
          </h1>
          <div className="mt-5 space-y-4">
            <label className="block">
              <span className="label">Usuario</span>
              <input
                className="field"
                autoComplete="username"
                autoCapitalize="none"
                autoCorrect="off"
                spellCheck={false}
                autoFocus
                required
                value={username}
                onChange={(e) => setUsername(e.target.value)}
              />
            </label>
            <label className="block">
              <span className="label">Contraseña</span>
              <input
                className="field"
                type="password"
                autoComplete={isRegister ? 'new-password' : 'current-password'}
                minLength={isRegister ? 12 : undefined}
                required
                value={password}
                onChange={(e) => setPassword(e.target.value)}
              />
              {isRegister && <span className="mt-1.5 block text-[0.85rem] text-ink-2">Mínimo 12 caracteres.</span>}
            </label>
          </div>
          {error && (
            <div className="mt-4">
              <ErrorNote message={error} />
            </div>
          )}
          <Button type="submit" variant="primary" className="mt-6 w-full" disabled={busy}>
            {busy ? (isRegister ? 'Creando cuenta…' : 'Entrando…') : isRegister ? 'Crear cuenta' : 'Entrar'}
          </Button>
          {canRegister && <button
            type="button"
            className="mt-4 block w-full text-center text-[0.85rem] font-medium text-accent-text hover:underline"
            onClick={() => {
              setMode(isRegister ? 'login' : 'register')
              setError('')
            }}
          >
            {isRegister ? '¿Ya tienes cuenta? Inicia sesión' : '¿Eres nuevo? Crea tu cuenta'}
          </button>}
          {!isRegister && <p className="mt-4 text-center text-[0.85rem] text-ink-3">El administrador principal está en ADMIN_USER y ADMIN_PASSWORD de tu .env.</p>}
        </form>
      </div>
    </main>
  )
}
