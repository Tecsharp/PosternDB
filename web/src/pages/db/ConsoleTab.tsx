import { useState, type FormEvent } from 'react'
import { PlayIcon } from '@solar-icons/react/linear'
import { api, can, type QueryResult } from '../../api'
import { Button, ErrorNote, Notice, Panel, useAction } from '../../components/ui'
import { formatNumber } from '../../format'
import type { TabProps } from './DatabaseDetail'

const EXAMPLES = [
  'SELECT table_name FROM information_schema.tables WHERE table_schema = \'public\'',
  'SELECT now(), current_user, version()',
]

export function ConsoleTab({ db, session }: TabProps) {
  const [sql, setSql] = useState(EXAMPLES[0])
  const [result, setResult] = useState<QueryResult | null>(null)
  const [history, setHistory] = useState<string[]>([])
  const { busy, error, run } = useAction()

  if (!can.operate(session.role)) {
    return <Notice tone="info">Tu rol de lector puede ver el esquema, pero no consultar datos.</Notice>
  }

  async function execute(e?: FormEvent) {
    e?.preventDefault()
    const text = sql.trim()
    if (!text) return
    const res = await run(() => api.query(db.slug, text))
    if (res) {
      setResult(res)
      setHistory((h) => [text, ...h.filter((x) => x !== text)].slice(0, 10))
    }
  }

  return (
    <div className="space-y-4">
      <Notice tone="info">
        Solo lectura, solo esta base. Cada consulta corre en una transacción de solo lectura con los permisos de <span className="font-mono">{db.username}</span>: no
        puede crear, modificar ni borrar nada, no puede ver ni tocar otras bases de este servidor, y no tiene acceso al servidor ni a sus archivos. Una sentencia por
        consulta, límite de 10 s y hasta 500 filas. Queda registrada en la actividad.
      </Notice>

      <form onSubmit={execute} className="group-box">
        <label htmlFor="sql" className="sr-only">
          Consulta SQL
        </label>
        <textarea
          id="sql"
          className="block min-h-40 w-full resize-y bg-surface px-4 py-3.5 font-mono text-[0.95rem] leading-relaxed text-ink outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring)] focus-visible:ring-inset"
          spellCheck={false}
          value={sql}
          onChange={(e) => setSql(e.target.value)}
          onKeyDown={(e) => {
            if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') {
              e.preventDefault()
              void execute()
            }
          }}
          aria-describedby="sql-help"
        />
        <div className="flex flex-wrap items-center justify-between gap-3 bg-surface-2 px-4 py-2">
          <span id="sql-help" className="text-[0.85rem] text-ink-3">
            Ctrl o ⌘ + Enter para ejecutar. Una sentencia por consulta.
          </span>
          <div className="flex items-center gap-2">
            {history.length > 0 && (
              <select className="field h-6 w-56 text-[0.85rem]" aria-label="Consultas recientes" value="" onChange={(e) => e.target.value && setSql(e.target.value)}>
                <option value="">Recientes…</option>
                {history.map((h) => (
                  <option key={h} value={h}>
                    {h.slice(0, 60)}
                  </option>
                ))}
              </select>
            )}
            <Button type="submit" variant="primary" size="sm" disabled={busy || !sql.trim()}>
              <PlayIcon size={16} aria-hidden />
              {busy ? 'Ejecutando…' : 'Ejecutar'}
            </Button>
          </div>
        </div>
      </form>

      {error && <ErrorNote message={error} />}
      {result && (
        <Panel
          title="Resultado"
          actions={
            <span className="text-[0.85rem] text-ink-2 tabular-nums" role="status">
              {formatNumber(result.rowCount)} {result.rowCount === 1 ? 'fila' : 'filas'}
              {result.truncated && ' (recortado)'} en {result.durationMs} ms
            </span>
          }
        >
          <ResultGrid result={result} />
        </Panel>
      )}
    </div>
  )
}

export function ResultGrid({ result, maxHeight = 'max-h-[480px]' }: { result: QueryResult; maxHeight?: string }) {
  if (result.columns.length === 0) return <p className="px-5 py-4 text-[0.95rem] text-ink-2">La consulta no devolvió columnas.</p>
  return (
    <div className={`overflow-auto ${maxHeight}`}>
      <table className="w-full border-separate border-spacing-0 font-mono text-[0.85rem]">
        <thead className="sticky top-0 bg-surface-2">
          <tr>
            {result.columns.map((c, i) => (
              <th key={`${c}-${i}`} scope="col" className="border-b border-line px-3 py-2 text-left font-semibold whitespace-nowrap">
                {c}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {result.rows.map((row, r) => (
            <tr key={r} className="hover:bg-surface-2">
              {row.map((v, c) => (
                <td key={c} className="max-w-[320px] truncate border-b border-line px-3 py-1.5 whitespace-nowrap" title={v ?? 'NULL'}>
                  {v === null ? <span className="text-ink-3 italic">NULL</span> : v}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
