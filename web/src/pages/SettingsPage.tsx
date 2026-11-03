import { useState } from 'react'
import { api, can } from '../api'
import type { PageProps } from '../App'
import { PageBody, PageHeader } from '../components/Shell'
import { Button, ErrorNote, KeyValue, Notice, Panel, Tag, useAction } from '../components/ui'

export function SettingsPage({ settings, session }: PageProps) {
  const test = useAction()
  const [sent, setSent] = useState(false)
  const channels = settings.notifyChannels

  return (
    <>
      <PageHeader title="Ajustes" subtitle="Configuración activa. Se cambia en el archivo .env y se aplica ejecutando setup de nuevo." />
      <PageBody>
        <div className="grid gap-6 lg:grid-cols-2">
          <Panel title="Publicación">
            <KeyValue
              rows={
                settings.localMode
                  ? [
                      ['Modo', <Tag tone="warn">Local, sin Cloudflare</Tag>],
                      ['Bases en', <span className="font-mono">127.0.0.1:{settings.localDbPort}</span>],
                    ]
                  : [
                      ['Modo', <Tag tone="accent">Cloudflare Tunnel</Tag>],
                      ['Dominio', <span className="font-mono">{settings.baseDomain}</span>],
                      ['Panel', <span className="font-mono">{settings.panelHostname}</span>],
                      ['Hosts de clientes', <span className="font-mono">{settings.exampleHostname.replace('cliente1', '<cliente>')}</span>],
                      ['Puerto local del cliente', <span className="font-mono">{settings.clientLocalPort}</span>],
                    ]
              }
            />
          </Panel>

          <Panel title="Inactividad y respaldos">
            <KeyValue
              rows={[
                ['Expiración', `${settings.expirationDays} días sin conexiones`],
                ['Aviso previo', `${settings.expiringSoonDays} días antes`],
                ['Revisión', `cada ${settings.checkIntervalHours} h`],
                ['Al expirar o eliminar', settings.backupBeforeDelete ? 'Se respalda y luego se borra' : 'Se borra sin respaldo'],
              ]}
            />
          </Panel>

          <Panel title="Notificaciones">
            <div className="space-y-3 px-5 py-4">
              {channels.length > 0 ? (
                <p className="text-[0.95rem]">
                  Canales activos: {channels.map((c) => <Tag key={c} tone="accent">{c === 'webhook' ? 'Webhook' : 'Email'}</Tag>)}
                </p>
              ) : (
                <Notice tone="info">
                  Sin canales. Agrega <span className="font-mono">NOTIFY_WEBHOOK_URL</span> (Discord o Slack) o los ajustes <span className="font-mono">SMTP_*</span> y{' '}
                  <span className="font-mono">NOTIFY_EMAIL_TO</span> en .env para recibir avisos antes de que una base expire.
                </Notice>
              )}
              {can.operate(session.role) && channels.length > 0 && (
                <>
                  <Button
                    size="sm"
                    disabled={test.busy}
                    onClick={async () => {
                      setSent(false)
                      setSent(Boolean(await test.run(() => api.testNotification().then(() => true))))
                    }}
                  >
                    {test.busy ? 'Enviando…' : 'Enviar prueba'}
                  </Button>
                  <p role="status" className="text-[0.9rem] text-accent-text">
                    {sent ? 'Prueba enviada. Revisa el canal.' : ''}
                  </p>
                  {test.error && <ErrorNote message={test.error} />}
                </>
              )}
            </div>
          </Panel>

          <Panel title="Migrar de dominio">
            <div className="space-y-2 px-5 py-4 text-[0.95rem] text-ink-2">
              <p>Para pasar de un dominio compartido a uno propio, cambia BASE_DOMAIN, CLIENT_HOST_PREFIX y PANEL_HOSTNAME en .env, ejecuta setup y luego:</p>
              <pre className="overflow-x-auto rounded-[8px] bg-bg px-3 py-2 font-mono text-[0.85rem] text-ink">docker compose run --rm app migrate-hostnames --dry-run</pre>
            </div>
          </Panel>
        </div>
      </PageBody>
    </>
  )
}
