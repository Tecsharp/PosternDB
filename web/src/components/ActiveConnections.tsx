import { useCallback, useEffect, useState } from 'react'
import { RefreshIcon } from '@solar-icons/react/linear'
import type { Connection } from '../api'
import { formatRelative } from '../format'
import { Button, ErrorNote, Panel, errorMessage } from './ui'

const REFRESH_MS = 10_000

const stateLabel: Record<string, { text: string; tone: string }> = {
  active: { text: 'Ejecutando', tone: 'text-accent-text' },
  idle: { text: 'En espera', tone: 'text-ink-2' },
  'idle in transaction': { text: 'Transacción abierta', tone: 'text-warn' },
}

/** Sesiones abiertas ahora mismo en la base; se actualiza sola cada 10 s. */
export function ActiveConnections({ load }: { load: () => Promise<Connection[]> }) {
  const [rows, setRows] = useState<Connection[] | null>(null)
  const [error, setError] = useState('')

  const refresh = useCallback(() => {
    load()
      .then((r) => {
        setRows(r)
        setError('')
      })
      .catch((e) => setError(errorMessage(e)))
  }, [load])

  useEffect(() => {
    refresh()
    const timer = setInterval(() => {
      if (!document.hidden) refresh()
    }, REFRESH_MS)
    return () => clearInterval(timer)
  }, [refresh])

  return (
    <Panel
      title={`Conexiones activas${rows ? ` (${rows.length})` : ''}`}
      actions={
        <Button size="sm" variant="ghost" onClick={refresh} aria-label="Actualizar conexiones" title="Actualizar">
          <RefreshIcon size={16} aria-hidden />
        </Button>
      }
    >
      {error && (
        <div className="p-4">
          <ErrorNote message={error} />
        </div>
      )}
      {rows && rows.length === 0 && <p className="px-5 py-6 text-[0.95rem] text-ink-2">Nadie está conectado ahora mismo.</p>}
      {rows && rows.length > 0 && (
        <div className="overflow-x-auto">
          <table className="w-full text-left text-[0.95rem]">
            <thead>
              <tr className="text-[0.85rem] text-ink-2">
                <th scope="col" className="px-4 py-2 font-medium">Usuario</th>
                <th scope="col" className="px-4 py-2 font-medium">Aplicación</th>
                <th scope="col" className="px-4 py-2 font-medium">Estado</th>
                <th scope="col" className="px-4 py-2 font-medium">Conectada</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((c) => {
                const st = stateLabel[c.state] ?? { text: c.state || '—', tone: 'text-ink-2' }
                return (
                  <tr key={c.pid} className="border-t border-line align-top">
                    <td className="px-4 py-2.5 font-mono">{c.user}</td>
                    <td className="px-4 py-2.5 text-ink-2">{c.application || '—'}</td>
                    <td className="px-4 py-2.5">
                      <span className={`font-medium ${st.tone}`}>{st.text}</span>
                      {c.query && <code className="mt-1 block max-w-[28rem] truncate font-mono text-[0.82rem] text-ink-2" title={c.query}>{c.query}</code>}
                    </td>
                    <td className="px-4 py-2.5 text-ink-2">{formatRelative(c.connectedAt)}</td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}
    </Panel>
  )
}
