import { useEffect, useMemo, useRef, useState, type FormEvent } from 'react'
import {
  AddCircleIcon,
  AltArrowLeftIcon,
  AltArrowRightIcon,
  KeyMinimalisticIcon,
  MagnifierIcon,
  PenIcon,
  RefreshIcon,
  SortFromBottomToTopIcon,
  SortFromTopToBottomIcon,
  TrashBinTrashIcon,
} from '@solar-icons/react/linear'
import { api, can, type DataColumn, type RowValues, type TableData } from '../../api'
import { Button, EmptyState, ErrorNote, Loading, Notice, Sheet, SheetFooter, SheetHeader, useAction, useLoad } from '../../components/ui'
import { formatNumber } from '../../format'
import type { TabProps } from './DatabaseDetail'

type Selected = { schema: string; name: string }

// Postgres spells out type names ("timestamp with time zone", "character
// varying(255)") that would otherwise force very wide header columns.
const TYPE_ABBR: Record<string, string> = {
  'timestamp with time zone': 'timestamptz',
  'timestamp without time zone': 'timestamp',
  'character varying': 'varchar',
  'double precision': 'float8',
}
function shortType(type: string): string {
  for (const [long, short] of Object.entries(TYPE_ABBR)) {
    if (type.startsWith(long)) return short + type.slice(long.length)
  }
  return type
}


/**
 * Data editor: browse any table page by page, sort by any column, edit a
 * cell with a double click, edit or delete a whole row, and insert rows.
 * Writes run with the database owner's permissions and are audited.
 */
export function DataTab({ db, session }: TabProps) {
  const schema = useLoad(() => api.schema(db.slug), [db.slug])
  const [selected, setSelected] = useState<Selected | null>(null)
  const [filter, setFilter] = useState('')
  const tables = useMemo(() => (schema.data?.tables ?? []).filter((t) => t.name.includes(filter.trim().toLowerCase())), [schema.data, filter])

  useEffect(() => {
    if (!selected && schema.data?.tables.length) {
      const first = schema.data.tables[0]
      setSelected({ schema: first.schema, name: first.name })
    }
  }, [schema.data, selected])

  if (!can.operate(session.role)) return <Notice>Tu rol de lector puede ver el esquema, pero no los registros.</Notice>
  if (schema.loading && !schema.data) return <Loading label="Cargando tablas…" />
  if (schema.error) return <ErrorNote message={schema.error} />
  if (!schema.data || schema.data.tables.length === 0) {
    return (
      <div className="group-box">
        <EmptyState title="Esta base todavía no tiene tablas">Crea tablas desde tu aplicación, una plantilla o la consola SQL.</EmptyState>
      </div>
    )
  }

  return (
    <div className="grid gap-5 lg:grid-cols-[230px_1fr]">
      <nav aria-label="Tablas" className="self-start">
        <h2 className="section-title">Tablas</h2>
        <div className="group-box p-1.5">
          <label className="relative mb-1 block">
            <span className="sr-only">Filtrar tablas</span>
            <MagnifierIcon size={17} aria-hidden className="pointer-events-none absolute top-1/2 left-2.5 -translate-y-1/2 text-ink-3" />
            <input className="field h-8 pl-8" placeholder="Filtrar" value={filter} onChange={(e) => setFilter(e.target.value)} />
          </label>
          <ul className="max-h-[560px] overflow-y-auto">
            {tables.map((t) => {
              const active = selected?.schema === t.schema && selected?.name === t.name
              return (
                <li key={`${t.schema}.${t.name}`}>
                  <button
                    type="button"
                    aria-current={active ? 'true' : undefined}
                    onClick={() => setSelected({ schema: t.schema, name: t.name })}
                    className={`flex w-full items-center justify-between gap-2 rounded-[6px] px-2.5 py-1.5 text-left text-[1rem] ${active ? 'bg-surface-3 font-medium' : 'hover:bg-surface-2'}`}
                  >
                    <span className="truncate font-mono text-[0.95rem]">
                      {t.schema !== 'public' && <span className="text-ink-3">{t.schema}.</span>}
                      {t.name}
                    </span>
                    <span className="shrink-0 text-[0.85rem] text-ink-3 tabular-nums">{t.kind === 'table' ? formatNumber(t.rows) : 'vista'}</span>
                  </button>
                </li>
              )
            })}
          </ul>
        </div>
      </nav>
      {selected && <TableEditor key={`${selected.schema}.${selected.name}`} slug={db.slug} table={selected} />}
    </div>
  )
}

function keyOf(data: TableData, row: (string | null)[]): RowValues {
  const key: RowValues = {}
  data.columns.forEach((c, i) => {
    if (c.primaryKey) key[c.name] = row[i]
  })
  return key
}

function TableEditor({ slug, table }: { slug: string; table: Selected }) {
  const [page, setPage] = useState(1)
  const [sort, setSort] = useState<{ col: string; desc: boolean } | null>(null)
  // The row count scans the whole table, so it is asked for once and reused
  // while paging or sorting; any change to the data asks for it again.
  const knownTotal = useRef<number | undefined>(undefined)
  const { data, error, loading, reload: refetch } = useLoad(
    () => api.tableData(slug, table.schema, table.name, page, sort?.col, sort?.desc, knownTotal.current).then((d) => ((knownTotal.current = d.total), d)),
    [slug, table.schema, table.name, page, sort?.col, sort?.desc],
  )
  const reload = () => {
    knownTotal.current = undefined
    refetch()
  }
  const [editing, setEditing] = useState<{ row: number; col: number } | null>(null)
  const [draft, setDraft] = useState('')
  const [sheet, setSheet] = useState<{ mode: 'insert' } | { mode: 'edit'; row: (string | null)[] } | null>(null)
  const [deleting, setDeleting] = useState<(string | null)[] | null>(null)
  const [saved, setSaved] = useState('')
  const cancelled = useRef(false)
  const action = useAction()

  const pages = data ? Math.max(1, Math.ceil(data.total / data.pageSize)) : 1

  async function saveCell(rowIndex: number, colIndex: number) {
    if (!data) return
    const row = data.rows[rowIndex]
    const col = data.columns[colIndex]
    setEditing(null)
    if (cancelled.current) {
      cancelled.current = false
      return
    }
    if (draft === (row[colIndex] ?? '')) return
    const ok = await action.run(() => api.updateRow(slug, table.schema, table.name, keyOf(data, row), { [col.name]: draft }).then(() => true))
    if (ok) {
      setSaved(`${col.name} actualizado.`)
      reload()
    }
  }

  function toggleSort(col: string) {
    setPage(1)
    setSort((s) => (s?.col === col ? (s.desc ? null : { col, desc: true }) : { col, desc: false }))
  }

  return (
    <section aria-labelledby="table-title" className="min-w-0">
      <header className="flex flex-wrap items-end justify-between gap-3 pb-2">
        <div className="min-w-0 px-1">
          <h2 id="table-title" className="truncate font-mono text-[1.1rem] font-semibold">
            {table.schema}.{table.name}
          </h2>
          <p className="text-[0.95rem] text-ink-2" role="status">
            {data ? `${formatNumber(data.total)} registros` : 'Cargando…'}
            {saved && <span className="ml-2 text-accent-text">{saved}</span>}
          </p>
        </div>
        <div className="flex items-center gap-1.5">
          <Button variant="ghost" onClick={reload} aria-label="Recargar registros">
            <RefreshIcon size={18} aria-hidden />
          </Button>
          {data?.editable && (
            <Button variant="primary" onClick={() => setSheet({ mode: 'insert' })}>
              <AddCircleIcon size={18} aria-hidden />
              Agregar registro
            </Button>
          )}
        </div>
      </header>

      {data && !data.editable && (
        <div className="mb-3">
          <Notice>Solo lectura: es una vista o la tabla no tiene clave primaria, así que una fila no se puede identificar de forma segura.</Notice>
        </div>
      )}
      {(error || action.error) && (
        <div className="mb-3">
          <ErrorNote message={error || action.error} />
        </div>
      )}

      <div className="group-box">
        {loading && !data && <Loading label="Cargando registros…" />}
        {data && data.rows.length === 0 && <EmptyState title="Sin registros">{data.editable && 'Usa "Agregar registro" para crear el primero.'}</EmptyState>}
        {data && data.rows.length > 0 && (
          <div className="max-h-[600px] overflow-auto">
            <table className="w-full table-fixed border-separate border-spacing-0 text-[1rem]">
              <caption className="sr-only">
                Registros de {table.name}. Doble clic en una celda para editarla.
              </caption>
              <thead className="sticky top-0 z-10 bg-surface-2">
                <tr>
                  {data.columns.map((c) => (
                    <th
                      key={c.name}
                      scope="col"
                      className="w-[1%] min-w-[120px] border-b border-line p-0 text-left font-semibold"
                      style={{ width: `${Math.max(100 / data.columns.length, 10)}%` }}
                      aria-sort={sort?.col === c.name ? (sort.desc ? 'descending' : 'ascending') : 'none'}
                    >
                      <button type="button" onClick={() => toggleSort(c.name)} className="flex w-full min-w-0 items-center gap-1.5 px-3 py-2 hover:bg-surface-3" title={`${c.type}${c.nullable ? '' : ', NOT NULL'}`}>
                        {c.primaryKey && <KeyMinimalisticIcon size={15} className="shrink-0 text-accent-text" aria-label="clave primaria" />}
                        <span className="truncate font-mono text-[0.95rem]">{c.name}</span>
                        <span className="shrink-0 truncate text-[0.82rem] font-normal text-ink-3">{shortType(c.type)}</span>
                        {sort?.col === c.name && (sort.desc ? <SortFromTopToBottomIcon size={15} aria-hidden className="shrink-0" /> : <SortFromBottomToTopIcon size={15} aria-hidden className="shrink-0" />)}
                      </button>
                    </th>
                  ))}
                  {data.editable && (
                    <th scope="col" className="w-[76px] border-b border-line px-3">
                      <span className="sr-only">Acciones</span>
                    </th>
                  )}
                </tr>
              </thead>
              <tbody>
                {data.rows.map((row, r) => (
                  <tr key={r} className="group hover:bg-surface-2">
                    {row.map((v, c) => {
                      const col = data.columns[c]
                      const isEditing = editing?.row === r && editing.col === c
                      const canEdit = data.editable && !col.primaryKey && !col.generated
                      return (
                        <td
                          key={c}
                          className={`overflow-hidden border-b border-line px-3 py-1.5 font-mono text-[0.95rem] ${canEdit ? 'cursor-text' : ''}`}
                          onDoubleClick={() => {
                            if (!canEdit) return
                            setDraft(v ?? '')
                            setEditing({ row: r, col: c })
                          }}
                          title={canEdit && !isEditing ? 'Doble clic para editar' : undefined}
                        >
                          {isEditing ? (
                            <input
                              autoFocus
                              onFocus={(e) => e.currentTarget.select()}
                              aria-label={`Nuevo valor de ${col.name}`}
                              className="field h-7 min-w-[160px] font-mono text-[0.95rem]"
                              value={draft}
                              onChange={(e) => setDraft(e.target.value)}
                              onBlur={() => saveCell(r, c)}
                              onKeyDown={(e) => {
                                // Saving happens once, on blur; Escape cancels.
                                if (e.key === 'Escape') cancelled.current = true
                                if (e.key === 'Enter' || e.key === 'Escape') e.currentTarget.blur()
                              }}
                            />
                          ) : v === null ? (
                            <span className="text-ink-3 italic">NULL</span>
                          ) : (
                            <span className="block truncate">{v}</span>
                          )}
                        </td>
                      )
                    })}
                    {data.editable && (
                      <td className="border-b border-line px-2 py-1 text-right whitespace-nowrap">
                        <Button variant="ghost" size="sm" className="px-1.5" onClick={() => setSheet({ mode: 'edit', row })} aria-label={`Editar registro ${r + 1}`}>
                          <PenIcon size={17} aria-hidden />
                        </Button>
                        <Button variant="ghost" size="sm" className="px-1.5 hover:text-danger" onClick={() => setDeleting(row)} aria-label={`Eliminar registro ${r + 1}`}>
                          <TrashBinTrashIcon size={17} aria-hidden />
                        </Button>
                      </td>
                    )}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        {data && data.total > data.pageSize && (
          <nav aria-label="Páginas" className="flex items-center justify-between gap-3 bg-surface-2 px-4 py-2 text-[0.95rem]">
            <span className="text-ink-2">
              Página {page} de {pages}
            </span>
            <span className="flex gap-1">
              <Button size="sm" variant="ghost" disabled={page <= 1} onClick={() => setPage(page - 1)} aria-label="Página anterior">
                <AltArrowLeftIcon size={17} aria-hidden />
              </Button>
              <Button size="sm" variant="ghost" disabled={page >= pages} onClick={() => setPage(page + 1)} aria-label="Página siguiente">
                <AltArrowRightIcon size={17} aria-hidden />
              </Button>
            </span>
          </nav>
        )}
      </div>

      {data && sheet && (
        <RowSheet
          data={data}
          row={sheet.mode === 'edit' ? sheet.row : null}
          onClose={() => setSheet(null)}
          onSave={async (values) => {
            const ok = await action.run(() =>
              (sheet.mode === 'edit'
                ? api.updateRow(slug, table.schema, table.name, keyOf(data, sheet.row), values)
                : api.insertRow(slug, table.schema, table.name, values)
              ).then(() => true),
            )
            if (ok) {
              setSaved(sheet.mode === 'edit' ? 'Registro guardado.' : 'Registro agregado.')
              setSheet(null)
              reload()
            }
            return ok ?? false
          }}
          error={action.error}
          busy={action.busy}
        />
      )}

      <Sheet open={deleting !== null} onClose={() => setDeleting(null)} labelledBy="del-row">
        {deleting && data && (
          <>
            <SheetHeader
              id="del-row"
              title="¿Eliminar este registro?"
              subtitle={
                <span className="font-mono">
                  {Object.entries(keyOf(data, deleting))
                    .map(([k, v]) => `${k} = ${v}`)
                    .join(', ')}
                </span>
              }
            />
            <SheetFooter>
              <Button onClick={() => setDeleting(null)}>Cancelar</Button>
              <Button
                variant="danger"
                disabled={action.busy}
                onClick={async () => {
                  const ok = await action.run(() => api.deleteRow(slug, table.schema, table.name, keyOf(data, deleting)).then(() => true))
                  setDeleting(null)
                  if (ok) {
                    setSaved('Registro eliminado.')
                    reload()
                  }
                }}
              >
                Eliminar registro
              </Button>
            </SheetFooter>
          </>
        )}
      </Sheet>
    </section>
  )
}

type FieldState = { value: string; isNull: boolean; useDefault: boolean; touched: boolean }

function RowSheet({
  data,
  row,
  onClose,
  onSave,
  error,
  busy,
}: {
  data: TableData
  row: (string | null)[] | null
  onClose: () => void
  onSave: (values: RowValues) => Promise<boolean>
  error: string
  busy: boolean
}) {
  const insert = row === null
  const [fields, setFields] = useState<FieldState[]>(() =>
    data.columns.map((c, i) => ({
      value: row?.[i] ?? '',
      isNull: row ? row[i] === null : false,
      useDefault: insert && (c.hasDefault || c.generated),
      touched: false,
    })),
  )
  const update = (i: number, patch: Partial<FieldState>) => setFields((f) => f.map((x, k) => (k === i ? { ...x, ...patch, touched: true } : x)))

  async function submit(e: FormEvent) {
    e.preventDefault()
    const values: RowValues = {}
    data.columns.forEach((c, i) => {
      const f = fields[i]
      if (c.generated || (!insert && c.primaryKey)) return
      if (insert && f.useDefault) return
      if (!insert && !f.touched) return
      values[c.name] = f.isNull ? null : f.value
    })
    await onSave(values)
  }

  return (
    <Sheet open onClose={onClose} labelledBy="row-title" wide dismissable={!busy}>
      <form onSubmit={submit}>
        <SheetHeader id="row-title" title={insert ? 'Agregar registro' : 'Editar registro'} subtitle={insert ? 'Los campos con valor por defecto se pueden dejar en automático.' : 'Solo se guardan los campos que cambies.'} />
        <div className="max-h-[60vh] space-y-3 overflow-y-auto px-6 py-3">
          {data.columns.map((c, i) => (
            <Field key={c.name} column={c} state={fields[i]} insert={insert} onChange={(p) => update(i, p)} />
          ))}
          {error && <ErrorNote message={error} />}
        </div>
        <SheetFooter>
          <Button onClick={onClose} disabled={busy}>
            Cancelar
          </Button>
          <Button type="submit" variant="primary" disabled={busy}>
            {busy ? 'Guardando…' : insert ? 'Agregar' : 'Guardar cambios'}
          </Button>
        </SheetFooter>
      </form>
    </Sheet>
  )
}

function Field({ column: c, state, insert, onChange }: { column: DataColumn; state: FieldState; insert: boolean; onChange: (p: Partial<FieldState>) => void }) {
  const locked = c.generated || (!insert && c.primaryKey)
  const disabled = locked || state.isNull || (insert && state.useDefault)
  const id = `field-${c.name}`
  return (
    <div>
      <label htmlFor={id} className="mb-1 flex items-baseline gap-2">
        <span className="font-mono text-[0.95rem] font-semibold">{c.name}</span>
        <span className="text-[0.85rem] text-ink-3">
          {c.type}
          {c.primaryKey && ', clave primaria'}
          {!c.nullable && !c.primaryKey && ', obligatorio'}
          {c.generated && ', generado'}
        </span>
      </label>
      <input id={id} className="field font-mono" disabled={disabled} value={disabled && !locked ? '' : state.value} placeholder={insert && state.useDefault ? 'Automático (valor por defecto)' : state.isNull ? 'NULL' : ''} onChange={(e) => onChange({ value: e.target.value })} />
      {!locked && (
        <div className="mt-1 flex gap-4 text-[0.9rem] text-ink-2">
          {insert && c.hasDefault && (
            <label className="flex items-center gap-1.5">
              <input type="checkbox" className="accent-[var(--accent)]" checked={state.useDefault} onChange={(e) => onChange({ useDefault: e.target.checked })} />
              Usar valor por defecto
            </label>
          )}
          {c.nullable && (
            <label className="flex items-center gap-1.5">
              <input type="checkbox" className="accent-[var(--accent)]" checked={state.isNull} onChange={(e) => onChange({ isNull: e.target.checked })} />
              NULL
            </label>
          )}
        </div>
      )}
    </div>
  )
}
