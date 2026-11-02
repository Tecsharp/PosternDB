import { useState, type FormEvent } from 'react'
import { DownloadIcon } from '@solar-icons/react/linear'
import { api, can, type Credentials, type Job } from '../api'
import type { PageProps } from '../App'
import { CredentialsSheet } from '../components/CredentialsSheet'
import { PageBody, PageHeader } from '../components/Shell'
import { Button, EmptyState, ErrorNote, Loading, Sheet, SheetFooter, SheetHeader, useAction, useLoad } from '../components/ui'
import { formatBytes, formatDateTime, normalizeName, slugProblem } from '../format'
import { href, linkHandler } from '../router'

const reasonText: Record<string, string> = { manual: 'Manual', expiry: 'Al expirar', deletion: 'Al eliminar' }

export function Backups({ session, settings }: PageProps) {
  const { data, error, loading, reload } = useLoad(api.backups, [])
  const [restoring, setRestoring] = useState<Job | null>(null)
  const [removing, setRemoving] = useState<Job | null>(null)
  const [creds, setCreds] = useState<Credentials | null>(null)
  const action = useAction()
  const operate = can.operate(session.role)

  return (
    <>
      <PageHeader
        title="Respaldos"
        subtitle={settings.backupBeforeDelete ? 'Las bases se respaldan antes de borrarse al expirar o eliminarse.' : 'BACKUP_BEFORE_DELETE=false: solo hay respaldos manuales.'}
      />
      <PageBody>
        <section className="group-box">
          {loading && !data && <Loading label="Cargando respaldos…" />}
          {(error || action.error) && <div className="p-5"><ErrorNote message={error || action.error} /></div>}
          {data && data.length === 0 && <EmptyState title="Sin respaldos todavía">Crea uno desde la pestaña Respaldos de cualquier base.</EmptyState>}
          {data && data.length > 0 && (
            <table className="w-full text-left text-[13px]">
              <caption className="sr-only">Respaldos disponibles</caption>
              <thead className="max-md:sr-only">
                <tr className="text-[12px] text-ink-2">
                  {['Archivo', 'Base', 'Origen', 'Tamaño', 'Fecha', ''].map((h, i) => (
                    <th key={i} scope="col" className="border-b border-line px-5 py-2.5 font-medium">
                      {h || <span className="sr-only">Acciones</span>}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {data.map((b) => (
                  <tr key={b.id} className="hover:bg-surface-2 max-md:flex max-md:flex-col max-md:gap-1 max-md:border-b max-md:border-line max-md:px-5 max-md:py-3">
                    <td className="border-b border-line px-5 py-3 font-mono text-[12.5px] max-md:border-0 max-md:p-0">{b.file}</td>
                    <td className="border-b border-line px-5 py-3 max-md:border-0 max-md:p-0">
                      {b.slug ? (
                        <a className="hover:underline" href={href({ page: 'database', slug: b.slug, tab: 'respaldos' })} onClick={linkHandler(href({ page: 'database', slug: b.slug, tab: 'respaldos' }))}>
                          {b.slug}
                        </a>
                      ) : (
                        '—'
                      )}
                    </td>
                    <td className="border-b border-line px-5 py-3 text-ink-2 max-md:border-0 max-md:p-0">{reasonText[b.reason ?? ''] ?? b.reason}</td>
                    <td className="border-b border-line px-5 py-3 font-mono text-ink-2 tabular-nums max-md:border-0 max-md:p-0">{b.bytes !== undefined ? formatBytes(b.bytes) : '—'}</td>
                    <td className="border-b border-line px-5 py-3 whitespace-nowrap text-ink-2 max-md:border-0 max-md:p-0">{formatDateTime(b.createdAt)}</td>
                    <td className="border-b border-line px-5 py-3 text-right whitespace-nowrap max-md:border-0 max-md:p-0 max-md:text-left">
                      {operate && (
                        <span className="inline-flex gap-1">
                          <a className="inline-flex h-6 items-center rounded-[6px] px-1.5 text-ink-2 hover:bg-surface-2 hover:text-ink" href={api.backupDownloadUrl(b.id)} download aria-label={`Descargar ${b.file}`}>
                            <DownloadIcon size={16} aria-hidden />
                          </a>
                          <Button size="sm" onClick={() => setRestoring(b)}>
                            Restaurar
                          </Button>
                          <Button size="sm" variant="ghost" className="hover:text-danger" onClick={() => setRemoving(b)}>
                            Borrar
                          </Button>
                        </span>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </section>
      </PageBody>

      <RestoreSheet
        backup={restoring}
        onClose={() => setRestoring(null)}
        onRestored={(c) => {
          setRestoring(null)
          setCreds(c)
        }}
      />
      <CredentialsSheet credentials={creds} title={creds ? `${creds.database} se está restaurando` : ''} onClose={() => setCreds(null)} />
      <Sheet open={removing !== null} onClose={() => setRemoving(null)} labelledBy="rm-backup">
        {removing && (
          <>
            <SheetHeader id="rm-backup" title="¿Borrar este respaldo?" subtitle={<span className="font-mono">{removing.file}</span>} />
            <SheetFooter>
              <Button onClick={() => setRemoving(null)}>Cancelar</Button>
              <Button
                variant="danger"
                disabled={action.busy}
                onClick={async () => {
                  await action.run(() => api.deleteBackup(removing.id))
                  setRemoving(null)
                  reload()
                }}
              >
                Borrar respaldo
              </Button>
            </SheetFooter>
          </>
        )}
      </Sheet>
    </>
  )
}

export function RestoreSheet({ backup, onClose, onRestored }: { backup: Job | null; onClose: () => void; onRestored: (c: Credentials) => void }) {
  const [raw, setRaw] = useState('')
  const { busy, error, run } = useAction()
  const name = normalizeName(raw)
  const problem = slugProblem(name)

  async function submit(e: FormEvent) {
    e.preventDefault()
    if (!backup || problem || !name) return
    const c = await run(() => api.restoreBackup(backup.id, name))
    if (c) {
      setRaw('')
      onRestored(c)
    }
  }

  return (
    <Sheet open={backup !== null} onClose={onClose} labelledBy="restore-title" dismissable={!busy}>
      {backup && (
        <form onSubmit={submit}>
          <SheetHeader id="restore-title" title="Restaurar como base nueva" subtitle={<>A partir de <span className="font-mono">{backup.file}</span>. La base original no se toca.</>} />
          <div className="space-y-4 px-6 py-5">
            <label className="block">
              <span className="label">Nombre de la base nueva</span>
              <input className="field font-mono" autoFocus required placeholder={`${backup.slug || 'cliente'}-restaurada`} value={raw} aria-invalid={problem ? 'true' : 'false'} onChange={(e) => setRaw(e.target.value)} />
              {problem && <span className="mt-1.5 block text-[12px] text-danger">{problem}</span>}
            </label>
            {error && <ErrorNote message={error} />}
          </div>
          <SheetFooter>
            <Button onClick={onClose} disabled={busy}>
              Cancelar
            </Button>
            <Button type="submit" variant="primary" disabled={busy || !name || !!problem}>
              {busy ? 'Creando…' : 'Restaurar'}
            </Button>
          </SheetFooter>
        </form>
      )}
    </Sheet>
  )
}
