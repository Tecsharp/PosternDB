import { useState, type FormEvent } from 'react'
import { AddCircleIcon, AltArrowRightIcon } from '@solar-icons/react/linear'
import { me, type Credentials, type Database } from '../../api'
import { CredentialsSheet } from '../../components/CredentialsSheet'
import { PageBody, PageHeader } from '../../components/Shell'
import { Button, CodeBlock, EmptyState, ErrorNote, Loading, Sheet, SheetFooter, SheetHeader, StatusBadge, useAction, useLoad } from '../../components/ui'
import { formatRelative, normalizeName, slugProblem } from '../../format'
import { href, linkHandler, navigate } from '../../router'

/** "Mis bases": the whole reduced panel a self-service client sees. */
export function MyDatabases() {
  const { data, error, loading, reload } = useLoad(me.databases, [])
  const [creating, setCreating] = useState(false)
  const [creds, setCreds] = useState<Credentials | null>(null)

  return (
    <>
      <PageHeader
        title="Mis bases de datos"
        subtitle="Cada una tiene su propio usuario y contraseña. Abre una para ver y editar sus datos."
        actions={
          <Button variant="primary" onClick={() => setCreating(true)}>
            <AddCircleIcon size={18} aria-hidden />
            Nueva base de datos
          </Button>
        }
      />
      <PageBody>
        <div className="group-box">
          {loading && !data && <Loading label="Cargando…" />}
          {error && (
            <div className="p-5">
              <ErrorNote message={error} />
            </div>
          )}
          {data && data.length === 0 && (
            <EmptyState
              title="Todavía no tienes bases de datos"
              action={
                <Button variant="primary" onClick={() => setCreating(true)}>
                  Crear la primera
                </Button>
              }
            >
              Crea una base de datos para empezar a guardar y consultar tu información.
            </EmptyState>
          )}
          {data && data.length > 0 && (
            <ul className="group-rows">
              {data.map((db) => (
                <Row key={db.id} db={db} />
              ))}
            </ul>
          )}
        </div>
      </PageBody>

      <CreateSheet
        open={creating}
        existing={data ?? []}
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

function Row({ db }: { db: Database }) {
  const to = href({ page: 'my-database', slug: db.slug, tab: 'resumen' })
  return (
    <li
      className="flex cursor-pointer flex-wrap items-center justify-between gap-3 px-5 py-3.5 hover:bg-surface-2"
      onClick={(e) => {
        if ((e.target as HTMLElement).closest('a')) return
        navigate(to)
      }}
    >
      <div className="min-w-0 flex-1">
        <a href={to} onClick={linkHandler(to)} className="font-mono text-[0.97rem] font-semibold text-ink hover:text-accent-text">
          {db.slug}
        </a>
        <p className="text-[0.85rem] text-ink-2">Última actividad {formatRelative(db.lastSeenAt)}</p>
      </div>
      <StatusBadge status={db.status} />
      <a href={to} onClick={linkHandler(to)} className="inline-flex h-7 items-center gap-1 rounded-[6px] px-2 text-[0.85rem] text-ink-2 hover:bg-surface-3 hover:text-ink">
        Abrir
        <AltArrowRightIcon size={17} aria-hidden />
      </a>
    </li>
  )
}

function CreateSheet({
  open,
  existing,
  onClose,
  onCreated,
}: {
  open: boolean
  existing: Database[]
  onClose: () => void
  onCreated: (c: Credentials) => void
}) {
  const [raw, setRaw] = useState('')
  const [templateId, setTemplateId] = useState(0)
  const templates = useLoad(() => (open ? me.templates() : Promise.resolve([])), [open])
  const { busy, error, run, setError } = useAction()
  const name = normalizeName(raw)
  const problem = slugProblem(name)

  function close() {
    setRaw('')
    setTemplateId(0)
    setError('')
    onClose()
  }

  async function submit(e: FormEvent) {
    e.preventDefault()
    if (problem || !name) return
    const creds = await run(() => me.create(name, templateId || undefined))
    if (creds) {
      setRaw('')
      setTemplateId(0)
      onCreated(creds)
    }
  }

  return (
    <Sheet open={open} onClose={close} labelledBy="create-my-db" dismissable={!busy}>
      <form onSubmit={submit}>
        <SheetHeader id="create-my-db" title="Nueva base de datos" subtitle="Es solo tuya: nadie más puede verla ni usarla." />
        <div className="space-y-4 px-6 py-5">
          <label className="block">
            <span className="label">Nombre</span>
            <input
              className="field font-mono"
              autoFocus
              required
              spellCheck={false}
              autoCapitalize="none"
              placeholder="mi-tienda"
              value={raw}
              aria-invalid={problem ? 'true' : 'false'}
              aria-describedby="my-db-help"
              onChange={(e) => setRaw(e.target.value)}
            />
            <span id="my-db-help" className={`mt-1.5 block text-[0.85rem] ${problem ? 'text-danger' : 'text-ink-2'}`} aria-live="polite">
              {problem || ' '}
            </span>
          </label>
          {templates.data && templates.data.length > 0 && (
            <fieldset className="space-y-2">
              <legend className="label">Empezar con (opcional)</legend>
              <TemplateChoice id={0} title="Base vacía" description="Sin tablas; tú las creas." selected={templateId === 0} onSelect={setTemplateId} />
              {templates.data.map((t) => (
                <TemplateChoice key={t.id} id={t.id} title={t.name} description={t.description} sql={t.sql} selected={templateId === t.id} onSelect={setTemplateId} />
              ))}
            </fieldset>
          )}
          {existing.length > 0 && <p className="text-[0.85rem] text-ink-2">Ya tienes {existing.length} base(s) de datos.</p>}
          {error && <ErrorNote message={error} />}
        </div>
        <SheetFooter>
          <Button onClick={close} disabled={busy}>
            Cancelar
          </Button>
          <Button type="submit" variant="primary" disabled={busy || !name || !!problem}>
            {busy ? 'Creando…' : 'Crear base de datos'}
          </Button>
        </SheetFooter>
      </form>
    </Sheet>
  )
}

function TemplateChoice({ id, title, description, sql, selected, onSelect }: { id: number; title: string; description: string; sql?: string; selected: boolean; onSelect: (id: number) => void }) {
  return (
    <div className={`rounded-[12px] px-3.5 py-2.5 ${selected ? 'bg-accent-soft' : 'bg-surface-2'}`}>
      <label className="flex cursor-pointer items-start gap-2.5">
        <input type="radio" name="template" className="mt-1.5 accent-[var(--accent)]" checked={selected} onChange={() => onSelect(id)} />
        <span className="min-w-0">
          <span className="block text-[0.95rem] font-medium">{title}</span>
          {description && <span className="block text-[0.85rem] text-ink-2">{description}</span>}
        </span>
      </label>
      {sql && (
        <details className="mt-1.5 pl-6">
          <summary className="cursor-pointer text-[0.85rem] text-accent-text">Ver SQL de muestra</summary>
          <div className="mt-2">
            <CodeBlock code={sql} label={`SQL de ${title}`} />
          </div>
        </details>
      )}
    </div>
  )
}
