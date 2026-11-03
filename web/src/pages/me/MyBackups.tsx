import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react'
import { DownloadMinimalisticIcon, RestartIcon, TrashBinMinimalisticIcon } from '@solar-icons/react/linear'
import { me, type Account, type Credentials, type Job } from '../../api'
import { CredentialsSheet } from '../../components/CredentialsSheet'
import { PageBody, PageHeader } from '../../components/Shell'
import { Button, ButtonLink, EmptyState, ErrorNote, Loading, Notice, Panel, Sheet, SheetFooter, SheetHeader, Tag, useAction, useLoad } from '../../components/ui'
import { databasePreview, formatBytes, formatRelative, nameRoom, normalizeName, slugProblem } from '../../format'

const POLL_MS = 4000
const POLL_TIMES = 15
const MAX_PER_DATABASE = 5

/** The user's own backups: take one, download it, restore it as a new database, delete it. */
export function MyBackups() {
  const backups = useLoad(me.backups, [])
  const databases = useLoad(me.databases, [])
  const account = useLoad(me.account, [])
  const [slug, setSlug] = useState('')
  const [waiting, setWaiting] = useState(false)
  const [restoring, setRestoring] = useState<Job | null>(null)
  const [removing, setRemoving] = useState<Job | null>(null)
  const [creds, setCreds] = useState<Credentials | null>(null)
  const create = useAction()
  const remove = useAction()
  const reload = backups.reload
  const polls = useRef(0)

  const active = (databases.data ?? []).filter((d) => d.status !== 'expirada')
  const chosen = slug || active[0]?.slug || ''
  const kept = (backups.data ?? []).filter((b) => b.slug === chosen).length

  // A backup runs in the background; check for it every few seconds for a minute.
  useEffect(() => {
    if (!waiting) return
    polls.current = 0
    const timer = setInterval(() => {
      polls.current += 1
      reload()
      if (polls.current >= POLL_TIMES) setWaiting(false)
    }, POLL_MS)
    return () => clearInterval(timer)
  }, [waiting, reload])

  const before = useRef(0)
  useEffect(() => {
    if (waiting && (backups.data?.length ?? 0) > before.current) setWaiting(false)
  }, [backups.data, waiting])

  async function backupNow() {
    before.current = backups.data?.length ?? 0
    const job = await create.run(() => me.createBackup(chosen))
    if (job) setWaiting(true)
  }

  const onRestored = useCallback((c: Credentials) => {
    setRestoring(null)
    setCreds(c)
    databases.reload()
  }, [databases])

  return (
    <>
      <PageHeader title="Respaldos" subtitle={`Copias de tus bases de datos. Puedes guardar hasta ${MAX_PER_DATABASE} por base.`} />
      <PageBody>
        <Panel title="Crear un respaldo">
          <div className="flex flex-wrap items-end gap-3 px-5 py-4">
            <label className="block">
              <span className="label">Base de datos</span>
              <select className="field w-64" value={chosen} onChange={(e) => setSlug(e.target.value)} disabled={active.length === 0}>
                {active.length === 0 && <option value="">Aún no tienes bases</option>}
                {active.map((d) => (
                  <option key={d.slug} value={d.slug}>
                    {d.slug}
                  </option>
                ))}
              </select>
            </label>
            <Button variant="primary" onClick={backupNow} disabled={!chosen || create.busy || waiting || kept >= MAX_PER_DATABASE}>
              {create.busy ? 'Enviando…' : waiting ? 'Respaldando…' : 'Respaldar ahora'}
            </Button>
            {chosen && (
              <span className="pb-2 text-[0.85rem] text-ink-2">
                {kept} de {MAX_PER_DATABASE} guardados en esta base
              </span>
            )}
          </div>
          {kept >= MAX_PER_DATABASE && <p className="px-5 pb-4 text-[0.9rem] text-warn">Ya tienes el máximo en esta base: elimina uno para crear otro.</p>}
          {create.error && (
            <div className="px-5 pb-4">
              <ErrorNote message={create.error} />
            </div>
          )}
          {waiting && (
            <div className="px-5 pb-4">
              <Notice tone="info">El respaldo está en curso. Aparecerá en la lista en unos segundos.</Notice>
            </div>
          )}
        </Panel>

        <Panel title="Tus respaldos">
          {backups.loading && !backups.data && <Loading label="Cargando respaldos…" />}
          {backups.error && (
            <div className="p-5">
              <ErrorNote message={backups.error} />
            </div>
          )}
          {backups.data && backups.data.length === 0 && (
            <EmptyState title="Todavía no tienes respaldos">Crea el primero arriba: es una copia de todo lo que hay en tu base ahora mismo.</EmptyState>
          )}
          {remove.error && (
            <div className="p-5">
              <ErrorNote message={remove.error} />
            </div>
          )}
          {backups.data && backups.data.length > 0 && (
            <div className="overflow-x-auto">
              <table className="w-full text-left text-[0.95rem]">
                <thead>
                  <tr className="text-[0.85rem] text-ink-2">
                    <th scope="col" className="px-4 py-2 font-medium">Base</th>
                    <th scope="col" className="px-4 py-2 font-medium">Creado</th>
                    <th scope="col" className="px-4 py-2 text-right font-medium">Tamaño</th>
                    <th scope="col" className="px-4 py-2 text-right font-medium">Acciones</th>
                  </tr>
                </thead>
                <tbody>
                  {backups.data.map((b) => (
                    <tr key={b.id} className="border-t border-line">
                      <td className="px-4 py-2.5">
                        <span className="font-mono font-semibold">{b.slug}</span>
                        {b.kind === 'archive' && (
                          <span className="ml-2">
                            <Tag>al expirar</Tag>
                          </span>
                        )}
                      </td>
                      <td className="px-4 py-2.5 text-ink-2" title={b.createdAt}>{formatRelative(b.createdAt)}</td>
                      <td className="px-4 py-2.5 text-right font-mono text-ink-2 tabular-nums">{b.bytes ? formatBytes(b.bytes) : '—'}</td>
                      <td className="px-4 py-2.5">
                        <div className="flex justify-end gap-1.5">
                          <ButtonLink size="sm" href={me.backupDownloadUrl(b.id)} download aria-label={`Descargar el respaldo de ${b.slug}`}>
                            <DownloadMinimalisticIcon size={16} aria-hidden />
                            Descargar
                          </ButtonLink>
                          <Button size="sm" onClick={() => setRestoring(b)} aria-label={`Restaurar el respaldo de ${b.slug} como base nueva`}>
                            <RestartIcon size={16} aria-hidden />
                            Restaurar
                          </Button>
                          <Button size="sm" variant="ghost" className="hover:text-danger" onClick={() => setRemoving(b)} aria-label={`Eliminar el respaldo de ${b.slug}`}>
                            <TrashBinMinimalisticIcon size={16} aria-hidden />
                          </Button>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Panel>
      </PageBody>

      {restoring && account.data && <RestoreSheet backup={restoring} account={account.data} onClose={() => setRestoring(null)} onRestored={onRestored} />}
      <Sheet open={removing !== null} onClose={() => setRemoving(null)} labelledBy="rm-backup">
        {removing && (
          <>
            <SheetHeader id="rm-backup" title="¿Eliminar este respaldo?" subtitle={`Se borra la copia de ${removing.slug}. La base de datos no cambia.`} />
            <SheetFooter>
              <Button onClick={() => setRemoving(null)}>Cancelar</Button>
              <Button
                variant="danger"
                disabled={remove.busy}
                onClick={async () => {
                  await remove.run(() => me.deleteBackup(removing.id))
                  setRemoving(null)
                  reload()
                }}
              >
                Eliminar respaldo
              </Button>
            </SheetFooter>
          </>
        )}
      </Sheet>
      <CredentialsSheet credentials={creds} title={creds ? `${creds.database} está lista` : ''} onClose={() => setCreds(null)} />
    </>
  )
}

function RestoreSheet({ backup, account, onClose, onRestored }: { backup: Job; account: Account; onClose: () => void; onRestored: (c: Credentials) => void }) {
  // The backup's slug already starts with the user's code; the new name does not repeat it.
  const original = backup.slug.startsWith(`${account.code}-`) ? backup.slug.slice(account.code.length + 1) : backup.slug
  const [raw, setRaw] = useState(`${original}-copia`.slice(0, nameRoom(account.code)))
  const { busy, error, run } = useAction()
  const name = normalizeName(raw)
  const problem = slugProblem(name, nameRoom(account.code))
  const preview = name && !problem ? databasePreview(account, name) : null

  async function submit(e: FormEvent) {
    e.preventDefault()
    if (problem || !name) return
    const c = await run(() => me.restoreBackup(backup.id, name))
    if (c) onRestored(c)
  }

  return (
    <Sheet open onClose={onClose} labelledBy="restore-backup" dismissable={!busy}>
      <form onSubmit={submit}>
        <SheetHeader id="restore-backup" title="Restaurar como base nueva" subtitle={`Crea una base nueva con el contenido del respaldo de ${backup.slug}. No toca la original.`} />
        <div className="space-y-3 px-6 py-5">
          <label className="block">
            <span className="label">Nombre de la base nueva</span>
            <input className="field font-mono" autoFocus required spellCheck={false} autoCapitalize="none" value={raw} aria-invalid={problem ? 'true' : 'false'} onChange={(e) => setRaw(e.target.value)} />
          </label>
          <p className={`text-[0.85rem] ${problem ? 'text-danger' : 'text-ink-2'}`} aria-live="polite">
            {problem ||
              (preview ? (
                <>
                  Se creará como <span className="font-mono text-ink">{preview.slug}</span>. Cuenta dentro del máximo de bases de tu cuenta.
                </>
              ) : (
                'Cuenta dentro del máximo de bases de tu cuenta.'
              ))}
          </p>
          {error && <ErrorNote message={error} />}
        </div>
        <SheetFooter>
          <Button onClick={onClose} disabled={busy}>
            Cancelar
          </Button>
          <Button type="submit" variant="primary" disabled={busy || !!problem || !name}>
            {busy ? 'Restaurando…' : 'Restaurar'}
          </Button>
        </SheetFooter>
      </form>
    </Sheet>
  )
}
