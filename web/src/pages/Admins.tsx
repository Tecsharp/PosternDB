import { useState, type FormEvent } from 'react'
import { UserPlusRoundedIcon } from '@solar-icons/react/linear'
import { api, can, type Admin, type Role } from '../api'
import type { PageProps } from '../App'
import { PageBody, PageHeader } from '../components/Shell'
import { Button, ErrorNote, Loading, Notice, SecretField, Sheet, SheetFooter, SheetHeader, Tag, useAction, useLoad } from '../components/ui'
import { formatDate, formatRelative } from '../format'

const roles: { id: Role; label: string; hint: string }[] = [
  { id: 'viewer', label: 'Lector', hint: 'Ve bases, métricas, esquemas, respaldos y actividad. No ve datos ni cambia nada.' },
  { id: 'admin', label: 'Administrador', hint: 'Además crea, elimina, respalda y consulta bases, y gestiona plantillas.' },
  { id: 'owner', label: 'Propietario', hint: 'Además gestiona administradores.' },
]
const roleLabel = (r: Role) => roles.find((x) => x.id === r)?.label ?? r

export function Admins({ session }: PageProps) {
  const { data, error, loading, reload } = useLoad(() => (can.manageAdmins(session.role) ? api.admins() : Promise.resolve([])), [session.role])
  const [adding, setAdding] = useState(false)
  const [secret, setSecret] = useState<{ user: string; password: string } | null>(null)
  const [removing, setRemoving] = useState<Admin | null>(null)
  const action = useAction()

  if (!can.manageAdmins(session.role)) {
    return (
      <PageBody>
        <div className="pt-8">
          <Notice tone="info">Solo un propietario puede gestionar administradores.</Notice>
        </div>
      </PageBody>
    )
  }

  return (
    <>
      <PageHeader
        title="Administradores"
        subtitle="Quién puede entrar al panel y qué puede hacer."
        actions={
          <Button variant="primary" onClick={() => setAdding(true)}>
            <UserPlusRoundedIcon size={18} aria-hidden />
            Invitar administrador
          </Button>
        }
      />
      <PageBody>
        {(error || action.error) && <div className="mb-4"><ErrorNote message={error || action.error} /></div>}
        <section className="group-box">
          {loading && !data && <Loading label="Cargando…" />}
          <ul className="group-rows">
            {(data ?? []).map((a) => {
              const self = a.username === session.user
              return (
                <li key={a.username} className="flex flex-wrap items-center gap-3 px-5 py-3.5">
                  <div className="min-w-0 flex-1">
                    <p className="text-[0.97rem] font-semibold">
                      {a.username} {self && <span className="font-normal text-ink-2">(tú)</span>}
                    </p>
                    <p className="text-[0.85rem] text-ink-2">
                      {a.bootstrap
                        ? 'Definido en .env (ADMIN_USER). No se puede modificar desde aquí.'
                        : `Creado por ${a.createdBy} el ${formatDate(a.createdAt)}${a.lastLoginAt ? `, último acceso ${formatRelative(a.lastLoginAt)}` : ', nunca ha entrado'}`}
                    </p>
                  </div>
                  {a.bootstrap || self ? (
                    <Tag tone="accent">{roleLabel(a.role)}</Tag>
                  ) : (
                    <>
                      <label className="sr-only" htmlFor={`role-${a.username}`}>
                        Rol de {a.username}
                      </label>
                      <select
                        id={`role-${a.username}`}
                        className="field h-8 w-40 text-[0.9rem]"
                        value={a.role}
                        onChange={async (e) => {
                          await action.run(() => api.setAdminRole(a.username, e.target.value as Role))
                          reload()
                        }}
                      >
                        {roles.map((r) => (
                          <option key={r.id} value={r.id}>
                            {r.label}
                          </option>
                        ))}
                      </select>
                      <Button
                        size="sm"
                        onClick={async () => {
                          const r = await action.run(() => api.resetAdminPassword(a.username))
                          if (r) setSecret({ user: a.username, password: r.password })
                        }}
                      >
                        Nueva contraseña
                      </Button>
                      <Button size="sm" variant="ghost" className="hover:text-danger" onClick={() => setRemoving(a)}>
                        Quitar
                      </Button>
                    </>
                  )}
                </li>
              )
            })}
          </ul>
        </section>

        <dl className="mt-6 grid gap-3 md:grid-cols-3">
          {roles.map((r) => (
            <div key={r.id} className="px-1">
              <dt className="text-[0.95rem] font-semibold">{r.label}</dt>
              <dd className="mt-1 text-[0.9rem] leading-relaxed text-ink-2">{r.hint}</dd>
            </div>
          ))}
        </dl>
      </PageBody>

      <AddAdminSheet
        open={adding}
        onClose={() => setAdding(false)}
        onCreated={(user, password) => {
          setAdding(false)
          setSecret({ user, password })
          reload()
        }}
      />
      <Sheet open={secret !== null} onClose={() => setSecret(null)} labelledBy="admin-secret" dismissable={false}>
        {secret && (
          <>
            <SheetHeader id="admin-secret" title={`Contraseña de ${secret.user}`} subtitle="Compártela por un canal seguro. Solo se muestra ahora; podrá cambiarla al entrar." />
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
      <Sheet open={removing !== null} onClose={() => setRemoving(null)} labelledBy="rm-admin">
        {removing && (
          <>
            <SheetHeader id="rm-admin" title={`¿Quitar a ${removing.username}?`} subtitle="Pierde el acceso de inmediato, aunque tenga una sesión abierta." />
            <SheetFooter>
              <Button onClick={() => setRemoving(null)}>Cancelar</Button>
              <Button
                variant="danger"
                onClick={async () => {
                  await action.run(() => api.deleteAdmin(removing.username))
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

function AddAdminSheet({ open, onClose, onCreated }: { open: boolean; onClose: () => void; onCreated: (u: string, p: string) => void }) {
  const [username, setUsername] = useState('')
  const [role, setRole] = useState<Role>('viewer')
  const { busy, error, run } = useAction()

  async function submit(e: FormEvent) {
    e.preventDefault()
    const r = await run(() => api.createAdmin(username, role))
    if (r) {
      setUsername('')
      onCreated(r.admin.username, r.password)
    }
  }

  return (
    <Sheet open={open} onClose={onClose} labelledBy="add-admin">
      <form onSubmit={submit}>
        <SheetHeader id="add-admin" title="Invitar administrador" subtitle="Se genera una contraseña que verás una sola vez." />
        <div className="space-y-4 px-6 py-5">
          <label className="block">
            <span className="label">Usuario</span>
            <input className="field" autoFocus required autoCapitalize="none" spellCheck={false} placeholder="ana" value={username} onChange={(e) => setUsername(e.target.value)} />
          </label>
          <fieldset>
            <legend className="label">Rol</legend>
            {roles.map((r) => (
              <label key={r.id} className="flex items-start gap-2.5 py-1.5 text-[0.95rem]">
                <input type="radio" name="role" className="mt-0.5 accent-[var(--accent)]" checked={role === r.id} onChange={() => setRole(r.id)} />
                <span>
                  <span className="font-semibold">{r.label}</span>
                  <span className="block text-[0.85rem] text-ink-2">{r.hint}</span>
                </span>
              </label>
            ))}
          </fieldset>
          {error && <ErrorNote message={error} />}
        </div>
        <SheetFooter>
          <Button onClick={onClose}>Cancelar</Button>
          <Button type="submit" variant="primary" disabled={busy || !username.trim()}>
            {busy ? 'Creando…' : 'Crear acceso'}
          </Button>
        </SheetFooter>
      </form>
    </Sheet>
  )
}
