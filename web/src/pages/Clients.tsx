import { useState, type FormEvent } from 'react'
import { UserPlusRoundedIcon } from '@solar-icons/react/linear'
import { api, can, type ClientUser } from '../api'
import type { PageProps } from '../App'
import { PageBody, PageHeader } from '../components/Shell'
import { Button, ErrorNote, Loading, Notice, SecretField, Sheet, SheetFooter, SheetHeader, Tag, useAction, useLoad } from '../components/ui'
import { formatRelative } from '../format'

/**
 * Owner-only management of self-service tenants: people who register
 * themselves (or whom the owner adds directly) to use the reduced "panel de
 * usuario" — only their own databases, never the admin panel. The owner can
 * move each one's expiry exception here too; it applies to every database
 * that client owns and has no exception of its own.
 */
export function Clients({ session }: PageProps) {
  const { data, error, loading, reload } = useLoad(() => (can.manageAdmins(session.role) ? api.clients() : Promise.resolve([])), [session.role])
  const [adding, setAdding] = useState(false)
  const [secret, setSecret] = useState<{ user: string; password: string } | null>(null)
  const [removing, setRemoving] = useState<ClientUser | null>(null)
  const action = useAction()

  async function resetPassword(u: ClientUser) {
    const r = await action.run(() => api.resetClientPassword(u.id))
    if (r) setSecret({ user: u.email || u.username, password: r.password })
  }

  if (!can.manageAdmins(session.role)) {
    return (
      <PageBody>
        <div className="pt-8">
          <Notice tone="info">Solo un propietario puede gestionar usuarios.</Notice>
        </div>
      </PageBody>
    )
  }

  return (
    <>
      <PageHeader
        title="Usuarios"
        subtitle="Cuentas que administran solo sus propias bases de datos, desde el panel reducido."
        actions={
          <Button variant="primary" onClick={() => setAdding(true)}>
            <UserPlusRoundedIcon size={18} aria-hidden />
            Agregar usuario
          </Button>
        }
      />
      <PageBody>
        {(error || action.error) && (
          <div className="mb-4">
            <ErrorNote message={error || action.error} />
          </div>
        )}
        <section className="group-box">
          {loading && !data && <Loading label="Cargando…" />}
          <ul className="group-rows">
            {(data ?? []).map((u) => <ClientRow key={u.id} client={u} onChanged={reload} onRemove={() => setRemoving(u)} onResetPassword={() => resetPassword(u)} />)}
            {data && data.length === 0 && <li className="px-5 py-6 text-[0.95rem] text-ink-2">Todavía no hay usuarios registrados.</li>}
          </ul>
        </section>
      </PageBody>

      <AddClientSheet
        open={adding}
        onClose={() => setAdding(false)}
        onCreated={(user, password) => {
          setAdding(false)
          setSecret({ user, password })
          reload()
        }}
      />
      <Sheet open={secret !== null} onClose={() => setSecret(null)} labelledBy="client-secret" dismissable={false}>
        {secret && (
          <>
            <SheetHeader id="client-secret" title={`Contraseña de ${secret.user}`} subtitle="Compártela por un canal seguro. Solo se muestra ahora; podrá cambiarla al entrar." />
            <div className="px-6 py-5">
              <div className="group-box bg-surface-2 px-4 py-3">
                <SecretField label="contraseña" value={secret.password} />
              </div>
            </div>
            <SheetFooter>
              <Button variant="primary" onClick={() => setSecret(null)}>
                Listo
              </Button>
            </SheetFooter>
          </>
        )}
      </Sheet>
      <Sheet open={removing !== null} onClose={() => setRemoving(null)} labelledBy="rm-client">
        {removing && (
          <>
            <SheetHeader
              id="rm-client"
              title={`¿Quitar a ${removing.username}?`}
              subtitle="Pierde el acceso de inmediato. Sus bases de datos no se borran: quedan sin usuario y vuelven a la política global de expiración."
            />
            <SheetFooter>
              <Button onClick={() => setRemoving(null)}>Cancelar</Button>
              <Button
                variant="danger"
                onClick={async () => {
                  await action.run(() => api.deleteClient(removing.id))
                  setRemoving(null)
                  reload()
                }}
              >
                Quitar acceso
              </Button>
            </SheetFooter>
          </>
        )}
      </Sheet>
    </>
  )
}

export function ClientRow({ client, onChanged, onRemove, onResetPassword }: { client: ClientUser; onChanged: () => void; onRemove?: () => void; onResetPassword?: () => void }) {
  const [never, setNever] = useState(client.neverExpires)
  const [custom, setCustom] = useState(client.maxIdleDays !== undefined)
  const [days, setDays] = useState(String(client.maxIdleDays ?? ''))
  const save = useAction()

  async function submit(e: FormEvent) {
    e.preventDefault()
    const ok = await save.run(() => api.setClientExpiry(client.id, never, custom && !never && days ? Number(days) : null).then(() => true))
    if (ok) onChanged()
  }

  return (
    <li className="px-5 py-3.5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="min-w-0 flex-1">
          <p className="flex flex-wrap items-center gap-2 text-[0.97rem] font-semibold">
            {client.fullName || client.username}
            {client.googleLinked && <Tag tone="accent">Google</Tag>}
          </p>
          {client.fullName && <p className="font-mono text-[0.85rem] text-ink-2">{client.email || client.username}</p>}
          <p className="text-[0.85rem] text-ink-2">
            {client.databaseCount ?? 0} {client.databaseCount === 1 ? 'base de datos' : 'bases de datos'}
            {' · '}
            {client.createdBy ? `agregado por ${client.createdBy}` : 'se registró solo'}
            {client.lastLoginAt ? `, último acceso ${formatRelative(client.lastLoginAt)}` : ', nunca ha entrado'}
          </p>
        </div>
        {client.neverExpires || client.maxIdleDays !== undefined ? (
          <Tag tone="accent">{client.neverExpires ? 'Nunca expira' : `${client.maxIdleDays} días`}</Tag>
        ) : (
          <Tag tone="neutral">Política global</Tag>
        )}
        <form className="flex flex-wrap items-center gap-3" onSubmit={submit}>
          <label className="flex items-center gap-1.5 text-[0.85rem]">
            <input type="checkbox" className="accent-[var(--accent)]" checked={never} onChange={(e) => setNever(e.target.checked)} />
            Nunca expira
          </label>
          <label className={`flex items-center gap-1.5 text-[0.85rem] ${never ? 'opacity-40' : ''}`}>
            <input type="checkbox" className="accent-[var(--accent)]" disabled={never} checked={custom} onChange={(e) => setCustom(e.target.checked)} />
            Días propios
          </label>
          {custom && !never && (
            <input
              className="field h-8 w-20 font-mono"
              type="number"
              min={1}
              max={3650}
              value={days}
              onChange={(e) => setDays(e.target.value)}
              aria-label={`Días propios para ${client.email || client.username}`}
            />
          )}
          <Button size="sm" type="submit" variant="primary" disabled={save.busy}>
            {save.busy ? 'Guardando…' : 'Guardar'}
          </Button>
          {onResetPassword && (
            <Button size="sm" variant="ghost" onClick={onResetPassword}>
              Nueva contraseña
            </Button>
          )}
          {onRemove && (
            <Button size="sm" variant="ghost" className="hover:text-danger" onClick={onRemove}>
              Quitar
            </Button>
          )}
        </form>
      </div>
      {save.error && (
        <div className="mt-2">
          <ErrorNote message={save.error} />
        </div>
      )}
    </li>
  )
}

function AddClientSheet({ open, onClose, onCreated }: { open: boolean; onClose: () => void; onCreated: (u: string, p: string) => void }) {
  const [email, setEmail] = useState('')
  const [fullName, setFullName] = useState('')
  const { busy, error, run } = useAction()

  async function submit(e: FormEvent) {
    e.preventDefault()
    const r = await run(() => api.createClient(email, fullName))
    if (r) {
      setEmail('')
      setFullName('')
      onCreated(r.client.email || r.client.username, r.password)
    }
  }

  return (
    <Sheet open={open} onClose={onClose} labelledBy="add-client">
      <form onSubmit={submit}>
        <SheetHeader id="add-client" title="Agregar usuario" subtitle="Se genera una contraseña que verás una sola vez. Entrará al panel reducido, no al panel admin. También podrá entrar con Google usando ese correo." />
        <div className="space-y-4 px-6 py-5">
          <label className="block">
            <span className="label">Nombre completo</span>
            <input className="field" autoFocus required minLength={2} maxLength={80} placeholder="Ana López" value={fullName} onChange={(e) => setFullName(e.target.value)} />
          </label>
          <label className="block">
            <span className="label">Correo</span>
            <input className="field" type="email" required autoCapitalize="none" spellCheck={false} placeholder="ana@ejemplo.com" value={email} onChange={(e) => setEmail(e.target.value)} />
          </label>
          {error && <ErrorNote message={error} />}
        </div>
        <SheetFooter>
          <Button onClick={onClose}>Cancelar</Button>
          <Button type="submit" variant="primary" disabled={busy || !email.trim() || !fullName.trim()}>
            {busy ? 'Creando…' : 'Crear acceso'}
          </Button>
        </SheetFooter>
      </form>
    </Sheet>
  )
}
