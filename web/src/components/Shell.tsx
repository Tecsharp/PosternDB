import { useState, type ReactNode } from 'react'
import { applyTheme, getTheme, type Theme } from '../theme'
import {
  ArchiveIcon,
  CloseCircleIcon,
  CodeFileIcon,
  DatabaseIcon,
  HamburgerMenuIcon,
  KeyMinimalisticIcon,
  Logout2Icon,
  MonitorIcon,
  MoonIcon,
  PulseIcon,
  SettingsIcon,
  SunIcon,
  UsersGroupRoundedIcon,
  ClockCircleIcon,
} from '@solar-icons/react/linear'
import { can, type Role, type Session } from '../api'
import { href, linkHandler, type Route } from '../router'
import { Button } from './ui'

/** Server-rack mark in the PachisLab style: "Postern" + green "DB". */
export function Logo({ size = 'md' }: { size?: 'md' | 'lg' }) {
  const box = size === 'lg' ? 'size-11' : 'size-7'
  const text = size === 'lg' ? 'text-[1.9rem]' : 'text-[1.05rem]'
  return (
    <span className="inline-flex items-center gap-2">
      <svg viewBox="0 0 32 32" className={box} aria-hidden="true">
        <rect width="32" height="32" rx="9" fill="var(--accent)" />
        <rect x="7" y="7.5" width="18" height="4.5" rx="1.4" fill="var(--accent-ink)" />
        <rect x="7" y="13.75" width="18" height="4.5" rx="1.4" fill="var(--accent-ink)" />
        <rect x="7" y="20" width="18" height="4.5" rx="1.4" fill="var(--accent-ink)" />
        <circle cx="21.5" cy="9.75" r="1" fill="var(--accent)" />
        <circle cx="21.5" cy="16" r="1" fill="var(--accent)" />
        <circle cx="21.5" cy="22.25" r="1" fill="var(--accent)" />
      </svg>
      <span className={`${text} font-bold tracking-tight`}>
        Postern<span className="text-accent-text">DB</span>
      </span>
    </span>
  )
}

const roleText: Record<Role, string> = { owner: 'Propietario', admin: 'Administrador', viewer: 'Lector', client: 'Usuario' }

interface NavItem {
  to: Route
  label: string
  icon: typeof DatabaseIcon
  match: (r: Route) => boolean
  visible?: (role: Role) => boolean
}

const groups: { label: string; items: NavItem[] }[] = [
  {
    label: 'Operación',
    items: [
      { to: { page: 'databases' }, label: 'Bases de datos', icon: DatabaseIcon, match: (r) => r.page === 'databases' || r.page === 'database' },
      { to: { page: 'backups' }, label: 'Respaldos', icon: ArchiveIcon, match: (r) => r.page === 'backups' },
      { to: { page: 'templates' }, label: 'Plantillas', icon: CodeFileIcon, match: (r) => r.page === 'templates' },
    ],
  },
  {
    label: 'Gestión',
    items: [
      { to: { page: 'activity' }, label: 'Actividad', icon: PulseIcon, match: (r) => r.page === 'activity' },
      { to: { page: 'admins' }, label: 'Administradores', icon: UsersGroupRoundedIcon, match: (r) => r.page === 'admins', visible: can.manageAdmins },
      { to: { page: 'clients' }, label: 'Usuarios', icon: UsersGroupRoundedIcon, match: (r) => r.page === 'clients', visible: can.manageAdmins },
      { to: { page: 'expiry' }, label: 'Expiración', icon: ClockCircleIcon, match: (r) => r.page === 'expiry', visible: can.manageExpiry },
      { to: { page: 'settings' }, label: 'Ajustes', icon: SettingsIcon, match: (r) => r.page === 'settings' },
    ],
  },
]

export function Shell({
  route,
  session,
  onLogout,
  onChangePassword,
  children,
}: {
  route: Route
  session: Session
  onLogout: () => void
  onChangePassword: () => void
  children: ReactNode
}) {
  const [open, setOpen] = useState(false)
  const nav = (
    <nav aria-label="Principal" className="flex flex-1 flex-col gap-5 px-2.5">
      {groups.map((g) => (
        <div key={g.label}>
          <p className="mb-0.5 px-2.5 text-[0.78rem] font-semibold text-ink-3">{g.label}</p>
          <ul>
            {g.items
              .filter((i) => !i.visible || i.visible(session.role))
              .map((i) => {
                const current = i.match(route)
                const to = href(i.to)
                return (
                  <li key={i.label}>
                    <a
                      href={to}
                      onClick={(e) => {
                        setOpen(false)
                        linkHandler(to)(e)
                      }}
                      aria-current={current ? 'page' : undefined}
                      className={`flex h-8 items-center gap-2.5 rounded-[6px] px-2.5 text-[0.95rem] ${
                        current ? 'bg-surface-3 font-medium text-ink' : 'text-ink hover:bg-surface-2'
                      }`}
                    >
                      <i.icon size={19} aria-hidden className={current ? 'text-accent-text' : 'text-ink-2'} />
                      {i.label}
                    </a>
                  </li>
                )
              })}
          </ul>
        </div>
      ))}
    </nav>
  )

  const account = (
    <div>
    <ThemeSwitch />
    <div className="flex items-center gap-2.5 px-4 py-3">
      <span aria-hidden className="grid size-7 shrink-0 place-items-center rounded-full bg-surface-3 text-[0.85rem] font-semibold uppercase">
        {session.user.slice(0, 1)}
      </span>
      <div className="min-w-0 flex-1">
        <p className="truncate text-[0.9rem] font-medium">{session.user}</p>
        <p className="text-[0.82rem] text-ink-3">{roleText[session.role]}</p>
      </div>
      {!session.bootstrap && (
        <Button variant="ghost" size="sm" className="px-1.5" onClick={onChangePassword} aria-label="Cambiar mi contraseña" title="Cambiar mi contraseña">
          <KeyMinimalisticIcon size={18} aria-hidden />
        </Button>
      )}
      <Button variant="ghost" size="sm" className="px-1.5" onClick={onLogout} aria-label="Cerrar sesión" title="Cerrar sesión">
        <Logout2Icon size={18} aria-hidden />
      </Button>
    </div>
    </div>
  )

  return (
    <div className="flex min-h-dvh">
      <a href="#content" className="sr-only z-50 rounded-[6px] bg-accent px-3 py-1.5 text-accent-ink focus:not-sr-only focus:fixed focus:top-3 focus:left-3">
        Saltar al contenido
      </a>

      <aside className="sticky top-0 hidden h-dvh w-[17rem] shrink-0 flex-col gap-5 bg-sidebar pt-4 lg:flex">
        <div className="px-5">
          <Logo />
        </div>
        {nav}
        {account}
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        <div className="sticky top-0 z-30 flex h-12 items-center justify-between bg-sidebar px-4 lg:hidden">
          <Logo />
          <Button variant="ghost" size="sm" aria-expanded={open} aria-controls="mobile-nav" onClick={() => setOpen(!open)}>
            {open ? <CloseCircleIcon size={20} aria-hidden /> : <HamburgerMenuIcon size={20} aria-hidden />}
            <span className="sr-only">Menú</span>
          </Button>
        </div>
        {open && (
          <div id="mobile-nav" className="bg-sidebar pb-2 lg:hidden">
            {nav}
            {account}
          </div>
        )}
        <main id="content" tabIndex={-1} className="flex-1 outline-none">
          {children}
        </main>
      </div>
    </div>
  )
}

/** Minimal top-bar shell for the reduced "panel de usuario": just the logo,
 * theme switch and logout — no admin nav, since a client has one page
 * (their databases) and nothing else to navigate to. */
export function ClientShell({
  session,
  onLogout,
  onChangePassword,
  children,
}: {
  session: Session
  onLogout: () => void
  onChangePassword: () => void
  children: ReactNode
}) {
  return (
    <div className="flex min-h-dvh flex-col">
      <header className="sticky top-0 z-30 flex h-14 items-center justify-between border-b border-hairline bg-sidebar px-4 md:px-6">
        <Logo />
        <div className="flex items-center gap-3">
          <ThemeSwitch compact />
          <span className="hidden max-w-[16rem] truncate text-[0.9rem] text-ink-2 sm:inline" title={session.email ?? session.user}>{session.fullName || session.user}</span>
          <Button variant="ghost" size="sm" className="px-1.5" onClick={onChangePassword} aria-label="Cambiar mi contraseña" title="Cambiar mi contraseña">
            <KeyMinimalisticIcon size={18} aria-hidden />
          </Button>
          <Button variant="ghost" size="sm" className="px-1.5" onClick={onLogout} aria-label="Cerrar sesión" title="Cerrar sesión">
            <Logout2Icon size={18} aria-hidden />
          </Button>
        </div>
      </header>
      <main id="content" tabIndex={-1} className="flex-1 outline-none">
        {children}
      </main>
    </div>
  )
}

const themes: { id: Theme; label: string; icon: typeof SunIcon }[] = [
  { id: 'light', label: 'Claro', icon: SunIcon },
  { id: 'dark', label: 'Oscuro', icon: MoonIcon },
  { id: 'system', label: 'Sistema', icon: MonitorIcon },
]

function ThemeSwitch({ compact = false }: { compact?: boolean }) {
  const [theme, setTheme] = useState<Theme>(getTheme)
  return (
    <div role="group" aria-label="Apariencia" className={`flex rounded-[10px] bg-surface-2 p-0.5 ${compact ? '' : 'mx-4'}`}>
      {themes.map((t) => (
        <button
          key={t.id}
          type="button"
          aria-pressed={theme === t.id}
          title={t.label}
          onClick={() => {
            applyTheme(t.id)
            setTheme(t.id)
          }}
          className={`flex h-8 items-center justify-center gap-1.5 rounded-[8px] text-[0.82rem] ${compact ? 'w-9' : 'flex-1'} ${theme === t.id ? 'bg-surface font-medium text-ink shadow-[0_1px_1px_rgb(0_0_0/0.12)]' : 'text-ink-2 hover:text-ink'}`}
        >
          <t.icon size={17} aria-hidden />
          <span className={compact ? 'sr-only' : ''}>{t.label}</span>
        </button>
      ))}
    </div>
  )
}

export function PageHeader({
  title,
  subtitle,
  actions,
  crumbs,
}: {
  title: ReactNode
  subtitle?: ReactNode
  actions?: ReactNode
  crumbs?: { label: string; to: string }[]
}) {
  return (
    <header className="mx-auto flex max-w-[75rem] flex-wrap items-end justify-between gap-4 px-5 pt-7 pb-5 md:px-8">
      <div className="min-w-0">
        {crumbs && (
          <nav aria-label="Ruta" className="mb-1 text-[0.85rem] text-ink-2">
            {crumbs.map((c) => (
              <span key={c.to}>
                <a href={c.to} onClick={linkHandler(c.to)} className="hover:text-ink">
                  {c.label}
                </a>
                <span aria-hidden className="mx-1 text-ink-3">
                  ›
                </span>
              </span>
            ))}
          </nav>
        )}
        <h1 className="truncate text-[1.9rem] font-bold tracking-[-0.015em]">{title}</h1>
        {subtitle && <p className="mt-0.5 text-[0.95rem] text-ink-2">{subtitle}</p>}
      </div>
      {actions && <div className="flex flex-wrap gap-2">{actions}</div>}
    </header>
  )
}

export function PageBody({ children }: { children: ReactNode }) {
  return <div className="mx-auto max-w-[75rem] space-y-6 px-5 pb-16 md:px-8">{children}</div>
}
