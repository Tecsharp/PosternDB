const dateFmt = new Intl.DateTimeFormat('es-MX', { day: 'numeric', month: 'short', year: 'numeric' })
const dateTimeFmt = new Intl.DateTimeFormat('es-MX', { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' })
const relFmt = new Intl.RelativeTimeFormat('es-MX', { numeric: 'auto' })
const numFmt = new Intl.NumberFormat('es-MX')

const MINUTE = 60_000
const HOUR = 60 * MINUTE
const DAY = 24 * HOUR

export const formatDate = (iso: string) => dateFmt.format(new Date(iso))
export const formatDateTime = (iso: string) => dateTimeFmt.format(new Date(iso))
export const formatNumber = (n: number) => numFmt.format(n)

/** "hace 3 horas", "ayer", "en 12 días". */
export function formatRelative(iso: string, now = Date.now()): string {
  const diff = new Date(iso).getTime() - now
  const abs = Math.abs(diff)
  if (abs < HOUR) return relFmt.format(Math.round(diff / MINUTE), 'minute')
  if (abs < DAY) return relFmt.format(Math.round(diff / HOUR), 'hour')
  return relFmt.format(Math.round(diff / DAY), 'day')
}

/** Whole days left until iso (never negative). */
export function daysUntil(iso: string, now = Date.now()): number {
  return Math.max(0, Math.ceil((new Date(iso).getTime() - now) / DAY))
}

export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  const units = ['KB', 'MB', 'GB', 'TB']
  let v = bytes / 1024
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v.toFixed(v < 10 ? 1 : 0)} ${units[i]}`
}

export const normalizeName = (raw: string) => raw.trim().toLowerCase()
export const SLUG_PATTERN = /^[a-z][a-z0-9]*(-[a-z0-9]+)*$/
export const USER_PATTERN = /^[a-z][a-z0-9]{1,15}$/
export const SLUG_MIN = 3
export const SLUG_MAX = 40

export function slugProblem(name: string): string {
  if (name.length === 0) return ''
  if (name.length < SLUG_MIN) return `Usa al menos ${SLUG_MIN} caracteres.`
  if (name.length > SLUG_MAX) return `Usa como máximo ${SLUG_MAX} caracteres.`
  if (!SLUG_PATTERN.test(name)) return 'Empieza con una letra; usa solo a-z, 0-9 y guiones sueltos.'
  return ''
}
