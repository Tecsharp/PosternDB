import { api } from '../../api'
import { ErrorNote, KeyValue, Loading, Metrics, Panel, useLoad } from '../../components/ui'
import { formatBytes, formatDate, formatDateTime, formatNumber, formatRelative } from '../../format'
import { Lifeline } from '../Databases'
import type { TabProps } from './DatabaseDetail'
import { JobsList } from './BackupsTab'

export function OverviewTab({ db, settings }: TabProps) {
  const active = db.status !== 'expirada'
  const stats = useLoad(() => (active ? api.stats(db.slug) : Promise.resolve(null)), [db.slug, active])
  const jobs = useLoad(() => api.jobs(db.slug), [db.slug])
  const s = stats.data

  return (
    <div className="space-y-6">
      {active && (
        <>
          {stats.loading && !s && <Loading label="Leyendo métricas…" />}
          {stats.error && <ErrorNote message={stats.error} />}
          {s && (
            <Metrics
              items={[
                { label: 'Tamaño en disco', value: formatBytes(s.sizeBytes) },
                { label: 'Tablas', value: formatNumber(s.tableCount), hint: `${formatNumber(s.totalRows)} registros aprox.` },
                { label: 'Conexiones abiertas', value: formatNumber(s.openConnections), hint: 'Sin contar el panel' },
                { label: 'Sesiones totales', value: formatNumber(s.totalSessions), hint: `${formatNumber(s.commits)} commits` },
              ]}
            />
          )}
        </>
      )}

      <div className="grid gap-6 lg:grid-cols-[1.2fr_1fr]">
        {s && s.topTables.length > 0 ? (
          <Panel title="Tablas más grandes">
            <table className="w-full text-left text-[13px]">
              <thead>
                <tr className="text-[12px] text-ink-2">
                  <th scope="col" className="px-4 py-2 font-medium">Tabla</th>
                  <th scope="col" className="px-4 py-2 text-right font-medium">Registros</th>
                  <th scope="col" className="px-4 py-2 text-right font-medium">Tamaño</th>
                </tr>
              </thead>
              <tbody>
                {s.topTables.map((t) => (
                  <tr key={`${t.schema}.${t.name}`} className="border-t border-line">
                    <td className="px-4 py-2.5 font-mono">
                      {t.schema !== 'public' && <span className="text-ink-3">{t.schema}.</span>}
                      {t.name}
                    </td>
                    <td className="px-4 py-2.5 text-right font-mono tabular-nums">{formatNumber(t.rows)}</td>
                    <td className="px-4 py-2.5 text-right font-mono text-ink-2 tabular-nums">{formatBytes(t.sizeBytes)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </Panel>
        ) : (
          <Panel title="Tablas más grandes">
            <p className="px-5 py-6 text-[13px] text-ink-2">{active ? 'Todavía no hay tablas.' : 'La base expiró; sus datos ya no están en línea.'}</p>
          </Panel>
        )}

        <Panel title="Ciclo de vida">
          <KeyValue
            rows={[
              ['Estado', <Lifeline db={db} settings={settings} />],
              ['Última actividad', <span title={formatDateTime(db.lastSeenAt)}>{formatRelative(db.lastSeenAt)}</span>],
              [
                'Expira',
                db.neverExpires ? 'Nunca' : db.expiresAt ? `${formatDate(db.expiresAt)} si nadie se conecta` : '—',
              ],
              ['Límite de inactividad', db.neverExpires ? 'Sin límite' : `${db.maxIdleDays ?? settings.expirationDays} días${db.maxIdleDays ? '' : ' (global)'}`],
              ['Creada', formatDate(db.createdAt)],
              ['Base / usuario', <span className="font-mono text-[12.5px]">{db.database}</span>],
            ]}
          />
        </Panel>
      </div>

      <Panel title="Trabajos recientes">
        {jobs.data && jobs.data.length > 0 ? (
          <JobsList jobs={jobs.data} />
        ) : (
          <p className="px-5 py-6 text-[13px] text-ink-2">Sin respaldos, clonaciones ni restauraciones todavía.</p>
        )}
      </Panel>
    </div>
  )
}
