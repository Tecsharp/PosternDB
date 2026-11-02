import { useMemo, useState } from 'react'

// The diagram pulls in React Flow and dagre; load it only when needed.
const SchemaDiagram = lazy(() => import('../../components/SchemaDiagram'))
import { AltArrowDownIcon, AltArrowRightIcon, DiagramDownIcon, EyeIcon, KeyMinimalisticIcon, LinkMinimalistic2Icon, ListIcon, MagnifierIcon, TableIcon } from '@solar-icons/react/linear'
import { api, can, type Table } from '../../api'
import { lazy, Suspense } from 'react'
import { tableKey } from '../../components/schemaKey'
import { Button, EmptyState, ErrorNote, Loading, Panel, segmentClass, Tag, useAction, useLoad } from '../../components/ui'
import { formatBytes, formatNumber } from '../../format'
import type { TabProps } from './DatabaseDetail'
import { ResultGrid } from './ConsoleTab'

type View = 'diagram' | 'tree'

export function SchemaTab({ db, session }: TabProps) {
  const { data, error, loading, reload } = useLoad(() => api.schema(db.slug), [db.slug])
  const [view, setView] = useState<View>('diagram')
  const [selected, setSelected] = useState<string | null>(null)
  const [filter, setFilter] = useState('')

  const tables = useMemo(() => {
    const f = filter.trim().toLowerCase()
    return (data?.tables ?? []).filter((t) => !f || t.name.includes(f) || t.columns.some((c) => c.name.includes(f)))
  }, [data, filter])
  const current = data?.tables.find((t) => tableKey(t) === selected) ?? null

  if (loading && !data) return <Loading label="Leyendo el esquema…" />
  if (error) return <ErrorNote message={error} />
  if (!data || data.tables.length === 0) {
    return (
      <Panel>
        <EmptyState title="Esta base todavía no tiene tablas">
          Crea tablas desde tu aplicación o con una plantilla y aparecerán aquí con sus relaciones.
        </EmptyState>
      </Panel>
    )
  }

  const totalRows = data.tables.filter((t) => t.kind === 'table').reduce((n, t) => n + t.rows, 0)

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-[13px] text-ink-2">
          {data.tables.length} objetos en {data.schemas.length} {data.schemas.length === 1 ? 'esquema' : 'esquemas'}, {formatNumber(totalRows)} registros aprox.
        </p>
        <div className="flex items-center gap-2">
          <label className="relative">
            <span className="sr-only">Filtrar tablas y columnas</span>
            <MagnifierIcon size={14} aria-hidden className="pointer-events-none absolute top-1/2 left-2.5 -translate-y-1/2 text-ink-3" />
            <input className="field h-7 w-56 pl-8 text-[12.5px]" placeholder="Filtrar tablas o columnas" value={filter} onChange={(e) => setFilter(e.target.value)} />
          </label>
          <div role="group" aria-label="Vista" className="inline-flex rounded-[7px] bg-surface-2 p-0.5">
            <button type="button" aria-pressed={view === 'diagram'} onClick={() => setView('diagram')} className={segmentClass(view === 'diagram')}>
              <DiagramDownIcon size={14} aria-hidden /> Diagrama
            </button>
            <button type="button" aria-pressed={view === 'tree'} onClick={() => setView('tree')} className={segmentClass(view === 'tree')}>
              <ListIcon size={14} aria-hidden /> Árbol
            </button>
          </div>
          <Button size="sm" variant="ghost" onClick={reload}>
            Actualizar
          </Button>
        </div>
      </div>

      <div className={`grid gap-4 ${current ? 'xl:grid-cols-[1fr_420px]' : ''}`}>
        <div className="group-box">
          {view === 'diagram' ? (
            <Suspense fallback={<Loading label="Dibujando diagrama…" />}>
              <SchemaDiagram tables={tables} selected={selected} onSelect={setSelected} />
            </Suspense>
          ) : (
            <SchemaTree tables={tables} schemas={data.schemas} selected={selected} onSelect={setSelected} />
          )}
        </div>
        {current && <TableDetail key={tableKey(current)} slug={db.slug} table={current} canRead={can.operate(session.role)} onClose={() => setSelected(null)} />}
      </div>
    </div>
  )
}

/** Disclosure tree: schemas > tables > columns. Plain buttons, fully keyboard accessible. */
function SchemaTree({ tables, schemas, selected, onSelect }: { tables: Table[]; schemas: string[]; selected: string | null; onSelect: (k: string) => void }) {
  const [open, setOpen] = useState<Set<string>>(() => new Set(schemas))
  const toggle = (k: string) =>
    setOpen((prev) => {
      const next = new Set(prev)
      if (next.has(k)) next.delete(k)
      else next.add(k)
      return next
    })

  return (
    <div className="max-h-[620px] overflow-y-auto p-3 font-mono text-[12.5px]">
      <ul>
        {schemas.map((s) => {
          const inSchema = tables.filter((t) => t.schema === s)
          if (inSchema.length === 0) return null
          return (
            <li key={s}>
              <TreeToggle open={open.has(s)} onClick={() => toggle(s)} label={`esquema ${s}`}>
                <span className="font-semibold">{s}</span>
                <span className="text-ink-3">{inSchema.length}</span>
              </TreeToggle>
              {open.has(s) && (
                <ul className="ml-4 border-l border-line pl-2">
                  {inSchema.map((t) => {
                    const k = tableKey(t)
                    return (
                      <li key={k}>
                        <div className="flex items-center">
                          <TreeToggle open={open.has(k)} onClick={() => toggle(k)} label={`columnas de ${t.name}`}>
                            <TableIcon size={14} aria-hidden className="text-ink-3" />
                            <span>{t.name}</span>
                          </TreeToggle>
                          <span className="ml-auto pr-2 text-[11px] text-ink-3 tabular-nums">{t.kind === 'table' ? formatNumber(t.rows) : t.kind === 'view' ? 'vista' : 'mat.'}</span>
                          <Button size="sm" variant={selected === k ? 'secondary' : 'ghost'} className="h-6 px-2 text-[11px]" onClick={() => onSelect(k)} aria-pressed={selected === k}>
                            Detalles
                          </Button>
                        </div>
                        {open.has(k) && (
                          <ul className="ml-5 border-l border-line pl-3">
                            {t.columns.map((c) => (
                              <li key={c.name} className="flex items-center gap-2 py-0.5 text-[12px]">
                                {c.primaryKey ? <KeyMinimalisticIcon size={12} className="text-accent-text" aria-label="clave primaria" /> : t.foreignKeys.some((f) => f.columns.includes(c.name)) ? <LinkMinimalistic2Icon size={12} className="text-ink-2" aria-label="clave foránea" /> : <span className="w-[11px]" />}
                                <span>{c.name}</span>
                                <span className="text-ink-3">{c.type}</span>
                              </li>
                            ))}
                          </ul>
                        )}
                      </li>
                    )
                  })}
                </ul>
              )}
            </li>
          )
        })}
      </ul>
    </div>
  )
}

function TreeToggle({ open, onClick, label, children }: { open: boolean; onClick: () => void; label: string; children: React.ReactNode }) {
  return (
    <button type="button" onClick={onClick} aria-expanded={open} aria-label={`${open ? 'Contraer' : 'Expandir'} ${label}`} className="flex h-7 items-center gap-1.5 rounded-[6px] px-1.5 hover:bg-surface-2">
      {open ? <AltArrowDownIcon size={13} aria-hidden /> : <AltArrowRightIcon size={13} aria-hidden />}
      {children}
    </button>
  )
}

function TableDetail({ slug, table, canRead, onClose }: { slug: string; table: Table; canRead: boolean; onClose: () => void }) {
  const [exact, setExact] = useState<number | null>(null)
  const [preview, setPreview] = useState<Awaited<ReturnType<typeof api.preview>> | null>(null)
  const counting = useAction()
  const loadingRows = useAction()

  return (
    <aside className="group-box self-start" aria-label={`Detalles de ${table.name}`}>
      <header className="flex items-start justify-between gap-3 px-4 pt-4 pb-1">
        <div className="min-w-0">
          <h2 className="truncate font-mono text-[15px] font-semibold">
            {table.schema}.{table.name}
          </h2>
          {table.comment && <p className="mt-0.5 text-[12.5px] text-ink-2">{table.comment}</p>}
          <div className="mt-2 flex flex-wrap gap-1.5">
            <Tag>{table.kind === 'table' ? 'tabla' : table.kind === 'view' ? 'vista' : 'vista materializada'}</Tag>
            {table.kind !== 'view' && <Tag>{formatBytes(table.sizeBytes)}</Tag>}
          </div>
        </div>
        <Button size="sm" variant="ghost" onClick={onClose}>
          Cerrar
        </Button>
      </header>

      <div className="space-y-5 px-4 pt-3 pb-4">
        <div className="flex flex-wrap items-center gap-2">
          <p className="text-[13px]">
            <span className="font-mono text-[18px] font-semibold tabular-nums">{formatNumber(exact ?? table.rows)}</span>{' '}
            <span className="text-ink-2">{exact === null ? 'registros (estimado)' : 'registros (exacto)'}</span>
          </p>
          {exact === null && (
            <Button size="sm" disabled={counting.busy} onClick={() => counting.run(() => api.count(slug, table.schema, table.name).then(setExact))}>
              {counting.busy ? 'Contando…' : 'Contar exacto'}
            </Button>
          )}
        </div>
        {counting.error && <ErrorNote message={counting.error} />}

        <section>
          <h3 className="mb-2 text-[12.5px] font-semibold text-ink-2">Columnas</h3>
          <ul className="group-rows overflow-hidden rounded-[8px] bg-surface-2 font-mono text-[12px]">
            {table.columns.map((c) => (
              <li key={c.name} className="flex items-center gap-2 px-3 py-1.5">
                {c.primaryKey && <KeyMinimalisticIcon size={12} className="text-accent-text" aria-label="clave primaria" />}
                <span className={c.primaryKey ? 'font-semibold' : ''}>{c.name}</span>
                <span className="ml-auto text-ink-2">{c.type}</span>
                {!c.nullable && <span className="text-ink-3" title="NOT NULL">nn</span>}
              </li>
            ))}
          </ul>
        </section>

        {table.foreignKeys.length > 0 && (
          <section>
            <h3 className="mb-2 text-[12.5px] font-semibold text-ink-2">Relaciones</h3>
            <ul className="space-y-1 font-mono text-[12px]">
              {table.foreignKeys.map((fk) => (
                <li key={fk.name}>
                  {fk.columns.join(', ')} <span className="text-ink-3">→</span> {fk.refTable}({fk.refColumns.join(', ')})
                </li>
              ))}
            </ul>
          </section>
        )}

        {table.indexes.length > 0 && (
          <section>
            <h3 className="mb-2 text-[12.5px] font-semibold text-ink-2">Índices</h3>
            <ul className="space-y-1.5 font-mono text-[11.5px] text-ink-2">
              {table.indexes.map((ix) => (
                <li key={ix.name} className="break-all">
                  {ix.definition}
                </li>
              ))}
            </ul>
          </section>
        )}

        {canRead && (
          <section>
            {!preview ? (
              <Button size="sm" disabled={loadingRows.busy} onClick={() => loadingRows.run(() => api.preview(slug, table.schema, table.name).then(setPreview))}>
                <EyeIcon size={14} aria-hidden />
                {loadingRows.busy ? 'Cargando…' : 'Ver primeras filas'}
              </Button>
            ) : (
              <>
                <h3 className="mb-2 text-[12.5px] font-semibold text-ink-2">Primeras {preview.rowCount} filas</h3>
                <ResultGrid result={preview} maxHeight="max-h-72" />
              </>
            )}
            {loadingRows.error && <ErrorNote message={loadingRows.error} />}
          </section>
        )}
      </div>
    </aside>
  )
}
