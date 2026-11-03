import { useId, useMemo, useRef, useState, type FormEvent, type KeyboardEvent } from 'react'
import { api, can, type AccountRole, type ClientUser } from '../api'
import type { PageProps } from '../App'
import { PageBody, PageHeader } from '../components/Shell'
import { Button, ErrorNote, Loading, Notice, Tag, useAction, useLoad } from '../components/ui'
import { formatRelative } from '../format'
import { PersonActivitySheet } from './Activity'
import { accountRoleLabel } from './Clients'

type StaffRole = Exclude<AccountRole, 'user'>

const roles: { id: StaffRole; label: string; hint: string }[] = [
  { id: 'viewer', label: 'Lector', hint: 'Ve bases, métricas, esquemas, respaldos y actividad. No ve datos ni cambia nada.' },
  { id: 'admin', label: 'Administrador', hint: 'Además crea, elimina, respalda y consulta bases, gestiona plantillas, usuarios y roles. No puede tocar al propietario ni la política de expiración.' },
]

const personName = (u: ClientUser) => u.fullName || u.username
const personHandle = (u: ClientUser) => u.email || u.username

/** Names, emails, usernames and personal codes all find the person. */
function matches(u: ClientUser, q: string): boolean {
  const needle = q.trim().toLowerCase()
  if (!needle) return true
  return [u.fullName, u.email, u.username, u.code].some((v) => v?.toLowerCase().includes(needle))
}

/**
 * Admins and users are the same accounts: giving someone a role means picking
 * the existing account, never typing a new name. The list narrows as you type.
 */
export function Admins({ session }: PageProps) {
  const allowed = can.manageUsers(session.role)
  const staff = useLoad(() => (allowed ? api.admins() : Promise.resolve(null)), [allowed])
  const accounts = useLoad(() => (allowed ? api.clients() : Promise.resolve([])), [allowed])
  const [picked, setPicked] = useState<ClientUser | null>(null)
  const [role, setRole] = useState<StaffRole>('viewer')
  const [viewing, setViewing] = useState<ClientUser | null>(null)
  const action = useAction()

  const candidates = useMemo(() => (accounts.data ?? []).filter((u) => u.role === 'user'), [accounts.data])
  const reload = () => {
    staff.reload()
    accounts.reload()
  }

  if (!allowed) {
    return (
      <PageBody>
        <div className="pt-8">
          <Notice tone="info">Solo el propietario y los administradores pueden gestionar roles.</Notice>
        </div>
      </PageBody>
    )
  }

  async function give(e: FormEvent) {
    e.preventDefault()
    if (!picked) return
    const done = await action.run(() => api.setClientRole(picked.id, role).then(() => true))
    if (done) {
      setPicked(null)
      reload()
    }
  }

  async function change(u: ClientUser, next: AccountRole) {
    await action.run(() => api.setClientRole(u.id, next).then(() => true))
    reload()
  }

  return (
    <>
      <PageHeader title="Administradores" subtitle="Quién puede entrar al panel de administración y qué puede hacer. Son las mismas cuentas que los usuarios." />
      <PageBody>
        {(staff.error || accounts.error || action.error) && (
          <div className="mb-4">
            <ErrorNote message={staff.error || accounts.error || action.error} />
          </div>
        )}

        <form onSubmit={give} className="mb-6 rounded-[14px] bg-surface space-y-4 px-5 py-4">
          <div>
            <h2 className="text-[0.97rem] font-semibold">Dar un rol</h2>
            <p className="text-[0.85rem] text-ink-2">Escribe el nombre, correo, usuario o código de una cuenta y elígela de la lista.</p>
          </div>
          <div className="flex flex-wrap items-end gap-3">
            <div className="min-w-[16rem] flex-1">
              <UserPicker candidates={candidates} loading={accounts.loading && !accounts.data} value={picked} onChange={setPicked} />
            </div>
            <label className="block">
              <span className="label">Rol</span>
              <select className="field h-10 w-44" value={role} onChange={(e) => setRole(e.target.value as StaffRole)}>
                {roles.map((r) => (
                  <option key={r.id} value={r.id}>
                    {r.label}
                  </option>
                ))}
              </select>
            </label>
            <Button type="submit" variant="primary" disabled={!picked || action.busy}>
              {action.busy ? 'Guardando…' : 'Dar rol'}
            </Button>
          </div>
        </form>

        <section className="group-box">
          {staff.loading && !staff.data && <Loading label="Cargando…" />}
          <ul className="group-rows">
            {staff.data && (
              <li className="flex flex-wrap items-center gap-3 px-5 py-3.5">
                <div className="min-w-0 flex-1">
                  <p className="text-[0.97rem] font-semibold">
                    {staff.data.owner.username} {staff.data.owner.username === session.user && <span className="font-normal text-ink-2">(tú)</span>}
                  </p>
                  <p className="text-[0.85rem] text-ink-2">Definido en .env (ADMIN_USER). Nadie puede modificarlo desde aquí.</p>
                </div>
                <Tag tone="accent">Propietario</Tag>
              </li>
            )}
            {(staff.data?.admins ?? []).map((a) => {
              const self = a.username === session.user
              return (
                <li key={a.id} className="flex flex-wrap items-center gap-3 px-5 py-3.5">
                  <div className="min-w-0 flex-1">
                    <p className="text-[0.97rem] font-semibold">
                      {personName(a)} {self && <span className="font-normal text-ink-2">(tú)</span>}
                    </p>
                    <p className="text-[0.85rem] text-ink-2">
                      <span className="font-mono">{personHandle(a)}</span>
                      {a.lastLoginAt ? `, último acceso ${formatRelative(a.lastLoginAt)}` : ', nunca ha entrado'}
                    </p>
                  </div>
                  {self ? (
                    <Tag tone="accent">{accountRoleLabel[a.role]}</Tag>
                  ) : (
                    <>
                      <label className="sr-only" htmlFor={`role-${a.id}`}>
                        Rol de {personName(a)}
                      </label>
                      <select id={`role-${a.id}`} className="field h-8 w-40 text-[0.9rem]" value={a.role} onChange={(e) => change(a, e.target.value as AccountRole)}>
                        {roles.map((r) => (
                          <option key={r.id} value={r.id}>
                            {r.label}
                          </option>
                        ))}
                      </select>
                      <Button size="sm" variant="ghost" className="hover:text-danger" onClick={() => change(a, 'user')}>
                        Quitar rol
                      </Button>
                    </>
                  )}
                  <Button size="sm" variant="ghost" onClick={() => setViewing(a)}>
                    Actividad
                  </Button>
                </li>
              )
            })}
            {staff.data && staff.data.admins.length === 0 && <li className="px-5 py-6 text-[0.95rem] text-ink-2">Solo el propietario tiene acceso por ahora.</li>}
          </ul>
        </section>

        <dl className="mt-6 grid gap-3 md:grid-cols-3">
          {[{ id: 'owner', label: 'Propietario', hint: 'Dueño del panel. Se define en .env y no se puede cambiar ni quitar.' }, ...roles].map((r) => (
            <div key={r.id} className="px-1">
              <dt className="text-[0.95rem] font-semibold">{r.label}</dt>
              <dd className="mt-1 text-[0.9rem] leading-relaxed text-ink-2">{r.hint}</dd>
            </div>
          ))}
        </dl>
      </PageBody>
      <PersonActivitySheet person={viewing} onClose={() => setViewing(null)} />
    </>
  )
}

/** Accessible combobox (ARIA 1.2 list pattern): type to filter, arrows to move, Enter to choose. */
function UserPicker({
  candidates,
  loading,
  value,
  onChange,
}: {
  candidates: ClientUser[]
  loading: boolean
  value: ClientUser | null
  onChange: (u: ClientUser | null) => void
}) {
  const id = useId()
  const listId = `${id}-list`
  const input = useRef<HTMLInputElement>(null)
  const [query, setQuery] = useState('')
  const [open, setOpen] = useState(false)
  const [active, setActive] = useState(0)

  const shown = useMemo(() => candidates.filter((u) => matches(u, query)).slice(0, 8), [candidates, query])
  const text = value ? `${personName(value)} · ${personHandle(value)}` : query
  const expanded = open && !value

  function choose(u: ClientUser) {
    onChange(u)
    setOpen(false)
    setQuery('')
  }

  function onKey(e: KeyboardEvent<HTMLInputElement>) {
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      setOpen(true)
      setActive((i) => Math.min(i + 1, Math.max(shown.length - 1, 0)))
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      setActive((i) => Math.max(i - 1, 0))
    } else if (e.key === 'Enter' && expanded && shown[active]) {
      e.preventDefault()
      choose(shown[active])
    } else if (e.key === 'Escape' && expanded) {
      e.stopPropagation()
      setOpen(false)
    } else if (e.key === 'Backspace' && value) {
      onChange(null)
      setQuery('')
    }
  }

  return (
    <div className="relative">
      <label htmlFor={id} className="label">
        Cuenta
      </label>
      <input
        id={id}
        ref={input}
        className="field h-10"
        role="combobox"
        aria-expanded={expanded}
        aria-controls={listId}
        aria-autocomplete="list"
        aria-activedescendant={expanded && shown[active] ? `${id}-opt-${shown[active].id}` : undefined}
        autoComplete="off"
        autoCapitalize="none"
        spellCheck={false}
        placeholder="Ana, ana@ejemplo.com, código…"
        value={text}
        onChange={(e) => {
          onChange(null)
          setQuery(e.target.value)
          setActive(0)
          setOpen(true)
        }}
        onFocus={() => setOpen(true)}
        onBlur={() => setOpen(false)}
        onKeyDown={onKey}
      />
      {value && (
        <button
          type="button"
          className="absolute right-2 top-[2.1rem] text-[0.85rem] text-ink-2 hover:text-ink"
          aria-label="Quitar selección"
          onClick={() => {
            onChange(null)
            setQuery('')
            input.current?.focus()
          }}
        >
          ✕
        </button>
      )}
      {expanded && (
        <ul
          id={listId}
          role="listbox"
          aria-label="Cuentas sin rol"
          className="absolute z-20 mt-1 max-h-72 w-full overflow-auto rounded-[10px] border border-line bg-surface p-1 shadow-lg"
        >
          {loading && <li className="px-3 py-2 text-[0.9rem] text-ink-2">Cargando…</li>}
          {!loading && shown.length === 0 && (
            <li role="presentation" className="px-3 py-2 text-[0.9rem] text-ink-2">
              {query.trim() ? 'Nadie coincide. Solo se puede dar un rol a una cuenta que ya existe.' : 'No hay cuentas sin rol.'}
            </li>
          )}
          {shown.map((u, i) => (
            <li
              key={u.id}
              id={`${id}-opt-${u.id}`}
              role="option"
              aria-selected={i === active}
              className={`cursor-pointer rounded-[6px] px-3 py-2 ${i === active ? 'bg-surface-3' : 'hover:bg-surface-2'}`}
              // mouseDown runs before the input's blur closes the list.
              onMouseDown={(e) => {
                e.preventDefault()
                choose(u)
              }}
              onMouseEnter={() => setActive(i)}
            >
              <p className="text-[0.95rem] font-medium">{personName(u)}</p>
              <p className="font-mono text-[0.82rem] text-ink-2">
                {personHandle(u)}
                {u.code && <span className="ml-2 text-ink-3">código {u.code}</span>}
              </p>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
