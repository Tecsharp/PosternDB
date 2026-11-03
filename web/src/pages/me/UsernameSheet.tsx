import { useState, type FormEvent } from 'react'
import { me } from '../../api'
import { useSession } from '../../components/SessionContext'
import { Button, ErrorNote, Sheet, SheetFooter, SheetHeader, useAction } from '../../components/ui'

export const USERNAME_HINT = 'De 3 a 32 caracteres: letras minúsculas, números, punto, guion o guion bajo. Empieza con una letra.'
const USERNAME_PATTERN = /^[a-z][a-z0-9._-]{2,31}$/

export function usernameProblem(raw: string): string {
  const name = raw.trim().toLowerCase()
  if (!name) return ''
  return USERNAME_PATTERN.test(name) ? '' : USERNAME_HINT
}

/**
 * First thing a person who joined with Google sees: pick a username. It is
 * not dismissable; until they choose one, the panel waits.
 */
export function UsernameSheet({ open }: { open: boolean }) {
  const { session, refresh } = useSession()
  const [name, setName] = useState('')
  const { busy, error, run } = useAction()
  const problem = usernameProblem(name)

  async function submit(e: FormEvent) {
    e.preventDefault()
    if (problem || !name.trim()) return
    const ok = await run(() => me.updateAccount({ username: name }).then(() => true))
    if (ok) await refresh()
  }

  return (
    <Sheet open={open} onClose={() => undefined} labelledBy="username-title" dismissable={false}>
      <form onSubmit={submit}>
        <SheetHeader
          id="username-title"
          title="Elige tu nombre de usuario"
          subtitle={`Podrás entrar con él o con tu correo${session.email ? ` (${session.email})` : ''}. Lo cambias cuando quieras en Seguridad.`}
        />
        <div className="space-y-3 px-6 py-5">
          <label className="block">
            <span className="label">Nombre de usuario</span>
            <input
              className="field font-mono"
              autoFocus
              required
              maxLength={32}
              spellCheck={false}
              autoCapitalize="none"
              autoComplete="username"
              placeholder="ana.lopez"
              value={name}
              aria-invalid={problem ? 'true' : 'false'}
              onChange={(e) => setName(e.target.value)}
            />
          </label>
          <p className={`text-[0.85rem] ${problem ? 'text-danger' : 'text-ink-2'}`} aria-live="polite">
            {USERNAME_HINT}
          </p>
          {error && <ErrorNote message={error} />}
        </div>
        <SheetFooter>
          <Button type="submit" variant="primary" disabled={busy || !name.trim() || !!problem}>
            {busy ? 'Guardando…' : 'Continuar'}
          </Button>
        </SheetFooter>
      </form>
    </Sheet>
  )
}
