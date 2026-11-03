import { useState } from 'react'
import { api, can, type Credentials } from '../../api'
import { CredentialsSheet } from '../../components/CredentialsSheet'
import { Button, ErrorNote, KeyValue, Notice, Panel, Sheet, SheetFooter, SheetHeader, useAction } from '../../components/ui'
import { formatDate } from '../../format'
import { href, linkHandler, navigate } from '../../router'
import { Lifeline } from '../Databases'
import type { TabProps } from './DatabaseDetail'

export function SettingsTab({ db, session, settings }: TabProps) {
  const operate = can.operate(session.role)
  const active = db.status !== 'expirada'
  const [creds, setCreds] = useState<Credentials | null>(null)
  const [deleting, setDeleting] = useState(false)
  const reset = useAction()

  if (!operate) return <Notice>Tu rol de lector no puede cambiar ajustes.</Notice>

  return (
    <div className="grid gap-6 lg:grid-cols-2">
      {active && (
        <Panel title="Expiración por inactividad">
          <div className="px-5 pt-4">
            <Lifeline db={db} settings={settings} />
          </div>
          <KeyValue
            rows={[
              ['Expira', db.neverExpires ? 'Nunca' : db.expiresAt ? formatDate(db.expiresAt) : '—'],
              ['Límite', db.neverExpires ? 'Sin límite' : `${db.maxIdleDays ?? settings.expirationDays} días${db.maxIdleDays ? '' : ' (global)'}`],
            ]}
          />
          <div className="px-5 pt-1 pb-4">
            {can.manageExpiry(session.role) ? (
              <a href={href({ page: 'expiry' })} onClick={linkHandler(href({ page: 'expiry' }))} className="text-[0.85rem] font-medium text-accent-text hover:underline">
                Cambiar en Ajustes → Expiración
              </a>
            ) : (
              <p className="text-[0.85rem] text-ink-2">Solo el propietario del panel cambia esto, desde Ajustes → Expiración.</p>
            )}
          </div>
        </Panel>
      )}

      <div className="space-y-6">
        {active && (
          <Panel title="Contraseña del dueño">
            <div className="space-y-3 px-5 py-4">
              <p className="text-[0.95rem] text-ink-2">
                Genera una nueva contraseña para <span className="font-mono text-ink">{db.username}</span>. Los datos no cambian; las apps tendrán que usar la nueva.
              </p>
              {reset.error && <ErrorNote message={reset.error} />}
              <Button size="sm" disabled={reset.busy} onClick={async () => setCreds((await reset.run(() => api.resetPassword(db.slug))) ?? null)}>
                {reset.busy ? 'Generando…' : 'Generar contraseña nueva'}
              </Button>
            </div>
          </Panel>
        )}

        <section className="rounded-[10px] bg-danger-soft" aria-labelledby="danger-title">
          <div className="space-y-3 px-5 py-4">
            <h2 id="danger-title" className="text-[1rem] font-semibold text-danger">
              Eliminar base de datos
            </h2>
            <p className="text-[0.95rem] text-ink-2">
              {active
                ? settings.backupBeforeDelete
                  ? 'Se retira el host, se cierran las conexiones y se guarda un respaldo antes de borrar los datos.'
                  : 'Se retira el host, se cierran las conexiones y los datos se borran sin respaldo.'
                : 'La base ya expiró. Eliminarla solo la quita de la lista y libera el nombre.'}
            </p>
            <Button size="sm" variant="danger" onClick={() => setDeleting(true)}>
              Eliminar {db.slug}
            </Button>
          </div>
        </section>
      </div>

      <CredentialsSheet credentials={creds} title="Contraseña nueva" onClose={() => setCreds(null)} />
      <DeleteSheet open={deleting} slug={db.slug} onClose={() => setDeleting(false)} />
    </div>
  )
}

function DeleteSheet({ open, slug, onClose }: { open: boolean; slug: string; onClose: () => void }) {
  const [typed, setTyped] = useState('')
  const { busy, error, run } = useAction()
  return (
    <Sheet open={open} onClose={onClose} labelledBy="del-title" dismissable={!busy}>
      <form
        onSubmit={async (e) => {
          e.preventDefault()
          if (typed !== slug) return
          const ok = await run(() => api.remove(slug).then(() => true))
          if (ok) navigate(href({ page: 'databases' }))
        }}
      >
        <SheetHeader id="del-title" title={`¿Eliminar ${slug}?`} subtitle="Esta acción no se puede deshacer desde el panel." />
        <div className="space-y-4 px-6 py-5">
          <label className="block">
            <span className="label">
              Escribe <span className="font-mono font-semibold text-ink">{slug}</span> para confirmar
            </span>
            <input className="field font-mono" autoFocus autoComplete="off" spellCheck={false} value={typed} onChange={(e) => setTyped(e.target.value)} />
          </label>
          {error && <ErrorNote message={error} />}
        </div>
        <SheetFooter>
          <Button onClick={onClose} disabled={busy}>
            Cancelar
          </Button>
          <Button type="submit" variant="danger" disabled={busy || typed !== slug}>
            {busy ? 'Eliminando…' : 'Eliminar base de datos'}
          </Button>
        </SheetFooter>
      </form>
    </Sheet>
  )
}
