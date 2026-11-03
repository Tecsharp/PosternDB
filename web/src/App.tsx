import { useCallback, useEffect, useState, type FormEvent } from 'react'
import { api, me, type Session, type Settings } from './api'
import { ClientShell, Shell } from './components/Shell'
import { Button, ErrorNote, Loading, Sheet, SheetFooter, SheetHeader, useAction } from './components/ui'
import { Activity } from './pages/Activity'
import { Admins } from './pages/Admins'
import { Backups } from './pages/Backups'
import { Clients } from './pages/Clients'
import { Expiry } from './pages/Expiry'
import { DatabaseDetail } from './pages/db/DatabaseDetail'
import { Databases } from './pages/Databases'
import { Login } from './pages/Login'
import { MyDatabaseDetail } from './pages/me/MyDatabaseDetail'
import { MyDatabases } from './pages/me/MyDatabases'
import { SettingsPage } from './pages/SettingsPage'
import { Templates } from './pages/Templates'
import { useRoute } from './router'

export default function App() {
  const [session, setSession] = useState<Session | null | undefined>(undefined)
  const loggedOut = useCallback(() => setSession(null), [])

  useEffect(() => {
    api
      .session()
      .then(setSession)
      .catch(() => setSession(null))
  }, [])

  if (session === undefined) return <Loading label="Cargando…" />
  if (session === null) return <Login onLoggedIn={setSession} />
  if (session.role === 'client') return <ClientWorkspace session={session} onLoggedOut={loggedOut} />
  return <Workspace session={session} onLoggedOut={loggedOut} />
}

/**
 * The reduced "panel de usuario" a self-service client lands on: only their
 * own databases, browsing and editing data, never the SQL console and never
 * another client's data. Deliberately not the admin Shell/nav.
 */
function ClientWorkspace({ session, onLoggedOut }: { session: Session; onLoggedOut: () => void }) {
  const route = useRoute()
  const [changingPassword, setChangingPassword] = useState(false)

  async function logout() {
    await api.logout().catch(() => undefined)
    onLoggedOut()
  }

  return (
    <ClientShell session={session} onLogout={logout} onChangePassword={() => setChangingPassword(true)}>
      {route.page === 'my-database' ? <MyDatabaseDetail key={route.slug} slug={route.slug} tab={route.tab} /> : <MyDatabases />}
      <ChangePasswordSheet open={changingPassword} onClose={() => setChangingPassword(false)} changePassword={me.changePassword} />
    </ClientShell>
  )
}

export interface PageProps {
  session: Session
  settings: Settings
}

function Workspace({ session, onLoggedOut }: { session: Session; onLoggedOut: () => void }) {
  const route = useRoute()
  const [settings, setSettings] = useState<Settings | null>(null)
  const [changingPassword, setChangingPassword] = useState(false)

  useEffect(() => {
    api.settings().then(setSettings).catch(onLoggedOut)
  }, [onLoggedOut])

  async function logout() {
    await api.logout().catch(() => undefined)
    onLoggedOut()
  }

  if (!settings) return <Loading label="Cargando…" />
  const props: PageProps = { session, settings }

  return (
    <Shell route={route} session={session} onLogout={logout} onChangePassword={() => setChangingPassword(true)}>
      {route.page === 'databases' && <Databases {...props} />}
      {route.page === 'database' && <DatabaseDetail key={route.slug} {...props} slug={route.slug} tab={route.tab} />}
      {route.page === 'backups' && <Backups {...props} />}
      {route.page === 'templates' && <Templates {...props} />}
      {route.page === 'activity' && <Activity />}
      {route.page === 'admins' && <Admins {...props} />}
      {route.page === 'clients' && <Clients {...props} />}
      {route.page === 'expiry' && <Expiry {...props} />}
      {route.page === 'settings' && <SettingsPage {...props} />}
      <ChangePasswordSheet open={changingPassword} onClose={() => setChangingPassword(false)} changePassword={api.changePassword} />
    </Shell>
  )
}

function ChangePasswordSheet({
  open,
  onClose,
  changePassword,
}: {
  open: boolean
  onClose: () => void
  changePassword: (current: string, next: string) => Promise<void>
}) {
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [done, setDone] = useState(false)
  const { busy, error, run } = useAction()

  function close() {
    setCurrent('')
    setNext('')
    setDone(false)
    onClose()
  }

  async function submit(e: FormEvent) {
    e.preventDefault()
    const ok = await run(() => changePassword(current, next).then(() => true))
    if (ok) setDone(true)
  }

  return (
    <Sheet open={open} onClose={close} labelledBy="pw-title">
      <form onSubmit={submit}>
        <SheetHeader id="pw-title" title="Cambiar mi contraseña" subtitle="Mínimo 12 caracteres." />
        <div className="space-y-4 px-6 py-5">
          {done ? (
            <p role="status" className="text-[0.95rem] text-accent-text">
              Contraseña actualizada.
            </p>
          ) : (
            <>
              <label className="block">
                <span className="label">Contraseña actual</span>
                <input className="field" type="password" autoComplete="current-password" required value={current} onChange={(e) => setCurrent(e.target.value)} />
              </label>
              <label className="block">
                <span className="label">Contraseña nueva</span>
                <input className="field" type="password" autoComplete="new-password" minLength={12} required value={next} onChange={(e) => setNext(e.target.value)} />
              </label>
              {error && <ErrorNote message={error} />}
            </>
          )}
        </div>
        <SheetFooter>
          <Button onClick={close}>{done ? 'Cerrar' : 'Cancelar'}</Button>
          {!done && (
            <Button type="submit" variant="primary" disabled={busy}>
              {busy ? 'Guardando…' : 'Cambiar contraseña'}
            </Button>
          )}
        </SheetFooter>
      </form>
    </Sheet>
  )
}
