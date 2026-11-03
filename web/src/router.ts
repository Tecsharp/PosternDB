import { useEffect, useState } from 'react'

// Minimal pathname router. The Go server falls back to index.html for any
// non-API path, so deep links like /databases/acme/schema work on reload.

export type Route =
  | { page: 'databases' }
  | { page: 'database'; slug: string; tab: DatabaseTab }
  | { page: 'backups' }
  | { page: 'templates' }
  | { page: 'activity' }
  | { page: 'admins' }
  | { page: 'clients' }
  | { page: 'expiry' }
  | { page: 'settings' }
  | { page: 'my-databases' }
  | { page: 'my-database'; slug: string; tab: MyDatabaseTab }
  | { page: 'my-backups' }
  | { page: 'my-activity' }
  | { page: 'my-security' }
  | { page: 'my-settings' }
  | { page: 'help' }

export const databaseTabs = ['resumen', 'datos', 'esquema', 'consola', 'conexion', 'usuarios', 'respaldos', 'ajustes'] as const
export type DatabaseTab = (typeof databaseTabs)[number]

export const myDatabaseTabs = ['resumen', 'datos', 'esquema', 'conexion', 'ajustes'] as const
export type MyDatabaseTab = (typeof myDatabaseTabs)[number]

export function parse(path: string): Route {
  const parts = path.split('/').filter(Boolean).map(decodeURIComponent)
  switch (parts[0]) {
    case 'databases':
      if (parts[1]) {
        const tab = (databaseTabs as readonly string[]).includes(parts[2] ?? '') ? (parts[2] as DatabaseTab) : 'resumen'
        return { page: 'database', slug: parts[1], tab }
      }
      return { page: 'databases' }
    case 'backups':
    case 'templates':
    case 'activity':
    case 'admins':
    case 'clients':
    case 'expiry':
    case 'settings':
      return { page: parts[0] }
    case 'mis-bases':
      if (parts[1]) {
        const tab = (myDatabaseTabs as readonly string[]).includes(parts[2] ?? '') ? (parts[2] as MyDatabaseTab) : 'resumen'
        return { page: 'my-database', slug: parts[1], tab }
      }
      return { page: 'my-databases' }
    case 'mis-respaldos':
      return { page: 'my-backups' }
    case 'mi-actividad':
      return { page: 'my-activity' }
    case 'seguridad':
      return { page: 'my-security' }
    case 'ajustes':
      return { page: 'my-settings' }
    case 'ayuda':
      return { page: 'help' }
    default:
      return { page: 'databases' }
  }
}

export function href(route: Route): string {
  if (route.page === 'database') {
    return `/databases/${encodeURIComponent(route.slug)}${route.tab === 'resumen' ? '' : `/${route.tab}`}`
  }
  if (route.page === 'my-database') {
    return `/mis-bases/${encodeURIComponent(route.slug)}${route.tab === 'resumen' ? '' : `/${route.tab}`}`
  }
  if (route.page === 'my-databases') return '/mis-bases'
  if (route.page === 'my-backups') return '/mis-respaldos'
  if (route.page === 'my-activity') return '/mi-actividad'
  if (route.page === 'my-security') return '/seguridad'
  if (route.page === 'my-settings') return '/ajustes'
  if (route.page === 'help') return '/ayuda'
  return `/${route.page}`
}

const listeners = new Set<() => void>()

export function navigate(to: string) {
  if (to === window.location.pathname) return
  window.history.pushState(null, '', to)
  listeners.forEach((l) => l())
}

export function useRoute(): Route {
  const [path, setPath] = useState(window.location.pathname)
  useEffect(() => {
    const update = () => setPath(window.location.pathname)
    listeners.add(update)
    window.addEventListener('popstate', update)
    return () => {
      listeners.delete(update)
      window.removeEventListener('popstate', update)
    }
  }, [])
  return parse(path)
}

/** onClick handler for <a href> that keeps normal browser behaviour for modifier clicks. */
export function linkHandler(to: string) {
  return (e: React.MouseEvent<HTMLAnchorElement>) => {
    if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return
    e.preventDefault()
    navigate(to)
  }
}
