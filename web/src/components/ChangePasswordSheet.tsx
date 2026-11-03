import { useState, type FormEvent } from 'react'
import { Button, ErrorNote, PasswordInput, Sheet, SheetFooter, SheetHeader, useAction } from './ui'

/**
 * Change (or, for an account that only uses Google, create) the password.
 * `needsCurrent` is false when there is no password yet.
 */
export function ChangePasswordSheet({
  open,
  onClose,
  changePassword,
  needsCurrent = true,
  onChanged,
}: {
  open: boolean
  onClose: () => void
  changePassword: (current: string, next: string) => Promise<void>
  needsCurrent?: boolean
  onChanged?: () => void
}) {
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [done, setDone] = useState(false)
  const { busy, error, run } = useAction()

  function close() {
    setCurrent('')
    setNext('')
    setDone(false)
    onClose()
  }

  async function submit(e: FormEvent) {
    e.preventDefault()
    const ok = await run(() => changePassword(current, next).then(() => true))
    if (ok) {
      setDone(true)
      onChanged?.()
    }
  }

  return (
    <Sheet open={open} onClose={close} labelledBy="pw-title">
      <form onSubmit={submit}>
        <SheetHeader
          id="pw-title"
          title={needsCurrent ? 'Cambiar mi contraseña' : 'Crear una contraseña'}
          subtitle={needsCurrent ? 'Mínimo 12 caracteres.' : 'Aún no tienes contraseña. Mínimo 12 caracteres.'}
        />
        <div className="space-y-4 px-6 py-5">
          {done ? (
            <p role="status" className="text-[0.95rem] text-accent-text">
              Contraseña actualizada.
            </p>
          ) : (
            <>
              {needsCurrent && (
                <label className="block">
                  <span className="label">Contraseña actual</span>
                  <PasswordInput autoComplete="current-password" required value={current} onChange={(e) => setCurrent(e.target.value)} />
                </label>
              )}
              <label className="block">
                <span className="label">Contraseña nueva</span>
                <PasswordInput autoComplete="new-password" minLength={12} required value={next} onChange={(e) => setNext(e.target.value)} />
              </label>
              {error && <ErrorNote message={error} />}
            </>
          )}
        </div>
        <SheetFooter>
          <Button onClick={close}>{done ? 'Cerrar' : 'Cancelar'}</Button>
          {!done && (
            <Button type="submit" variant="primary" disabled={busy}>
              {busy ? 'Guardando…' : needsCurrent ? 'Cambiar contraseña' : 'Crear contraseña'}
            </Button>
          )}
        </SheetFooter>
      </form>
    </Sheet>
  )
}
