import { useEffect, useState } from 'react'
import { ArchiveDownMinimalisticIcon, DownloadIcon } from '@solar-icons/react/linear'
import { api, can, type Credentials, type Job } from '../../api'
import { RestoreSheet } from '../Backups'
import { CredentialsSheet } from '../../components/CredentialsSheet'
import { Button, EmptyState, ErrorNote, Panel, Tag, useAction, useLoad } from '../../components/ui'
import { formatBytes, formatDateTime } from '../../format'
import type { TabProps } from './DatabaseDetail'

const kindText: Record<Job['kind'], string> = {
  dump: 'Respaldo manual',
  archive: 'Respaldo al retirar',
  restore: 'Restauración',
  clone: 'Clonación',
  delete_file: 'Borrado de respaldo',
}

const statusTone: Record<Job['status'], 'neutral' | 'accent' | 'warn' | 'danger'> = {
  pending: 'neutral',
  running: 'warn',
  done: 'accent',
  failed: 'danger',
}
const statusText: Record<Job['status'], string> = { pending: 'En cola', running: 'En curso', done: 'Listo', failed: 'Falló' }

export function JobsList({ jobs }: { jobs: Job[] }) {
  return (
    <ul className="group-rows">
      {jobs.map((j) => (
        <li key={j.id} className="flex flex-wrap items-center gap-3 px-5 py-3 text-[13px]">
          <Tag tone={statusTone[j.status]}>{statusText[j.status]}</Tag>
          <span className="font-medium">{kindText[j.kind]}</span>
          {j.file && <span className="truncate font-mono text-[12px] text-ink-2">{j.file}</span>}
          <span className="ml-auto text-[12px] text-ink-3">
            {formatDateTime(j.createdAt)} por {j.requestedBy}
          </span>
          {j.error && <p className="w-full font-mono text-[12px] text-danger">{j.error}</p>}
        </li>
      ))}
    </ul>
  )
}

export function BackupsTab({ db, session }: TabProps) {
  const backups = useLoad(() => api.databaseBackups(db.slug), [db.slug])
  const jobs = useLoad(() => api.jobs(db.slug), [db.slug])
  const action = useAction()
  const [restoring, setRestoring] = useState<Job | null>(null)
  const [creds, setCreds] = useState<Credentials | null>(null)
  const operate = can.operate(session.role)
  const pending = (jobs.data ?? []).some((j) => j.status === 'pending' || j.status === 'running')

  // Poll while the sidecar is working so the operator sees it finish.
  const reloadJobs = jobs.reload
  const reloadBackups = backups.reload
  useEffect(() => {
    if (!pending) return
    const t = setInterval(() => {
      reloadJobs()
      reloadBackups()
    }, 3000)
    return () => clearInterval(t)
  }, [pending, reloadJobs, reloadBackups])

  return (
    <div className="space-y-6">
      <Panel
        title="Respaldos guardados"
        actions={
          operate &&
          db.status !== 'expirada' && (
            <Button
              size="sm"
              variant="primary"
              disabled={action.busy}
              onClick={async () => {
                await action.run(() => api.backup(db.slug))
                jobs.reload()
              }}
            >
              <ArchiveDownMinimalisticIcon size={15} aria-hidden />
              {action.busy ? 'Encolando…' : 'Respaldar ahora'}
            </Button>
          )
        }
      >
        {action.error && <div className="p-5"><ErrorNote message={action.error} /></div>}
        {backups.data && backups.data.length === 0 && (
          <EmptyState title="Sin respaldos">Los respaldos se guardan en el volumen "backups" del servidor y se pueden restaurar como una base nueva.</EmptyState>
        )}
        {backups.data && backups.data.length > 0 && (
          <ul className="group-rows">
            {backups.data.map((b) => (
              <li key={b.id} className="flex flex-wrap items-center gap-3 px-5 py-3 text-[13px]">
                <div className="min-w-0 flex-1">
                  <p className="truncate font-mono text-[12.5px] font-semibold">{b.file}</p>
                  <p className="text-[12px] text-ink-2">
                    {formatDateTime(b.createdAt)}, {b.bytes !== undefined ? formatBytes(b.bytes) : 'tamaño desconocido'}, {kindText[b.kind].toLowerCase()}
                  </p>
                </div>
                {operate && (
                  <>
                    <a className="inline-flex h-6 items-center gap-1.5 rounded-[6px] px-2.5 text-[12px] font-medium text-ink-2 hover:bg-surface-2 hover:text-ink" href={api.backupDownloadUrl(b.id)} download>
                      <DownloadIcon size={15} aria-hidden />
                      Descargar
                    </a>
                    <Button size="sm" onClick={() => setRestoring(b)}>
                      Restaurar como nueva
                    </Button>
                  </>
                )}
              </li>
            ))}
          </ul>
        )}
      </Panel>

      <Panel title="Historial de trabajos">
        {jobs.data && jobs.data.length > 0 ? <JobsList jobs={jobs.data} /> : <p className="px-5 py-6 text-[13px] text-ink-2">Sin trabajos todavía.</p>}
      </Panel>

      <RestoreSheet
        backup={restoring}
        onClose={() => setRestoring(null)}
        onRestored={(c) => {
          setRestoring(null)
          setCreds(c)
        }}
      />
      <CredentialsSheet credentials={creds} title={creds ? `${creds.database} se está restaurando` : ''} onClose={() => setCreds(null)} />
    </div>
  )
}
