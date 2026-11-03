import { useState } from 'react'
import type { Database } from '../../api'
import { api as client, can } from '../../api'
import type { PageProps } from '../../App'
import { PageBody, PageHeader } from '../../components/Shell'
import { Button, ErrorNote, Loading, segmentClass, StatusBadge, useAction, useLoad } from '../../components/ui'
import { href, linkHandler, navigate, type DatabaseTab } from '../../router'
import { ArchiveDownMinimalisticIcon, CodeSquareIcon, TableIcon } from '@solar-icons/react/linear'
import { BackupsTab } from './BackupsTab'
import { DataTab } from './DataTab'
import { ConnectTab } from './ConnectTab'
import { ConsoleTab } from './ConsoleTab'
import { OverviewTab } from './OverviewTab'
import { SchemaTab } from './SchemaTab'
import { SettingsTab } from './SettingsTab'
import { UsersTab } from './UsersTab'

const tabs: { id: DatabaseTab; label: string; needsActive: boolean }[] = [
  { id: 'overview', label: 'Resumen', needsActive: false },
  { id: 'data', label: 'Datos', needsActive: true },
  { id: 'schema', label: 'Esquema', needsActive: true },
  { id: 'console', label: 'Consola SQL', needsActive: true },
  { id: 'connection', label: 'Conexión', needsActive: true },
  { id: 'users', label: 'Usuarios', needsActive: true },
  { id: 'backups', label: 'Respaldos', needsActive: false },
  { id: 'settings', label: 'Ajustes', needsActive: false },
]

export interface TabProps extends PageProps {
  db: Database
  onChange: (db: Database) => void
  reload: () => void
}

export function DatabaseDetail({ slug, tab, ...props }: PageProps & { slug: string; tab: DatabaseTab }) {
  const { data: db, error, loading, reload, setData } = useLoad(() => client.database(slug), [slug])
  const backup = useAction()
  const [backupDone, setBackupDone] = useState(false)

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
  const current = visible.some((t) => t.id === tab) ? tab : 'overview'
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
        actions={
          active &&
          can.operate(props.session.role) && (
            <>
              <Button onClick={() => navigate(href({ page: 'database', slug: db.slug, tab: 'data' }))}>
                <TableIcon size={18} aria-hidden />
                Ver datos
              </Button>
              <Button onClick={() => navigate(href({ page: 'database', slug: db.slug, tab: 'console' }))}>
                <CodeSquareIcon size={18} aria-hidden />
                Consola SQL
              </Button>
              <Button
                variant="primary"
                disabled={backup.busy}
                onClick={async () => {
                  setBackupDone(false)
                  const job = await backup.run(() => client.backup(db.slug))
                  if (job) setBackupDone(true)
                }}
              >
                <ArchiveDownMinimalisticIcon size={18} aria-hidden />
                {backup.busy ? 'Encolando…' : 'Respaldar ahora'}
              </Button>
            </>
          )
        }
      />
      <PageBody>
        {(backupDone || backup.error) && (
          <p role="status" className={`text-[1rem] ${backup.error ? 'text-danger' : 'text-accent-text'}`}>
            {backup.error || (
              <>
                Respaldo en cola. Lo verás en{' '}
                <a className="underline" href={href({ page: 'database', slug: db.slug, tab: 'backups' })} onClick={linkHandler(href({ page: 'database', slug: db.slug, tab: 'backups' }))}>
                  Respaldos
                </a>{' '}
                en unos segundos.
              </>
            )}
          </p>
        )}
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

        {current === 'overview' && <OverviewTab {...tabProps} />}
        {current === 'data' && <DataTab {...tabProps} />}
        {current === 'schema' && <SchemaTab {...tabProps} />}
        {current === 'console' && <ConsoleTab {...tabProps} />}
        {current === 'connection' && <ConnectTab {...tabProps} />}
        {current === 'users' && <UsersTab {...tabProps} />}
        {current === 'backups' && <BackupsTab {...tabProps} />}
        {current === 'settings' && <SettingsTab {...tabProps} />}
      </PageBody>
    </>
  )
}
