import { api, type Database } from '../../api'
import type { PageProps } from '../../App'
import { PageBody, PageHeader } from '../../components/Shell'
import { ErrorNote, Loading, segmentClass, StatusBadge, useLoad } from '../../components/ui'
import { href, linkHandler, type DatabaseTab } from '../../router'
import { BackupsTab } from './BackupsTab'
import { ConnectTab } from './ConnectTab'
import { ConsoleTab } from './ConsoleTab'
import { OverviewTab } from './OverviewTab'
import { SchemaTab } from './SchemaTab'
import { SettingsTab } from './SettingsTab'
import { UsersTab } from './UsersTab'

const tabs: { id: DatabaseTab; label: string; needsActive: boolean }[] = [
  { id: 'resumen', label: 'Resumen', needsActive: false },
  { id: 'esquema', label: 'Esquema', needsActive: true },
  { id: 'consola', label: 'Consola SQL', needsActive: true },
  { id: 'conexion', label: 'Conexión', needsActive: true },
  { id: 'usuarios', label: 'Usuarios', needsActive: true },
  { id: 'respaldos', label: 'Respaldos', needsActive: false },
  { id: 'ajustes', label: 'Ajustes', needsActive: false },
]

export interface TabProps extends PageProps {
  db: Database
  onChange: (db: Database) => void
  reload: () => void
}

export function DatabaseDetail({ slug, tab, ...props }: PageProps & { slug: string; tab: DatabaseTab }) {
  const { data: db, error, loading, reload, setData } = useLoad(() => api.database(slug), [slug])

  if (loading && !db) return <Loading label="Cargando base de datos…" />
  if (error || !db) {
    return (
      <PageBody>
        <div className="pt-8">
          <ErrorNote message={error || 'No encontrada.'} />
        </div>
      </PageBody>
    )
  }

  const active = db.status !== 'expirada'
  const visible = tabs.filter((t) => active || !t.needsActive)
  const current = visible.some((t) => t.id === tab) ? tab : 'resumen'
  const tabProps: TabProps = { ...props, db, onChange: setData, reload }

  return (
    <>
      <PageHeader
        crumbs={[{ label: 'Bases de datos', to: href({ page: 'databases' }) }]}
        title={
          <span className="flex flex-wrap items-center gap-3">
            {db.slug}
            <StatusBadge status={db.status} />
          </span>
        }
        subtitle={<span className="font-mono">{db.hostname || `${db.database} (modo local)`}</span>}
      />
      <PageBody>
        <nav aria-label="Secciones de la base de datos" className="overflow-x-auto">
          <ul className="inline-flex min-w-max rounded-[7px] bg-surface-2 p-0.5">
            {visible.map((t) => {
              const to = href({ page: 'database', slug: db.slug, tab: t.id })
              const isCurrent = t.id === current
              return (
                <li key={t.id}>
                  <a
                    href={to}
                    onClick={linkHandler(to)}
                    aria-current={isCurrent ? 'page' : undefined}
                    className={segmentClass(isCurrent)}
                  >
                    {t.label}
                  </a>
                </li>
              )
            })}
          </ul>
        </nav>

        {current === 'resumen' && <OverviewTab {...tabProps} />}
        {current === 'esquema' && <SchemaTab {...tabProps} />}
        {current === 'consola' && <ConsoleTab {...tabProps} />}
        {current === 'conexion' && <ConnectTab {...tabProps} />}
        {current === 'usuarios' && <UsersTab {...tabProps} />}
        {current === 'respaldos' && <BackupsTab {...tabProps} />}
        {current === 'ajustes' && <SettingsTab {...tabProps} />}
      </PageBody>
    </>
  )
}
