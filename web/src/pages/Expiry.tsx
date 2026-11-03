import { useState, type FormEvent } from 'react'
import { ClockCircleIcon } from '@solar-icons/react/linear'
import { api, type Database } from '../api'
import type { PageProps } from '../App'
import { PageBody, PageHeader } from '../components/Shell'
import { Button, ErrorNote, Loading, Panel, useAction, useLoad } from '../components/ui'
import { formatDate, formatDateTime } from '../format'
import { Lifeline } from './Databases'
import { ClientRow } from './Clients'

/**
 * Owner-only control centre for the inactivity policy: the global default
 * (overriding DB_EXPIRATION_DAYS / EXPIRING_SOON_DAYS from .env without a
 * restart) and, in the same place, exceptions per database — never expire,
 * a custom number of days, or a one-off extension — and, per registered
 * user, one exception that covers all of their databases.
 */
export function Expiry({ settings }: PageProps) {
  const global = useLoad(api.globalExpiry, [])
  const databases = useLoad(api.databases, [])
  const clients = useLoad(api.clients, [])

  return (
    <>
      <PageHeader title="Expiración" subtitle="Solo el propietario del panel ve y cambia esto. Aplica a todas las bases de datos." />
      <PageBody>
        {global.data && <GlobalPolicyCard current={global.data} fallback={settings} onSaved={global.reload} />}
        {global.error && <ErrorNote message={global.error} />}

        <Panel title="Por usuario" className="mb-6">
          {clients.loading && !clients.data && <Loading label="Cargando usuarios…" />}
          {clients.error && (
            <div className="p-5">
              <ErrorNote message={clients.error} />
            </div>
          )}
          {clients.data && clients.data.length === 0 && <p className="px-5 py-6 text-[0.95rem] text-ink-2">Todavía no se registra ningún usuario.</p>}
          {clients.data && clients.data.length > 0 && (
            <>
              <p className="px-5 pt-4 text-[0.9rem] text-ink-2">
                La excepción de un usuario aplica a todas sus bases, salvo las que tengan una propia. Sin excepción, usan la política global.
              </p>
              <ul className="group-rows">
                {clients.data.map((u) => (
                  <ClientRow key={`${u.id}-${u.neverExpires}-${u.maxIdleDays ?? ''}`} client={u} onChanged={clients.reload} />
                ))}
              </ul>
            </>
          )}
        </Panel>

        <Panel title="Excepciones por base de datos">
          {databases.loading && !databases.data && <Loading label="Cargando bases de datos…" />}
          {databases.error && (
            <div className="p-5">
              <ErrorNote message={databases.error} />
            </div>
          )}
          {databases.data && databases.data.length === 0 && <p className="px-5 py-6 text-[0.95rem] text-ink-2">Todavía no hay bases de datos.</p>}
          {databases.data && databases.data.length > 0 && (
            <ul className="group-rows">
              {databases.data.map((db) => (
                <ExceptionRow key={db.id} db={db} settings={settings} />
              ))}
            </ul>
          )}
        </Panel>
      </PageBody>
    </>
  )
}

function GlobalPolicyCard({ current, fallback, onSaved }: { current: NonNullable<ReturnType<typeof useLoad<import('../api').PlatformSettings>>['data']>; fallback: PageProps['settings']; onSaved: () => void }) {
  const [days, setDays] = useState(current.expirationDays?.toString() ?? '')
  const [warn, setWarn] = useState(current.expiringSoonDays?.toString() ?? '')
  const { busy, error, run } = useAction()

  async function submit(e: FormEvent) {
    e.preventDefault()
    const saved = await run(() => api.setGlobalExpiry(days === '' ? null : Number(days), warn === '' ? null : Number(warn)))
    if (saved) onSaved()
  }

  return (
    <Panel title="Política global" className="mb-6">
      <form onSubmit={submit} className="space-y-4 px-5 py-4">
        <p className="text-[0.95rem] text-ink-2">
          Se aplica a toda base que no tenga una excepción propia. El valor de <span className="font-mono">.env</span> se usa mientras el campo esté vacío.
        </p>
        <div className="flex flex-wrap gap-5">
          <label className="block">
            <span className="label">Días sin conexión antes de expirar</span>
            <input className="field h-9 w-36 font-mono" type="number" min={1} max={3650} placeholder={`.env: ${fallback.expirationDays}`} value={days} onChange={(e) => setDays(e.target.value)} />
          </label>
          <label className="block">
            <span className="label">Días de aviso antes de expirar</span>
            <input className="field h-9 w-36 font-mono" type="number" min={1} max={3650} placeholder={`.env: ${fallback.expiringSoonDays}`} value={warn} onChange={(e) => setWarn(e.target.value)} />
          </label>
        </div>
        {error && <ErrorNote message={error} />}
        <div className="flex items-center gap-3">
          <Button type="submit" variant="primary" disabled={busy}>
            {busy ? 'Guardando…' : 'Guardar política global'}
          </Button>
          {current.updatedBy && (
            <span className="text-[0.85rem] text-ink-3">
              Último cambio de {current.updatedBy} el {formatDateTime(current.updatedAt)}
            </span>
          )}
        </div>
      </form>
    </Panel>
  )
}

function ExceptionRow({ db, settings }: { db: Database; settings: PageProps['settings'] }) {
  const [never, setNever] = useState(db.neverExpires)
  const [custom, setCustom] = useState(db.maxIdleDays !== undefined)
  const [days, setDays] = useState(String(db.maxIdleDays ?? ''))
  const [extendDays, setExtendDays] = useState('')
  const [expiresAt, setExpiresAt] = useState(db.expiresAt)
  const save = useAction()
  const extend = useAction()
  const active = db.status !== 'expirada'

  if (!active) {
    return (
      <li className="flex items-center justify-between gap-3 px-5 py-3 text-[0.95rem] text-ink-3">
        <span className="font-mono">{db.slug}</span>
        <span>Expirada</span>
      </li>
    )
  }

  return (
    <li className="px-5 py-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <p className="font-mono text-[0.95rem] font-semibold">{db.slug}</p>
          <div className="mt-1 w-48">
            <Lifeline db={{ ...db, expiresAt, neverExpires: never, maxIdleDays: custom && days ? Number(days) : undefined }} settings={settings} />
          </div>
        </div>
        <form
          className="flex flex-wrap items-center gap-3"
          onSubmit={async (e) => {
            e.preventDefault()
            const updated = await save.run(() => api.setExpiry(db.slug, never, custom && !never && days ? Number(days) : null))
            if (updated) setExpiresAt(updated.expiresAt)
          }}
        >
          <label className="flex items-center gap-1.5 text-[0.85rem]">
            <input type="checkbox" className="accent-[var(--accent)]" checked={never} onChange={(e) => setNever(e.target.checked)} />
            Nunca expira
          </label>
          <label className={`flex items-center gap-1.5 text-[0.85rem] ${never ? 'opacity-40' : ''}`}>
            <input type="checkbox" className="accent-[var(--accent)]" disabled={never} checked={custom} onChange={(e) => setCustom(e.target.checked)} />
            Días propios
          </label>
          {custom && !never && (
            <input className="field h-8 w-20 font-mono" type="number" min={1} max={3650} value={days} placeholder={String(settings.expirationDays)} onChange={(e) => setDays(e.target.value)} aria-label="Días propios antes de expirar" />
          )}
          <Button size="sm" type="submit" disabled={save.busy}>
            {save.busy ? 'Guardando…' : 'Guardar'}
          </Button>
        </form>
        {!never && (
          <form
            className="flex items-center gap-2"
            onSubmit={async (e) => {
              e.preventDefault()
              if (!extendDays) return
              const updated = await extend.run(() => api.extend(db.slug, Number(extendDays)))
              if (updated) {
                setExpiresAt(updated.expiresAt)
                setExtendDays('')
              }
            }}
          >
            <input className="field h-8 w-16 font-mono" type="number" min={1} max={3650} placeholder="30" value={extendDays} onChange={(e) => setExtendDays(e.target.value)} aria-label={`Extender el plazo de ${db.slug}, días`} />
            <Button size="sm" type="submit" disabled={extend.busy || !extendDays}>
              <ClockCircleIcon size={16} aria-hidden />
              Extender
            </Button>
          </form>
        )}
      </div>
      {expiresAt && !never && <p className="mt-1.5 text-[0.82rem] text-ink-3">Expira el {formatDate(expiresAt)} si nadie se conecta.</p>}
      {(save.error || extend.error) && (
        <div className="mt-2">
          <ErrorNote message={save.error || extend.error} />
        </div>
      )}
    </li>
  )
}
