import { useState } from 'react'
import { api } from '../../api'
import { CodeBlock, ErrorNote, KeyValue, Loading, Notice, Panel, useLoad } from '../../components/ui'
import { buildSnippets } from '../../snippets'
import type { TabProps } from './DatabaseDetail'

export function ConnectTab({ db }: TabProps) {
  const { data: info, error, loading } = useLoad(() => api.connection(db.slug), [db.slug])
  const [picked, setPicked] = useState<string | null>(null)

  if (loading && !info) return <Loading label="Cargando…" />
  if (error || !info) return <ErrorNote message={error} />
  const snippets = buildSnippets(info)
  const current = snippets.find((s) => s.id === picked) ?? snippets[0]

  return (
    <div className="grid gap-6 lg:grid-cols-[320px_1fr]">
      <div className="space-y-4">
        <Panel title="Datos de conexión">
          <KeyValue
            stacked
            rows={[
              [info.localMode ? 'Servidor' : 'Host público', <span className="font-mono text-[0.9rem] break-all">{info.localMode ? `127.0.0.1:${info.port}` : info.hostname}</span>],
              ['Puerto local', <span className="font-mono">{info.port}</span>],
              ['Base de datos', <span className="font-mono">{info.database}</span>],
              ['Usuario', <span className="font-mono">{info.username}</span>],
            ]}
          />
        </Panel>
        <Notice tone="info">
          La contraseña solo se muestra al crear la base. Si la perdiste, genera una nueva en <strong>Ajustes</strong> sin tocar los datos.
          {!info.localMode && (
            <span className="mt-1.5 block">
              <a className="text-accent-text hover:underline" href={`/ayuda?host=${encodeURIComponent(info.hostname)}`} target="_blank" rel="noopener noreferrer">
                Ver tutorial: cómo abrir el puente con cloudflared
              </a>
            </span>
          )}
        </Notice>
        <nav aria-label="Fragmentos" className="group-box p-1.5">
          <ul>
            {snippets.map((s) => (
              <li key={s.id}>
                <button
                  type="button"
                  onClick={() => setPicked(s.id)}
                  aria-current={s.id === current.id ? 'true' : undefined}
                  className={`w-full rounded-[6px] px-2.5 py-1.5 text-left text-[0.95rem] ${s.id === current.id ? 'bg-surface-3 font-medium' : 'text-ink hover:bg-surface-2'}`}
                >
                  {s.title}
                </button>
              </li>
            ))}
          </ul>
        </nav>
      </div>
      <Panel title={current.title}>
        <div className="space-y-3 px-5 py-4">
          <p className="text-[0.95rem] text-ink-2">{current.description}</p>
          <CodeBlock code={current.code} label={current.title} />
          {!info.localMode && current.id !== 'cloudflared' && current.id !== 'compose' && current.id !== 'systemd' && current.id !== 'launchd' && (
            <p className="text-[0.85rem] text-ink-3">Requiere el túnel abierto en esa máquina (ver "Abrir el túnel" o el servicio para tu sistema).</p>
          )}
        </div>
      </Panel>
    </div>
  )
}
