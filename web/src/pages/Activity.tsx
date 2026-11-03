import { useMemo, useState, type ReactNode } from 'react'
import { api, type AuditEntry, type ClientUser } from '../api'
import { PageBody, PageHeader } from '../components/Shell'
import { Button, EmptyState, ErrorNote, Loading, Sheet, SheetFooter, SheetHeader, useLoad } from '../components/ui'
import { formatDateTime } from '../format'

export const actionText: Record<string, string> = {
  db_created: 'Base de datos creada',
  db_deleted: 'Base de datos eliminada',
  db_expired: 'Expiró por inactividad',
  db_archived_for_backup: 'Archivada para respaldo',
  db_dropped: 'Datos borrados',
  route_removed: 'Host retirado de Cloudflare',
  activity_detected: 'Actividad detectada',
  inactivity_audit_run: 'Revisión de inactividad',
  login_success: 'Inicio de sesión',
  login_failed: 'Inicio de sesión fallido',
  hostname_migrated: 'Host migrado',
  provision_failed: 'Creación fallida y revertida',
  backup_completed: 'Respaldo guardado',
  backup_failed: 'Respaldo fallido',
  backup_requested: 'Respaldo solicitado',
  backup_deleted: 'Respaldo borrado',
  restore_requested: 'Restauración solicitada',
  clone_requested: 'Clonación solicitada',
  password_reset: 'Contraseña regenerada',
  expiry_changed: 'Expiración cambiada',
  expiry_warning_sent: 'Aviso de expiración enviado',
  notification_failed: 'Notificación fallida',
  db_user_created: 'Usuario de base creado',
  db_user_deleted: 'Usuario de base eliminado',
  template_saved: 'Plantilla guardada',
  template_deleted: 'Plantilla eliminada',
  role_changed: 'Rol cambiado',
  admin_created: 'Administrador creado',
  admin_updated: 'Administrador modificado',
  admin_deleted: 'Administrador eliminado',
  query_executed: 'Consulta en la consola',
  client_registered: 'Cuenta creada',
  client_created: 'Cuenta creada por el administrador',
  client_deleted: 'Cuenta eliminada',
  client_expiry_changed: 'Vigencia de la cuenta cambiada',
  client_password_changed: 'Contraseña cambiada',
  client_password_reset: 'Contraseña restablecida por el administrador',
  client_google_linked: 'Google vinculado',
  client_username_changed: 'Nombre de usuario cambiado',
}

export const alerting = new Set(['login_failed', 'provision_failed', 'backup_failed', 'db_expired', 'notification_failed'])

type Filter = 'all' | 'databases' | 'security' | 'auditor'
const filters: { id: Filter; label: string; match: (e: AuditEntry) => boolean }[] = [
  { id: 'all', label: 'Todo', match: () => true },
  { id: 'databases', label: 'Bases de datos', match: (e) => !e.action.startsWith('login') && !e.action.startsWith('admin') && e.actor !== 'inactivity-auditor' },
  { id: 'security', label: 'Accesos', match: (e) => e.action.startsWith('login') || e.action.startsWith('admin') || e.action.startsWith('role') || e.action === 'password_reset' },
  { id: 'auditor', label: 'Auditor', match: (e) => e.actor === 'inactivity-auditor' || e.actor === 'backup-sidecar' },
]

export function summary(e: AuditEntry): string {
  const d = e.details ?? {}
  switch (e.action) {
    case 'inactivity_audit_run':
      return `${d.checked ?? 0} revisadas, ${d.activeSeen ?? 0} con actividad, ${d.expired ?? 0} expiradas`
    case 'activity_detected':
      return `${d.openConnections ?? 0} conexiones abiertas, ${d.newSessions ?? 0} sesiones nuevas`
    case 'hostname_migrated':
      return `${d.from} a ${d.to}`
    case 'query_executed':
      return String(d.sql ?? '')
    case 'provision_failed':
      return String(d.error ?? '')
    case 'login_success':
    case 'login_failed':
      return d.ip ? `desde ${d.ip}${d.method === 'google' ? ' con Google' : ''}` : ''
    case 'client_username_changed':
      return d.from ? `antes ${d.from}` : ''
    default:
      return String(d.file ?? d.hostname ?? d.username ?? '')
  }
}

type Scope = 'me' | 'all'

/** "Actividad": the signed-in person's own feed first; the whole platform's log on the next tab. */
export function Activity() {
  const [scope, setScope] = useState<Scope>('me')
  const tabs: { id: Scope; label: string }[] = [
    { id: 'me', label: 'Mi actividad' },
    { id: 'all', label: 'Toda la plataforma' },
  ]
  return (
    <ActivityFeed
      key={scope}
      title="Actividad"
      subtitle={scope === 'me' ? 'Lo que hiciste tú y lo que pasó con tu cuenta.' : 'Los últimos 100 eventos registrados en audit_log.'}
      load={scope === 'me' ? api.myActivity : api.audit}
      filtered={scope === 'all'}
      header={
        <div role="tablist" aria-label="Qué actividad ver" className="mb-4 flex gap-1.5">
          {tabs.map((t) => (
            <Button key={t.id} role="tab" size="sm" variant={scope === t.id ? 'secondary' : 'ghost'} aria-selected={scope === t.id} onClick={() => setScope(t.id)}>
              {t.label}
            </Button>
          ))}
        </div>
      }
    />
  )
}

/** The activity of one person, user or administrator, for whoever manages them. */
export function PersonActivitySheet({ person, onClose }: { person: ClientUser | null; onClose: () => void }) {
  return (
    <Sheet open={person !== null} onClose={onClose} labelledBy="person-activity">
      {person && (
        <>
          <SheetHeader id="person-activity" title={`Actividad de ${person.fullName || person.username}`} subtitle="Lo que hizo y lo que pasó con su cuenta y sus bases." />
          <div className="max-h-[60vh] overflow-y-auto">
            <ActivityRows load={() => api.clientActivity(person.id)} deps={[person.id]} />
          </div>
          <SheetFooter>
            <Button onClick={onClose}>Cerrar</Button>
          </SheetFooter>
        </>
      )}
    </Sheet>
  )
}

function ActivityRows({ load, deps, filter = 'all' }: { load: () => Promise<AuditEntry[]>; deps: unknown[]; filter?: Filter }) {
  const { data, error, loading } = useLoad(load, deps)
  const rows = useMemo(() => (data ?? []).filter(filters.find((f) => f.id === filter)!.match), [data, filter])
  return (
    <>
      {loading && !data && <Loading label="Cargando actividad…" />}
      {error && <div className="p-5"><ErrorNote message={error} /></div>}
      {data && rows.length === 0 && <EmptyState title="Sin eventos" />}
      <EntryList rows={rows} />
    </>
  )
}

function EntryList({ rows }: { rows: AuditEntry[] }) {
  return (
    <ol className="group-rows">
      {rows.map((e) => (
        <li key={e.id} className="grid grid-cols-[130px_1fr] gap-x-4 px-5 py-3 text-[0.95rem] max-sm:grid-cols-1">
          <time dateTime={e.occurredAt} className="text-ink-2 tabular-nums">
            {formatDateTime(e.occurredAt)}
          </time>
          <div className="min-w-0">
            <p>
              <span className={alerting.has(e.action) ? 'font-semibold text-warn' : 'font-semibold'}>{actionText[e.action] ?? e.action}</span>
              {e.target && <span className="ml-2 font-mono text-[0.85rem] text-ink-2">{e.target}</span>}
            </p>
            <p className="mt-0.5 truncate text-[0.85rem] text-ink-2">{[summary(e), `por ${e.actor}`].filter(Boolean).join('. ')}</p>
          </div>
        </li>
      ))}
    </ol>
  )
}

function ActivityFeed({
  title,
  subtitle,
  load,
  filtered,
  header,
}: {
  title: string
  subtitle: string
  load: () => Promise<AuditEntry[]>
  filtered: boolean
  header: ReactNode
}) {
  const { data, error, loading, reload } = useLoad(load, [])
  const [filter, setFilter] = useState<Filter>('all')
  const rows = useMemo(() => (data ?? []).filter(filters.find((f) => f.id === filter)!.match), [data, filter])

  return (
    <>
      <PageHeader title={title} subtitle={subtitle} actions={<Button onClick={reload}>Actualizar</Button>} />
      <PageBody>
        {header}
        {filtered && (
          <div role="group" aria-label="Filtrar eventos" className="mb-4 flex flex-wrap gap-1.5">
            {filters.map((f) => (
              <Button key={f.id} size="sm" variant={filter === f.id ? 'secondary' : 'ghost'} aria-pressed={filter === f.id} onClick={() => setFilter(f.id)}>
                {f.label}
              </Button>
            ))}
          </div>
        )}
        <section className="group-box">
          {loading && !data && <Loading label="Cargando actividad…" />}
          {error && <div className="p-5"><ErrorNote message={error} /></div>}
          {data && rows.length === 0 && <EmptyState title="Sin eventos" />}
          <EntryList rows={rows} />
        </section>
      </PageBody>
    </>
  )
}
