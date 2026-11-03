import { useCallback, useEffect, useState } from 'react'
import { api, type Session, type Settings } from './api'
import { ChangePasswordSheet } from './components/ChangePasswordSheet'
import { SessionContext } from './components/SessionContext'
import { clientGroups, Shell } from './components/Shell'
import { Loading } from './components/ui'
import { Activity } from './pages/Activity'
import { Admins } from './pages/Admins'
import { Backups } from './pages/Backups'
import { Clients } from './pages/Clients'
import { Expiry } from './pages/Expiry'
import { DatabaseDetail } from './pages/db/DatabaseDetail'
import { Databases } from './pages/Databases'
import { Help } from './pages/Help'
import { Login } from './pages/Login'
import { MyActivity } from './pages/me/MyActivity'
import { MyBackups } from './pages/me/MyBackups'
import { MyDatabaseDetail } from './pages/me/MyDatabaseDetail'
import { MyDatabases } from './pages/me/MyDatabases'
import { MySecurity } from './pages/me/MySecurity'
import { MySettings } from './pages/me/MySettings'
import { UsernameSheet } from './pages/me/UsernameSheet'
import { SettingsPage } from './pages/SettingsPage'
import { Templates } from './pages/Templates'
import { href, navigate, useRoute } from './router'

export default function App() {
  const route = useRoute()
  const [session, setSession] = useState<Session | null | undefined>(undefined)
  const loggedOut = useCallback(() => setSession(null), [])
  const refresh = useCallback(async () => setSession(await api.session()), [])

  useEffect(() => {
    api
      .session()
      .then(setSession)
      .catch(() => setSession(null))
  }, [])

  // The tunnel tutorial is public: it opens in a new tab, signed in or not.
  if (route.page === 'help') return <Help />
  if (session === undefined) return <Loading label="Cargando…" />
  if (session === null) return <Login onLoggedIn={setSession} />
  if (session.role === 'client') {
    return (
      <SessionContext.Provider value={{ session, refresh }}>
        <ClientWorkspace session={session} onLoggedOut={loggedOut} />
      </SessionContext.Provider>
    )
  }
  return <Workspace session={session} onLoggedOut={loggedOut} />
}

const clientPages = ['my-database', 'my-backups', 'my-activity', 'my-security', 'my-settings']

/**
 * The reduced "panel de usuario": only their own databases, backups, activity
 * and account — never the SQL console and never another user's data.
 */
function ClientWorkspace({ session, onLoggedOut }: { session: Session; onLoggedOut: () => void }) {
  const route = useRoute()

  async function logout() {
    await api.logout().catch(() => undefined)
    onLoggedOut()
  }

  return (
    <Shell route={route} session={session} groups={clientGroups} onLogout={logout} onChangePassword={() => navigate(href({ page: 'my-security' }))}>
      {route.page === 'my-database' && <MyDatabaseDetail key={route.slug} slug={route.slug} tab={route.tab} />}
      {route.page === 'my-backups' && <MyBackups />}
      {route.page === 'my-activity' && <MyActivity />}
      {route.page === 'my-security' && <MySecurity />}
      {route.page === 'my-settings' && <MySettings />}
      {!clientPages.includes(route.page) && <MyDatabases />}
      <UsernameSheet open={session.needsUsername === true} />
    </Shell>
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
