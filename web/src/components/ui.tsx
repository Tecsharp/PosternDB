import { useCallback, useEffect, useRef, useState, type ButtonHTMLAttributes, type ReactNode } from 'react'
import { CheckCircleIcon, CopyIcon, DangerTriangleIcon, EyeClosedIcon, EyeIcon, InfoCircleIcon, RefreshIcon } from '@solar-icons/react/linear'
import { ApiError, type Status } from '../api'

type Variant = 'primary' | 'secondary' | 'danger' | 'ghost'

const variants: Record<Variant, string> = {
  primary: 'bg-ink text-bg hover:opacity-90 active:opacity-80',
  secondary: 'bg-surface-3 text-ink hover:brightness-110 active:brightness-95',
  danger: 'bg-danger text-danger-ink hover:brightness-110 active:brightness-95',
  ghost: 'text-ink-2 hover:bg-surface-2 hover:text-ink',
}

/** Boton: relleno, sin contorno; el principal es neutro (tinta sobre fondo). */
export function Button({
  variant = 'secondary',
  size = 'md',
  className = '',
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: Variant; size?: 'sm' | 'md' }) {
  const sizing = size === 'sm' ? 'h-8 px-3 text-[0.85rem] gap-1.5' : 'h-10 px-4 text-[0.95rem] gap-2'
  return (
    <button
      type="button"
      className={`inline-flex items-center justify-center rounded-[10px] font-medium whitespace-nowrap transition-[filter,background-color,color] duration-100 disabled:cursor-not-allowed disabled:opacity-40 ${sizing} ${variants[variant]} ${className}`}
      {...props}
    />
  )
}

/** Native <dialog> sheet: focus trap, Escape and inert background for free. */
export function Sheet({
  open,
  onClose,
  labelledBy,
  children,
  dismissable = true,
  wide = false,
}: {
  open: boolean
  onClose: () => void
  labelledBy: string
  children: ReactNode
  dismissable?: boolean
  wide?: boolean
}) {
  const ref = useRef<HTMLDialogElement>(null)
  useEffect(() => {
    const el = ref.current
    if (!el) return
    if (open && !el.open) el.showModal()
    if (!open && el.open) el.close()
  }, [open])
  return (
    <dialog
      ref={ref}
      className={wide ? 'sheet sheet-wide' : 'sheet'}
      aria-labelledby={labelledBy}
      onCancel={(e) => {
        e.preventDefault()
        if (dismissable) onClose()
      }}
    >
      {open && children}
    </dialog>
  )
}

export function SheetHeader({ id, title, subtitle }: { id: string; title: ReactNode; subtitle?: ReactNode }) {
  return (
    <header className="px-6 pt-6 pb-2">
      <h2 id={id} className="text-[1.05rem] font-semibold">
        {title}
      </h2>
      {subtitle && <p className="mt-1 text-[0.95rem] leading-relaxed text-ink-2">{subtitle}</p>}
    </header>
  )
}

export function SheetFooter({ children }: { children: ReactNode }) {
  return <footer className="flex justify-end gap-2 px-6 pt-2 pb-5">{children}</footer>
}

export function CopyButton({ value, label, compact = false }: { value: string; label: string; compact?: boolean }) {
  const [copied, setCopied] = useState(false)
  useEffect(() => {
    if (!copied) return
    const t = setTimeout(() => setCopied(false), 1600)
    return () => clearTimeout(t)
  }, [copied])
  return (
    <>
      <Button
        variant="ghost"
        size="sm"
        className={compact ? 'px-1.5' : ''}
        aria-label={`Copiar ${label}`}
        onClick={async () => {
          await navigator.clipboard.writeText(value)
          setCopied(true)
        }}
      >
        {copied ? <CheckCircleIcon size={17} aria-hidden className="text-accent-text" /> : <CopyIcon size={17} aria-hidden />}
        {!compact && (copied ? 'Copiado' : 'Copiar')}
      </Button>
      <span role="status" className="sr-only">
        {copied ? `${label} copiado al portapapeles` : ''}
      </span>
    </>
  )
}

export function Spinner({ label }: { label: string }) {
  return (
    <span role="status" className="inline-flex items-center gap-2 text-[0.95rem] text-ink-2">
      <RefreshIcon size={18} className="animate-spin" aria-hidden />
      {label}
    </span>
  )
}

export function Loading({ label }: { label: string }) {
  return (
    <div className="grid place-items-center py-16">
      <Spinner label={label} />
    </div>
  )
}

export function ErrorNote({ message }: { message: string }) {
  return (
    <p role="alert" className="flex items-start gap-2 rounded-[8px] bg-danger-soft px-3 py-2 text-[0.95rem] text-danger">
      <DangerTriangleIcon size={18} aria-hidden className="mt-px shrink-0" />
      <span>{message}</span>
    </p>
  )
}

export function Notice({ tone = 'info', children }: { tone?: 'warn' | 'info'; children: ReactNode }) {
  const Icon = tone === 'warn' ? DangerTriangleIcon : InfoCircleIcon
  return (
    <div className={`flex items-start gap-2.5 rounded-[10px] bg-surface px-3.5 py-3 text-[0.95rem] leading-relaxed ${tone === 'warn' ? 'text-warn' : 'text-ink-2'}`}>
      <Icon size={19} aria-hidden className="mt-px shrink-0" />
      <div className="min-w-0">{children}</div>
    </div>
  )
}

const statusText: Record<Status, string> = { activa: 'Activa', por_expirar: 'Por expirar', expirada: 'Expirada' }
const statusDot: Record<Status, string> = { activa: 'bg-accent', por_expirar: 'bg-warn', expirada: 'bg-idle' }

/** Status as a coloured dot and plain text, like macOS service indicators. */
export function StatusBadge({ status }: { status: Status }) {
  return (
    <span className="inline-flex items-center gap-1.5 text-[0.95rem] text-ink">
      <span aria-hidden className={`size-2 rounded-full ${statusDot[status]}`} />
      {statusText[status]}
    </span>
  )
}

export function Tag({ children, tone = 'neutral' }: { children: ReactNode; tone?: 'neutral' | 'accent' | 'warn' | 'danger' }) {
  const tones = { neutral: 'text-ink-2', accent: 'text-accent-text', warn: 'text-warn', danger: 'text-danger' }
  return <span className={`inline-flex items-center rounded-[5px] bg-surface-3 px-1.5 py-px text-[0.82rem] font-medium ${tones[tone]}`}>{children}</span>
}

/** Grouped section: heading outside, borderless filled box inside. */
export function Panel({ title, actions, children, className = '' }: { title?: ReactNode; actions?: ReactNode; children: ReactNode; className?: string }) {
  return (
    <section className={className}>
      {(title || actions) && (
        <header className="flex min-h-7 items-end justify-between gap-3 pb-1.5">
          {title && <h2 className="section-title p-0 px-1">{title}</h2>}
          {actions && <div className="flex items-center gap-1.5">{actions}</div>}
        </header>
      )}
      <div className="group-box">{children}</div>
    </section>
  )
}

export function EmptyState({ title, children, action }: { title: string; children?: ReactNode; action?: ReactNode }) {
  return (
    <div className="mx-auto max-w-md px-6 py-12 text-center">
      <h3 className="text-[1rem] font-semibold">{title}</h3>
      {children && <div className="mt-1 text-[0.95rem] leading-relaxed text-ink-2">{children}</div>}
      {action && <div className="mt-4">{action}</div>}
    </div>
  )
}

/** Row of figures in one grouped box, separated by hairlines (no cards). */
export function Metrics({ items }: { items: { label: string; value: ReactNode; hint?: ReactNode; tone?: string }[] }) {
  return (
    <dl className="group-box grid grid-cols-2 md:grid-cols-4">
      {items.map((m, i) => (
        <div key={m.label} className={`px-4 py-3.5 ${i > 0 ? 'md:shadow-[inset_1px_0_0_var(--hairline)]' : ''} ${i % 2 === 1 ? 'max-md:shadow-[inset_1px_0_0_var(--hairline)]' : ''} ${i > 1 ? 'max-md:shadow-[inset_0_1px_0_var(--hairline)]' : ''}`}>
          <dt className="text-[0.85rem] text-ink-2">{m.label}</dt>
          <dd className={`mt-0.5 font-mono text-[1.45rem] font-semibold tracking-tight tabular-nums ${m.tone ?? ''}`}>{m.value}</dd>
          {m.hint && <dd className="text-[0.85rem] text-ink-3">{m.hint}</dd>}
        </div>
      ))}
    </dl>
  )
}

/** Password row with reveal + copy, used everywhere credentials appear once. */
export function SecretField({ label, value }: { label: string; value: string }) {
  const [shown, setShown] = useState(false)
  return (
    <div className="flex items-center gap-1">
      <span className="min-w-0 flex-1 truncate font-mono text-[0.9rem]">{shown ? value : '•'.repeat(20)}</span>
      <Button variant="ghost" size="sm" className="px-1.5" onClick={() => setShown(!shown)} aria-pressed={shown} aria-label={shown ? `Ocultar ${label}` : `Mostrar ${label}`}>
        {shown ? <EyeClosedIcon size={17} aria-hidden /> : <EyeIcon size={17} aria-hidden />}
      </Button>
      <CopyButton value={value} label={label} compact />
    </div>
  )
}

/** System Settings style rows: label left, value right, inset hairlines. */
export function KeyValue({ rows, stacked = false }: { rows: [string, ReactNode][]; stacked?: boolean }) {
  const layout = stacked ? 'grid-cols-1 gap-0.5' : 'grid-cols-[150px_1fr] items-center gap-3 max-sm:grid-cols-1 max-sm:gap-0.5'
  return (
    <dl className="group-rows">
      {rows.map(([k, v]) => (
        <div key={k} className={`grid min-h-11 px-4 py-2.5 ${layout}`}>
          <dt className="text-[0.95rem] text-ink-2">{k}</dt>
          <dd className="min-w-0 text-[0.95rem]">{v}</dd>
        </div>
      ))}
    </dl>
  )
}

export function CodeBlock({ code, label, id }: { code: string; label: string; id?: string }) {
  return (
    <div className="relative">
      <pre id={id} className="overflow-x-auto rounded-[8px] bg-bg px-3.5 py-3 pr-12 font-mono text-[0.85rem] leading-relaxed text-ink">
        {code}
      </pre>
      <div className="absolute top-1.5 right-1.5">
        <CopyButton value={code} label={label} compact />
      </div>
    </div>
  )
}

/** macOS segmented control. Items render as links or buttons via `render`. */
export function Segmented({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div role="group" aria-label={label} className="inline-flex rounded-[7px] bg-surface-2 p-0.5">
      {children}
    </div>
  )
}

export const segmentClass = (active: boolean) =>
  `inline-flex h-7 items-center gap-1.5 rounded-[5px] px-3.5 text-[0.9rem] whitespace-nowrap transition-colors ${
    active ? 'bg-surface font-semibold text-ink shadow-[0_1px_2px_rgb(0_0_0/0.18)]' : 'text-ink-2 hover:text-ink'
  }`

export function errorMessage(err: unknown): string {
  return err instanceof ApiError ? err.message : 'No se pudo conectar con el servidor.'
}

/** Loads data with loading/error state and a reload function. */
export function useLoad<T>(load: () => Promise<T>, deps: unknown[]) {
  const [data, setData] = useState<T | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const run = useCallback(load, deps)
  const reload = useCallback(() => {
    setLoading(true)
    setError('')
    run()
      .then(setData)
      .catch((e) => setError(errorMessage(e)))
      .finally(() => setLoading(false))
  }, [run])
  useEffect(reload, [reload])
  return { data, error, loading, reload, setData }
}

/** Wraps an async action with busy/error state. */
export function useAction() {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const run = useCallback(async <T,>(fn: () => Promise<T>): Promise<T | undefined> => {
    setBusy(true)
    setError('')
    try {
      return await fn()
    } catch (e) {
      setError(errorMessage(e))
      return undefined
    } finally {
      setBusy(false)
    }
  }, [])
  return { busy, error, run, setError }
}
