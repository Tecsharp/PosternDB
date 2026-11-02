import { useState, type FormEvent } from 'react'
import { UserPlusRoundedIcon } from '@solar-icons/react/linear'
import { api, can, type Access, type Credentials, type DatabaseUser } from '../../api'
import { CredentialsSheet } from '../../components/CredentialsSheet'
import { Button, EmptyState, ErrorNote, Loading, Panel, Sheet, SheetFooter, SheetHeader, Tag, useAction, useLoad } from '../../components/ui'
import { formatDate, USER_PATTERN } from '../../format'
import type { TabProps } from './DatabaseDetail'

export function UsersTab({ db, session }: TabProps) {
  const { data, error, loading, reload } = useLoad(() => api.users(db.slug), [db.slug])
  const [adding, setAdding] = useState(false)
  const [creds, setCreds] = useState<Credentials | null>(null)
  const [removing, setRemoving] = useState<DatabaseUser | null>(null)
  const action = useAction()
  const operate = can.operate(session.role)

  return (
    <>
      <Panel
        title="Usuarios adicionales"
        actions={
          operate && (
            <Button size="sm" variant="primary" onClick={() => setAdding(true)}>
              <UserPlusRoundedIcon size={15} aria-hidden />
              Agregar usuario
            </Button>
          )
        }
      >
        <p className="px-4 py-3 text-[13px] text-ink-2">
          Además de <span className="font-mono text-ink">{db.username}</span> (dueño), puedes dar acceso de solo lectura, por ejemplo para un analista, o de
          lectura y escritura sin permisos para borrar tablas.
        </p>
        {loading && !data && <Loading label="Cargando…" />}
        {error && <div className="p-5"><ErrorNote message={error} /></div>}
        {action.error && <div className="p-5"><ErrorNote message={action.error} /></div>}
        {data && data.length === 0 && <EmptyState title="Sin usuarios adicionales" />}
        {data && data.length > 0 && (
          <ul className="group-rows">
            {data.map((u) => (
              <li key={u.id} className="flex flex-wrap items-center gap-3 px-5 py-3">
                <div className="min-w-0 flex-1">
                  <p className="font-mono text-[13px] font-semibold">{u.username}</p>
                  <p className="text-[12px] text-ink-2">Creado el {formatDate(u.createdAt)}</p>
                </div>
                <Tag tone={u.access === 'write' ? 'warn' : 'accent'}>{u.access === 'write' ? 'Lectura y escritura' : 'Solo lectura'}</Tag>
                {operate && (
                  <>
                    <Button size="sm" disabled={action.busy} onClick={async () => setCreds((await action.run(() => api.resetUserPassword(db.slug, u.name))) ?? null)}>
                      Nueva contraseña
                    </Button>
                    <Button size="sm" variant="ghost" className="hover:text-danger" onClick={() => setRemoving(u)}>
                      Eliminar
                    </Button>
                  </>
                )}
              </li>
            ))}
          </ul>
        )}
      </Panel>

      <AddUserSheet
        open={adding}
        slug={db.slug}
        onClose={() => setAdding(false)}
        onCreated={(c) => {
          setAdding(false)
          setCreds(c)
          reload()
        }}
      />
      <CredentialsSheet credentials={creds} title={creds ? `Credenciales de ${creds.username}` : ''} onClose={() => setCreds(null)} />
      <Sheet open={removing !== null} onClose={() => setRemoving(null)} labelledBy="rm-user">
        {removing && (
          <>
            <SheetHeader id="rm-user" title={`¿Eliminar ${removing.username}?`} subtitle="Se revocan sus permisos y se borra el usuario. Sus sesiones abiertas se cortan al reconectar." />
            <SheetFooter>
              <Button onClick={() => setRemoving(null)}>Cancelar</Button>
              <Button
                variant="danger"
                disabled={action.busy}
                onClick={async () => {
                  await action.run(() => api.deleteUser(db.slug, removing.name))
                  setRemoving(null)
                  reload()
                }}
              >
                Eliminar usuario
              </Button>
            </SheetFooter>
          </>
        )}
      </Sheet>
    </>
  )
}

function AddUserSheet({ open, slug, onClose, onCreated }: { open: boolean; slug: string; onClose: () => void; onCreated: (c: Credentials) => void }) {
  const [name, setName] = useState('')
  const [access, setAccess] = useState<Access>('read')
  const { busy, error, run } = useAction()
  const clean = name.trim().toLowerCase()
  const invalid = clean.length > 0 && !USER_PATTERN.test(clean)

  async function submit(e: FormEvent) {
    e.preventDefault()
    const c = await run(() => api.createUser(slug, clean, access))
    if (c) {
      setName('')
      onCreated(c)
    }
  }

  return (
    <Sheet open={open} onClose={onClose} labelledBy="add-user">
      <form onSubmit={submit}>
        <SheetHeader id="add-user" title="Agregar usuario" />
        <div className="space-y-4 px-6 py-5">
          <label className="block">
            <span className="label">Nombre corto</span>
            <input className="field font-mono" autoFocus required placeholder="bi" value={name} aria-invalid={invalid ? 'true' : 'false'} aria-describedby="user-help" onChange={(e) => setName(e.target.value)} />
            <span id="user-help" className={`mt-1.5 block text-[12px] ${invalid ? 'text-danger' : 'text-ink-2'}`}>
              {invalid ? 'Empieza con una letra; 2 a 16 caracteres a-z o 0-9.' : <>El usuario será <span className="font-mono text-ink">pdb_{slug.replaceAll('-', '_')}__{clean || 'nombre'}</span></>}
            </span>
          </label>
          <fieldset>
            <legend className="label">Acceso</legend>
            {(['read', 'write'] as Access[]).map((a) => (
              <label key={a} className="flex items-start gap-2.5 py-1.5 text-[13px]">
                <input type="radio" name="access" className="mt-0.5 accent-[var(--accent)]" checked={access === a} onChange={() => setAccess(a)} />
                <span>
                  <span className="font-semibold">{a === 'read' ? 'Solo lectura' : 'Lectura y escritura'}</span>
                  <span className="block text-[12px] text-ink-2">
                    {a === 'read' ? 'SELECT en todas las tablas, incluidas las futuras.' : 'SELECT, INSERT, UPDATE y DELETE. No puede crear ni borrar tablas.'}
                  </span>
                </span>
              </label>
            ))}
          </fieldset>
          {error && <ErrorNote message={error} />}
        </div>
        <SheetFooter>
          <Button onClick={onClose}>Cancelar</Button>
          <Button type="submit" variant="primary" disabled={busy || !clean || invalid}>
            {busy ? 'Creando…' : 'Crear usuario'}
          </Button>
        </SheetFooter>
      </form>
    </Sheet>
  )
}
