import { useCallback, useState } from 'react'
import { me, type Credentials, type Database, type DataColumn, type RowValues, type Table } from '../../api'
import { PageBody, PageHeader } from '../../components/Shell'
import { Button, ErrorNote, KeyValue, Loading, Notice, Panel, Sheet, SheetFooter, SheetHeader, StatusBadge, segmentClass, useAction, useLoad } from '../../components/ui'
import { ActiveConnections } from '../../components/ActiveConnections'
import { CredentialsSheet } from '../../components/CredentialsSheet'
import { formatBytes, formatDate, formatDateTime, formatRelative } from '../../format'
import { href, linkHandler, navigate, type MyDatabaseTab } from '../../router'
import { SchemaExplorer } from '../db/SchemaTab'

const tabs: { id: MyDatabaseTab; label: string }[] = [
  { id: 'overview', label: 'Resumen' },
  { id: 'data', label: 'Datos' },
  { id: 'schema', label: 'Esquema' },
  { id: 'connection', label: 'Conexión' },
  { id: 'settings', label: 'Ajustes' },
]

export function MyDatabaseDetail({ slug, tab }: { slug: string; tab: MyDatabaseTab }) {
  const { data: db, error, loading, reload } = useLoad(() => me.database(slug), [slug])

  if (loading && !db) return <Loading label="Cargando base de datos…" />
  if (error || !db) {
    return (
      <PageBody>
        <div className="pt-8">
          <ErrorNote message={error || 'No encontrada.'} />
        </div>
      </PageBody>
    )
  }

  const current = tabs.some((t) => t.id === tab) ? tab : 'overview'

  return (
    <>
      <PageHeader
        crumbs={[{ label: 'Mis bases de datos', to: href({ page: 'my-databases' }) }]}
        title={
          <span className="flex flex-wrap items-center gap-3">
            {db.slug}
            <StatusBadge status={db.status} />
          </span>
        }
        subtitle={<span className="font-mono">{db.hostname || db.database}</span>}
      />
      <PageBody>
        <nav aria-label="Secciones de la base de datos" className="overflow-x-auto">
          <ul className="inline-flex min-w-max rounded-[7px] bg-surface-2 p-0.5">
            {tabs.map((t) => {
              const to = href({ page: 'my-database', slug: db.slug, tab: t.id })
              const isCurrent = t.id === current
              return (
                <li key={t.id}>
                  <a href={to} onClick={linkHandler(to)} aria-current={isCurrent ? 'page' : undefined} className={segmentClass(isCurrent)}>
                    {t.label}
                  </a>
                </li>
              )
            })}
          </ul>
        </nav>

        {current === 'overview' && <ResumenTab db={db} />}
        {current === 'data' && <DatosTab slug={db.slug} />}
        {current === 'schema' && <SchemaExplorer slug={db.slug} source={{ load: () => me.schema(db.slug), count: (schema, table) => me.count(db.slug, schema, table) }} />}
        {current === 'connection' && <ConexionTab slug={db.slug} />}
        {current === 'settings' && <AjustesTab db={db} onChanged={reload} />}
      </PageBody>
    </>
  )
}

function ResumenTab({ db }: { db: Database }) {
  const { data: stats, error, loading } = useLoad(() => me.stats(db.slug), [db.slug])
  const loadConnections = useCallback(() => me.liveConnections(db.slug), [db.slug])
  if (loading && !stats) return <Loading label="Cargando métricas…" />
  if (error || !stats) return <ErrorNote message={error || 'No disponible.'} />
  return (
    <div className="space-y-6">
      <div className="grid gap-6 lg:grid-cols-2">
        <Panel title="Resumen">
          <KeyValue
            rows={[
              ['Tamaño', formatBytes(stats.sizeBytes)],
              ['Conexiones abiertas', String(stats.openConnections)],
              ['Tablas', String(stats.tableCount)],
              ['Filas totales', String(stats.totalRows)],
            ]}
          />
        </Panel>
        <Panel title="Vigencia">
          <KeyValue
            rows={[
              ['Última actividad', <span title={formatDateTime(db.lastSeenAt)}>{formatRelative(db.lastSeenAt)}</span>],
              ['Expira', db.neverExpires ? 'Nunca' : db.expiresAt ? `${formatDate(db.expiresAt)} si nadie se conecta` : '—'],
              ['Creada', formatDate(db.createdAt)],
            ]}
          />
          <p className="px-5 pb-4 text-[0.85rem] text-ink-2">Conectarte a la base reinicia el contador de inactividad.</p>
        </Panel>
      </div>
      <ActiveConnections load={loadConnections} />
    </div>
  )
}

function ConexionTab({ slug }: { slug: string }) {
  const { data: info, error, loading } = useLoad(() => me.connection(slug), [slug])
  if (loading && !info) return <Loading label="Cargando conexión…" />
  if (error || !info) return <ErrorNote message={error || 'No disponible.'} />
  return (
    <Panel title="Conexión">
      <KeyValue
        rows={[
          [info.localMode ? 'Servidor' : 'Host', <span className="font-mono">{info.localMode ? `localhost:${info.port}` : info.hostname}</span>],
          ['Base de datos', <span className="font-mono">{info.database}</span>],
          ['Usuario', <span className="font-mono">{info.username}</span>],
        ]}
      />
      <p className={`px-5 text-[0.85rem] text-ink-2 ${info.localMode ? 'pb-4' : 'pb-2'}`}>La contraseña no se guarda: genera una nueva en Ajustes si la perdiste.</p>
      {!info.localMode && (
        <p className="px-5 pb-4 text-[0.9rem]">
          <a className="text-accent-text hover:underline" href={`/help?host=${encodeURIComponent(info.hostname)}`} target="_blank" rel="noopener noreferrer">
            Ver tutorial: cómo abrir el puente con cloudflared
          </a>
        </p>
      )}
    </Panel>
  )
}

function AjustesTab({ db, onChanged }: { db: Database; onChanged: () => void }) {
  const [creds, setCreds] = useState<Credentials | null>(null)
  const [deleting, setDeleting] = useState(false)
  const reset = useAction()

  return (
    <div className="grid gap-6 lg:grid-cols-2">
      <Panel title="Contraseña de la base">
        <div className="space-y-3 px-5 py-4">
          <p className="text-[0.95rem] text-ink-2">
            Genera una nueva contraseña para <span className="font-mono text-ink">{db.username}</span>.
          </p>
          {reset.error && <ErrorNote message={reset.error} />}
          <Button size="sm" disabled={reset.busy} onClick={async () => setCreds((await reset.run(() => me.resetPassword(db.slug))) ?? null)}>
            {reset.busy ? 'Generando…' : 'Generar contraseña nueva'}
          </Button>
        </div>
      </Panel>

      <section className="rounded-[10px] bg-danger-soft" aria-labelledby="danger-title">
        <div className="space-y-3 px-5 py-4">
          <h2 id="danger-title" className="text-[1rem] font-semibold text-danger">
            Eliminar base de datos
          </h2>
          <p className="text-[0.95rem] text-ink-2">Se retiran los datos y no se pueden recuperar desde el panel.</p>
          <Button size="sm" variant="danger" onClick={() => setDeleting(true)}>
            Eliminar {db.slug}
          </Button>
        </div>
      </section>

      <CredentialsSheet credentials={creds} title="Contraseña nueva" onClose={() => setCreds(null)} />
      <DeleteSheet
        open={deleting}
        slug={db.slug}
        onClose={() => setDeleting(false)}
        onDeleted={() => {
          onChanged()
          navigate(href({ page: 'my-databases' }))
        }}
      />
    </div>
  )
}

function DeleteSheet({ open, slug, onClose, onDeleted }: { open: boolean; slug: string; onClose: () => void; onDeleted: () => void }) {
  const [typed, setTyped] = useState('')
  const { busy, error, run } = useAction()
  return (
    <Sheet open={open} onClose={onClose} labelledBy="del-my-db" dismissable={!busy}>
      <form
        onSubmit={async (e) => {
          e.preventDefault()
          if (typed !== slug) return
          const ok = await run(() => me.remove(slug).then(() => true))
          if (ok) onDeleted()
        }}
      >
        <SheetHeader id="del-my-db" title={`¿Eliminar ${slug}?`} subtitle="Esta acción no se puede deshacer." />
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

// ---- datos: a compact, no-SQL table browser/editor ------------------------------------

function DatosTab({ slug }: { slug: string }) {
  const { data: schema, error, loading } = useLoad(() => me.schema(slug), [slug])
  const [picked, setPicked] = useState<Table | null>(null)

  if (loading && !schema) return <Loading label="Cargando esquema…" />
  if (error || !schema) return <ErrorNote message={error || 'No disponible.'} />
  const tables = schema.tables.filter((t) => t.kind === 'table')
  const current = picked ?? tables[0] ?? null

  if (tables.length === 0) return <Notice tone="info">Esta base todavía no tiene tablas.</Notice>

  return (
    <div className="space-y-4">
      <label className="block max-w-xs">
        <span className="label">Tabla</span>
        <select
          className="field"
          value={current ? `${current.schema}.${current.name}` : ''}
          onChange={(e) => setPicked(tables.find((t) => `${t.schema}.${t.name}` === e.target.value) ?? null)}
        >
          {tables.map((t) => (
            <option key={`${t.schema}.${t.name}`} value={`${t.schema}.${t.name}`}>
              {t.schema}.{t.name}
            </option>
          ))}
        </select>
      </label>
      {current && <RowsGrid slug={slug} schema={current.schema} table={current.name} />}
    </div>
  )
}

function RowsGrid({ slug, schema, table }: { slug: string; schema: string; table: string }) {
  const { data, error, loading, reload } = useLoad(() => me.tableData(slug, schema, table, 1), [slug, schema, table])
  const [adding, setAdding] = useState(false)
  const action = useAction()

  if (loading && !data) return <Loading label="Cargando filas…" />
  if (error || !data) return <ErrorNote message={error || 'No disponible.'} />

  const keyOf = (row: (string | null)[]): RowValues => {
    const key: RowValues = {}
    data.columns.forEach((c, i) => {
      if (c.primaryKey) key[c.name] = row[i]
    })
    return key
  }

  return (
    <Panel
      title={`${table} (${data.total} filas)`}
      actions={
        data.editable && (
          <Button size="sm" variant="primary" onClick={() => setAdding(true)}>
            Agregar fila
          </Button>
        )
      }
    >
      {action.error && (
        <div className="p-3">
          <ErrorNote message={action.error} />
        </div>
      )}
      <div className="overflow-x-auto">
        <table className="w-full border-separate border-spacing-0 text-left text-[0.9rem]">
          <thead>
            <tr>
              {data.columns.map((c) => (
                <th key={c.name} className="border-b border-line px-3 py-2 font-medium text-ink-2">
                  {c.name}
                </th>
              ))}
              {data.editable && <th className="border-b border-line px-3 py-2" />}
            </tr>
          </thead>
          <tbody>
            {data.rows.map((row, i) => (
              <Row
                key={i}
                row={row}
                columns={data.columns}
                editable={data.editable}
                busy={action.busy}
                onSave={async (values) => {
                  await action.run(() => me.updateRow(slug, schema, table, keyOf(row), values).then(() => true))
                  reload()
                }}
                onDelete={async () => {
                  await action.run(() => me.deleteRow(slug, schema, table, keyOf(row)).then(() => true))
                  reload()
                }}
              />
            ))}
            {data.rows.length === 0 && (
              <tr>
                <td colSpan={data.columns.length + 1} className="px-3 py-6 text-center text-ink-2">
                  Sin filas todavía.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
      <AddRowSheet
        open={adding}
        columns={data.columns}
        onClose={() => setAdding(false)}
        onAdd={async (values) => {
          const ok = await action.run(() => me.insertRow(slug, schema, table, values).then(() => true))
          if (ok) {
            setAdding(false)
            reload()
          }
        }}
      />
    </Panel>
  )
}

function Row({
  row,
  columns,
  editable,
  busy,
  onSave,
  onDelete,
}: {
  row: (string | null)[]
  columns: DataColumn[]
  editable: boolean
  busy: boolean
  onSave: (values: RowValues) => void
  onDelete: () => void
}) {
  const [editing, setEditing] = useState(false)
  const [values, setValues] = useState<RowValues>(() => Object.fromEntries(columns.map((c, i) => [c.name, row[i]])))

  return (
    <tr className="border-b border-line">
      {columns.map((c) => (
        <td key={c.name} className="px-3 py-1.5">
          {editing && editable && !c.generated ? (
            <input
              className="field h-7 w-full font-mono text-[0.85rem]"
              value={values[c.name] ?? ''}
              onChange={(e) => setValues((v) => ({ ...v, [c.name]: e.target.value }))}
            />
          ) : (
            <span className="font-mono">{row[columns.indexOf(c)] ?? <span className="text-ink-3">NULL</span>}</span>
          )}
        </td>
      ))}
      {editable && (
        <td className="px-3 py-1.5 text-right whitespace-nowrap">
          {editing ? (
            <>
              <Button size="sm" variant="primary" disabled={busy} onClick={() => { onSave(values); setEditing(false) }}>
                Guardar
              </Button>
              <Button size="sm" variant="ghost" onClick={() => setEditing(false)}>
                Cancelar
              </Button>
            </>
          ) : (
            <>
              <Button size="sm" onClick={() => setEditing(true)}>
                Editar
              </Button>
              <Button size="sm" variant="ghost" className="hover:text-danger" disabled={busy} onClick={onDelete}>
                Borrar
              </Button>
            </>
          )}
        </td>
      )}
    </tr>
  )
}

function AddRowSheet({ open, columns, onClose, onAdd }: { open: boolean; columns: DataColumn[]; onClose: () => void; onAdd: (values: RowValues) => void }) {
  const [values, setValues] = useState<RowValues>({})
  const editableColumns = columns.filter((c) => !c.generated)

  function close() {
    setValues({})
    onClose()
  }

  return (
    <Sheet open={open} onClose={close} labelledBy="add-row">
      <form
        onSubmit={(e) => {
          e.preventDefault()
          onAdd(values)
        }}
      >
        <SheetHeader id="add-row" title="Agregar fila" subtitle="Deja vacío un campo para usar su valor por defecto." />
        <div className="max-h-[50vh] space-y-3 overflow-y-auto px-6 py-5">
          {editableColumns.map((c) => (
            <label key={c.name} className="block">
              <span className="label">
                {c.name} {c.primaryKey && '(clave)'}
              </span>
              <input
                className="field font-mono"
                placeholder={c.hasDefault ? 'valor por defecto' : c.nullable ? 'NULL' : ''}
                value={values[c.name] ?? ''}
                onChange={(e) => setValues((v) => ({ ...v, [c.name]: e.target.value }))}
              />
            </label>
          ))}
        </div>
        <SheetFooter>
          <Button onClick={close}>Cancelar</Button>
          <Button type="submit" variant="primary">
            Agregar fila
          </Button>
        </SheetFooter>
      </form>
    </Sheet>
  )
}
