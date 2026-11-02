import { useMemo, useState, type FormEvent } from 'react'
import { AddCircleIcon, MagnifierIcon } from '@solar-icons/react/linear'
import { api, can, type Credentials, type Database, type Settings } from '../api'
import type { PageProps } from '../App'
import { CredentialsSheet } from '../components/CredentialsSheet'
import { PageBody, PageHeader } from '../components/Shell'
import { Button, EmptyState, ErrorNote, Loading, Metrics, Sheet, SheetFooter, SheetHeader, StatusBadge, useAction, useLoad } from '../components/ui'
import { daysUntil, formatDate, formatRelative, normalizeName, slugProblem } from '../format'
import { href, linkHandler } from '../router'

export function Databases({ session, settings }: PageProps) {
  const { data, error, loading, reload } = useLoad(api.databases, [])
  const [query, setQuery] = useState('')
  const [creating, setCreating] = useState(false)
  const [creds, setCreds] = useState<Credentials | null>(null)
  const operate = can.operate(session.role)

  const filtered = useMemo(
    () => (data ?? []).filter((d) => d.slug.includes(query.trim().toLowerCase()) || d.hostname.includes(query.trim().toLowerCase())),
    [data, query],
  )
  const counts = useMemo(() => {
    const c = { activa: 0, por_expirar: 0, expirada: 0 }
    for (const d of data ?? []) c[d.status]++
    return c
  }, [data])

  return (
    <>
      <PageHeader
        title="Bases de datos"
        subtitle={
          settings.localMode
            ? `Modo local: accesibles en 127.0.0.1:${settings.localDbPort}`
            : `Publicadas como ${settings.clientHostPrefix}<cliente>.${settings.baseDomain}`
        }
        actions={
          operate && (
            <Button variant="primary" onClick={() => setCreating(true)}>
              <AddCircleIcon size={16} aria-hidden />
              Nueva base de datos
            </Button>
          )
        }
      />
      <PageBody>
        {data && data.length > 0 && (
          <Metrics
            items={[
              { label: 'Total', value: data.length },
              { label: 'Activas', value: counts.activa, tone: 'text-accent-text' },
              { label: 'Por expirar', value: counts.por_expirar, tone: 'text-warn' },
              { label: 'Expiradas', value: counts.expirada, tone: 'text-idle' },
            ]}
          />
        )}

        <section aria-labelledby="list-title">
          <header className="flex flex-wrap items-end justify-between gap-3 pb-1.5">
            <h2 id="list-title" className="section-title p-0 px-1">
              Clientes
            </h2>
            <label className="relative w-full max-w-[260px]">
              <span className="sr-only">Buscar base de datos</span>
              <MagnifierIcon size={15} aria-hidden className="pointer-events-none absolute top-1/2 left-2.5 -translate-y-1/2 text-ink-3" />
              <input className="field h-7 pl-8" placeholder="Buscar" value={query} onChange={(e) => setQuery(e.target.value)} />
            </label>
          </header>
          <div className="group-box">
          {loading && !data && <Loading label="Cargando bases de datos…" />}
          {error && (
            <div className="p-5">
              <ErrorNote message={error} />
            </div>
          )}
          {data && data.length === 0 && (
            <EmptyState
              title="Aún no hay bases de datos"
              action={
                operate && (
                  <Button variant="primary" onClick={() => setCreating(true)}>
                    Crear la primera
                  </Button>
                )
              }
            >
              Cada base tiene su propio usuario y su host, y expira sola si nadie se conecta en {settings.expirationDays} días.
            </EmptyState>
          )}
          {filtered.length > 0 && <DatabaseTable rows={filtered} settings={settings} />}
          {data && data.length > 0 && filtered.length === 0 && <EmptyState title={`Nada coincide con "${query}"`} />}
          </div>
        </section>
      </PageBody>

      <CreateSheet
        open={creating}
        settings={settings}
        databases={data ?? []}
        onClose={() => setCreating(false)}
        onCreated={(c) => {
          setCreating(false)
          setCreds(c)
          reload()
        }}
      />
      <CredentialsSheet credentials={creds} title={creds ? `${creds.database} está lista` : ''} onClose={() => setCreds(null)} />
    </>
  )
}

/** Lifeline: remaining days before the inactivity policy expires the DB. */
export function Lifeline({ db, settings }: { db: Database; settings: Settings }) {
  if (db.neverExpires) return <span className="text-[12px] text-ink-2">No expira</span>
  const max = db.maxIdleDays ?? settings.expirationDays
  const left = db.status === 'expirada' || !db.expiresAt ? 0 : daysUntil(db.expiresAt)
  const detail = db.status === 'expirada' ? (db.expiredAt ? `Expiró el ${formatDate(db.expiredAt)}` : 'Expirada') : left === 1 ? '1 día' : `${left} días`
  return (
    <div className="min-w-[140px]">
      <div className="mb-1.5 flex justify-between text-[12px] text-ink-2 tabular-nums">
        <span>{db.status === 'expirada' ? '' : 'Quedan'}</span>
        <span>{detail}</span>
      </div>
      <meter
        className="lifeline"
        min={0}
        max={Math.max(max, left)}
        low={1}
        high={settings.expiringSoonDays}
        optimum={Math.max(max, left)}
        value={left}
        aria-label={`Tiempo restante: ${detail}`}
      />
    </div>
  )
}

function DatabaseTable({ rows, settings }: { rows: Database[]; settings: Settings }) {
  return (
    <table className="w-full border-separate border-spacing-0 text-left text-[13px]">
      <caption className="sr-only">Bases de datos de clientes</caption>
      <thead className="max-md:sr-only">
        <tr className="text-[12px] text-ink-2">
          {['Cliente', 'Estado', 'Última actividad', 'Expiración', 'Creada'].map((h) => (
            <th key={h} scope="col" className="border-b border-line px-4 py-2 text-[12px] font-medium">
              {h}
            </th>
          ))}
        </tr>
      </thead>
      <tbody>
        {rows.map((db) => {
          const to = href({ page: 'database', slug: db.slug, tab: 'resumen' })
          return (
            <tr key={db.id} className="transition-colors hover:bg-surface-2 max-md:flex max-md:flex-col max-md:gap-2 max-md:border-b max-md:border-line max-md:px-4 max-md:py-3">
              <th scope="row" className="border-b border-line px-4 py-2.5 font-normal max-md:border-0 max-md:p-0">
                <a href={to} onClick={linkHandler(to)} className="font-semibold hover:text-accent-text hover:underline">
                  {db.slug}
                </a>
                <span className="mt-0.5 block truncate font-mono text-[12px] text-ink-2">{db.hostname || db.database}</span>
              </th>
              <td className="border-b border-line px-4 py-2.5 max-md:border-0 max-md:p-0">
                <StatusBadge status={db.status} />
              </td>
              <td className="border-b border-line px-4 py-2.5 whitespace-nowrap max-md:border-0 max-md:p-0">
                <time dateTime={db.lastSeenAt} title={new Date(db.lastSeenAt).toLocaleString('es-MX')}>
                  {formatRelative(db.lastSeenAt)}
                </time>
              </td>
              <td className="border-b border-line px-4 py-2.5 max-md:border-0 max-md:p-0">
                <Lifeline db={db} settings={settings} />
              </td>
              <td className="border-b border-line px-4 py-2.5 whitespace-nowrap text-ink-2 max-md:border-0 max-md:p-0">
                <time dateTime={db.createdAt}>{formatDate(db.createdAt)}</time>
              </td>
            </tr>
          )
        })}
      </tbody>
    </table>
  )
}

type Source = 'empty' | 'template' | 'clone' | 'backup'

function CreateSheet({
  open,
  settings,
  databases,
  onClose,
  onCreated,
}: {
  open: boolean
  settings: Settings
  databases: Database[]
  onClose: () => void
  onCreated: (c: Credentials) => void
}) {
  const [raw, setRaw] = useState('')
  const [source, setSource] = useState<Source>('empty')
  const [templateId, setTemplateId] = useState('')
  const [cloneFrom, setCloneFrom] = useState('')
  const [backupId, setBackupId] = useState('')
  const templates = useLoad(() => (open ? api.templates() : Promise.resolve([])), [open])
  const backups = useLoad(() => (open ? api.backups() : Promise.resolve([])), [open])
  const { busy, error, run, setError } = useAction()

  const name = normalizeName(raw)
  const problem = slugProblem(name)
  const host = settings.localMode
    ? `127.0.0.1:${settings.localDbPort}`
    : `${settings.clientHostPrefix}${name || 'cliente'}.${settings.baseDomain}`
  const active = databases.filter((d) => d.status !== 'expirada')
  const sourceReady =
    source === 'empty' || (source === 'template' && templateId) || (source === 'clone' && cloneFrom) || (source === 'backup' && backupId)

  function close() {
    setRaw('')
    setSource('empty')
    setError('')
    onClose()
  }

  async function submit(e: FormEvent) {
    e.preventDefault()
    if (problem || !name || !sourceReady) return
    const creds = await run(() =>
      api.create({
        name,
        templateId: source === 'template' ? Number(templateId) : undefined,
        cloneFrom: source === 'clone' ? cloneFrom : undefined,
        restoreBackupId: source === 'backup' ? Number(backupId) : undefined,
      }),
    )
    if (creds) {
      setRaw('')
      setSource('empty')
      onCreated(creds)
    }
  }

  const options: { value: Source; label: string; hint: string }[] = [
    { value: 'empty', label: 'Vacía', hint: 'Sin tablas.' },
    { value: 'template', label: 'Desde plantilla', hint: 'Ejecuta el SQL de una plantilla.' },
    { value: 'clone', label: 'Clonar otra base', hint: 'Copia esquema y datos.' },
    { value: 'backup', label: 'Restaurar respaldo', hint: 'Carga un respaldo guardado.' },
  ]

  return (
    <Sheet open={open} onClose={close} labelledBy="create-title" dismissable={!busy}>
      <form onSubmit={submit}>
        <SheetHeader id="create-title" title="Nueva base de datos" subtitle="Se crea una base PostgreSQL con su propio usuario y se publica automáticamente." />
        <div className="space-y-5 px-6 py-5">
          <label className="block">
            <span className="label">Nombre del cliente</span>
            <input
              className="field font-mono"
              autoFocus
              required
              spellCheck={false}
              autoCapitalize="none"
              placeholder="acme"
              value={raw}
              aria-invalid={problem ? 'true' : 'false'}
              aria-describedby="name-help"
              onChange={(e) => setRaw(e.target.value)}
            />
            <span id="name-help" className={`mt-1.5 block text-[12px] ${problem ? 'text-danger' : 'text-ink-2'}`} aria-live="polite">
              {problem || (
                <>
                  {settings.localMode ? 'Disponible en ' : 'Se publicará en '}
                  <span className="font-mono text-ink">{host}</span>
                </>
              )}
            </span>
          </label>

          <fieldset>
            <legend className="label">Contenido inicial</legend>
            <div className="grid grid-cols-2 gap-2">
              {options.map((o) => (
                <label
                  key={o.value}
                  className={`cursor-pointer rounded-[8px] px-3 py-2.5 transition-colors has-[:focus-visible]:outline-3 has-[:focus-visible]:outline-[var(--ring)] ${
                    source === o.value ? 'bg-accent-soft ring-1 ring-accent/60 ring-inset' : 'bg-surface-2 hover:bg-surface-3'
                  }`}
                >
                  <input type="radio" name="source" value={o.value} checked={source === o.value} onChange={() => setSource(o.value)} className="sr-only" />
                  <span className="block text-[13px] font-semibold">{o.label}</span>
                  <span className="block text-[12px] text-ink-2">{o.hint}</span>
                </label>
              ))}
            </div>
          </fieldset>

          {source === 'template' && (
            <label className="block">
              <span className="label">Plantilla</span>
              <select className="field" required value={templateId} onChange={(e) => setTemplateId(e.target.value)}>
                <option value="">Elige una plantilla</option>
                {(templates.data ?? []).map((t) => (
                  <option key={t.id} value={t.id}>
                    {t.name}
                  </option>
                ))}
              </select>
            </label>
          )}
          {source === 'clone' && (
            <label className="block">
              <span className="label">Base de origen</span>
              <select className="field" required value={cloneFrom} onChange={(e) => setCloneFrom(e.target.value)}>
                <option value="">Elige una base</option>
                {active.map((d) => (
                  <option key={d.id} value={d.slug}>
                    {d.slug}
                  </option>
                ))}
              </select>
              <span className="mt-1.5 block text-[12px] text-ink-2">La copia se hace en segundo plano; la base aparece vacía unos segundos.</span>
            </label>
          )}
          {source === 'backup' && (
            <label className="block">
              <span className="label">Respaldo</span>
              <select className="field" required value={backupId} onChange={(e) => setBackupId(e.target.value)}>
                <option value="">Elige un respaldo</option>
                {(backups.data ?? []).map((b) => (
                  <option key={b.id} value={b.id}>
                    {b.file} ({formatDate(b.createdAt)})
                  </option>
                ))}
              </select>
            </label>
          )}

          {error && <ErrorNote message={error} />}
        </div>
        <SheetFooter>
          <Button onClick={close} disabled={busy}>
            Cancelar
          </Button>
          <Button type="submit" variant="primary" disabled={busy || !name || !!problem || !sourceReady}>
            {busy ? 'Creando…' : 'Crear base de datos'}
          </Button>
        </SheetFooter>
      </form>
    </Sheet>
  )
}
