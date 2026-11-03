import { useState } from 'react'
import type { Credentials } from '../api'
import { buildSnippets } from '../snippets'
import { Button, CodeBlock, CopyButton, KeyValue, Notice, SecretField, Sheet, SheetFooter, SheetHeader } from './ui'

/**
 * Shows credentials exactly once (after create, password reset or new
 * database user). It cannot be dismissed with Escape so the password is not
 * lost by accident.
 */
export function CredentialsSheet({
  credentials,
  title,
  onClose,
}: {
  credentials: Credentials | null
  title: string
  onClose: () => void
}) {
  const [snippet, setSnippet] = useState('spring')
  if (!credentials) return null
  const snippets = buildSnippets(credentials, credentials.password)
  const current = snippets.find((s) => s.id === snippet) ?? snippets[0]
  const endpoint = credentials.localMode ? `localhost:${credentials.port}` : credentials.hostname

  return (
    <Sheet open onClose={onClose} labelledBy="creds-title" dismissable={false} wide>
      <SheetHeader id="creds-title" title={title} subtitle="Guarda la contraseña ahora: PosternDB no la almacena y no podrá mostrarla otra vez." />
      <div className="space-y-5 overflow-y-auto px-6 py-5">
        <div className="group-box bg-surface-2">
          <KeyValue
            rows={[
              [credentials.localMode ? 'Servidor' : 'Host', <span className="flex items-center gap-2 font-mono">{endpoint}<CopyButton value={endpoint} label="host" compact /></span>],
              ['Base de datos', <span className="flex items-center gap-2 font-mono">{credentials.database}<CopyButton value={credentials.database} label="base de datos" compact /></span>],
              ['Usuario', <span className="flex items-center gap-2 font-mono">{credentials.username}<CopyButton value={credentials.username} label="usuario" compact /></span>],
              ['Contraseña', <SecretField label="contraseña" value={credentials.password} />],
            ]}
          />
        </div>

        {!credentials.localMode && (
          <Notice tone="info">
            Antes de conectar, abre el túnel en la máquina cliente:
            <span className="mt-1.5 block font-mono text-[0.85rem] text-ink">
              cloudflared access tcp --hostname {credentials.hostname} --url localhost:{credentials.port}
            </span>
          </Notice>
        )}

        <div>
          <label htmlFor="snippet-pick" className="label">
            Fragmento de conexión
          </label>
          <select id="snippet-pick" className="field mb-2" value={current.id} onChange={(e) => setSnippet(e.target.value)}>
            {snippets.map((s) => (
              <option key={s.id} value={s.id}>
                {s.title}
              </option>
            ))}
          </select>
          <p className="mb-2 text-[0.85rem] text-ink-2">{current.description}</p>
          <CodeBlock code={current.code} label={current.title} />
        </div>
      </div>
      <SheetFooter>
        <Button variant="primary" onClick={onClose}>
          Ya guardé las credenciales
        </Button>
      </SheetFooter>
    </Sheet>
  )
}
