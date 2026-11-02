import { useState, type FormEvent } from 'react'
import { api, can, type Credentials } from '../../api'
import { CredentialsSheet } from '../../components/CredentialsSheet'
import { Button, ErrorNote, Notice, Panel, Sheet, SheetFooter, SheetHeader, useAction } from '../../components/ui'
import { formatDate } from '../../format'
import { href, navigate } from '../../router'
import type { TabProps } from './DatabaseDetail'

export function SettingsTab({ db, session, settings, onChange }: TabProps) {
  const operate = can.operate(session.role)
  const active = db.status !== 'expirada'
  const [never, setNever] = useState(db.neverExpires)
  const [custom, setCustom] = useState(db.maxIdleDays !== undefined)
  const [days, setDays] = useState(String(db.maxIdleDays ?? settings.expirationDays))
  const [extendDays, setExtendDays] = useState('30')
  const [creds, setCreds] = useState<Credentials | null>(null)
  const [deleting, setDeleting] = useState(false)
  const expiry = useAction()
  const extend = useAction()
  const reset = useAction()

  if (!operate) return <Notice tone="info">Tu rol de lector no puede cambiar ajustes.</Notice>

  async function saveExpiry(e: FormEvent) {
    e.preventDefault()
    const updated = await expiry.run(() => api.setExpiry(db.slug, never, custom && !never ? Number(days) : null))
    if (updated) onChange(updated)
  }

  return (
    <div className="grid gap-6 lg:grid-cols-2">
      {active && (
        <Panel title="Expiración por inactividad">
          <form onSubmit={saveExpiry} className="space-y-4 px-5 py-4">
            <label className="flex items-start gap-2.5 text-[13px]">
              <input type="checkbox" className="mt-0.5 accent-[var(--accent)]" checked={never} onChange={(e) => setNever(e.target.checked)} />
              <span>
                <span className="font-semibold">Nunca expira</span>
                <span className="block text-[12px] text-ink-2">Para clientes fijos. El auditor la ignora.</span>
              </span>
            </label>
            <label className={`flex items-start gap-2.5 text-[13px] ${never ? 'opacity-50' : ''}`}>
              <input type="checkbox" className="mt-0.5 accent-[var(--accent)]" disabled={never} checked={custom} onChange={(e) => setCustom(e.target.checked)} />
              <span className="flex-1">
                <span className="font-semibold">Límite propio</span>
                <span className="block text-[12px] text-ink-2">En lugar de los {settings.expirationDays} días globales.</span>
                {custom && !never && (
                  <span className="mt-2 flex items-center gap-2">
                    <input className="field h-7 w-24 font-mono" type="number" min={1} max={3650} required value={days} onChange={(e) => setDays(e.target.value)} aria-label="Días sin conexión antes de expirar" />
                    <span className="text-[12px] text-ink-2">días sin conexiones</span>
                  </span>
                )}
              </span>
            </label>
            {expiry.error && <ErrorNote message={expiry.error} />}
            <Button type="submit" size="sm" variant="primary" disabled={expiry.busy}>
              {expiry.busy ? 'Guardando…' : 'Guardar política'}
            </Button>
          </form>
          {!db.neverExpires && (
            <form
              className="flex flex-wrap items-end gap-2 px-5 py-4 shadow-[inset_0_1px_0_var(--hairline)]"
              onSubmit={async (e) => {
                e.preventDefault()
                const updated = await extend.run(() => api.extend(db.slug, Number(extendDays)))
                if (updated) onChange(updated)
              }}
            >
              <label className="block">
                <span className="label">Extender el plazo</span>
                <input className="field h-7 w-24 font-mono" type="number" min={1} max={3650} value={extendDays} onChange={(e) => setExtendDays(e.target.value)} />
              </label>
              <Button type="submit" size="sm" disabled={extend.busy}>
                Extender {extendDays} días
              </Button>
              {db.expiresAt && <p className="w-full text-[12px] text-ink-2">Ahora expira el {formatDate(db.expiresAt)}.</p>}
              {extend.error && <ErrorNote message={extend.error} />}
            </form>
          )}
        </Panel>
      )}

      <div className="space-y-6">
        {active && (
          <Panel title="Contraseña del dueño">
            <div className="space-y-3 px-5 py-4">
              <p className="text-[13px] text-ink-2">
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
            <h2 id="danger-title" className="text-[14px] font-semibold text-danger">
              Eliminar base de datos
            </h2>
            <p className="text-[13px] text-ink-2">
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
