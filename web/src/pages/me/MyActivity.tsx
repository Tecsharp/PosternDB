import { me } from '../../api'
import { useSession } from '../../components/SessionContext'
import { PageBody, PageHeader } from '../../components/Shell'
import { Button, EmptyState, ErrorNote, Loading, useLoad } from '../../components/ui'
import { formatDateTime } from '../../format'
import { actionText, alerting, summary } from '../Activity'

/** What happened to the user's account and databases, most recent first. */
export function MyActivity() {
  const { session } = useSession()
  const { data, error, loading, reload } = useLoad(me.activity, [])
  const mine = new Set([session.user, session.email].filter(Boolean))

  function who(actor: string): string {
    if (mine.has(actor)) return 'ti'
    if (actor === 'inactivity-auditor' || actor === 'system') return 'el sistema'
    return actor === 'administrador' ? 'el administrador' : actor
  }

  return (
    <>
      <PageHeader title="Actividad" subtitle="Lo que ha pasado con tu cuenta y tus bases de datos." actions={<Button onClick={reload}>Actualizar</Button>} />
      <PageBody>
        <section className="group-box">
          {loading && !data && <Loading label="Cargando actividad…" />}
          {error && (
            <div className="p-5">
              <ErrorNote message={error} />
            </div>
          )}
          {data && data.length === 0 && <EmptyState title="Todavía no hay actividad">Cuando crees una base de datos o cambies algo de tu cuenta, aparecerá aquí.</EmptyState>}
          <ol className="group-rows">
            {(data ?? []).map((e) => (
              <li key={e.id} className="grid grid-cols-[130px_1fr] gap-x-4 px-5 py-3 text-[0.95rem] max-sm:grid-cols-1">
                <time dateTime={e.occurredAt} className="text-ink-2 tabular-nums">
                  {formatDateTime(e.occurredAt)}
                </time>
                <div className="min-w-0">
                  <p>
                    <span className={alerting.has(e.action) ? 'font-semibold text-warn' : 'font-semibold'}>{actionText[e.action] ?? e.action}</span>
                    {e.target && !mine.has(e.target) && <span className="ml-2 font-mono text-[0.85rem] text-ink-2">{e.target}</span>}
                  </p>
                  <p className="mt-0.5 truncate text-[0.85rem] text-ink-2">{[summary(e), `por ${who(e.actor)}`].filter(Boolean).join('. ')}</p>
                </div>
              </li>
            ))}
          </ol>
        </section>
      </PageBody>
    </>
  )
}
