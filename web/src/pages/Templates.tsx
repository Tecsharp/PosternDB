import { useState, type FormEvent } from 'react'
import { AddCircleIcon } from '@solar-icons/react/linear'
import { api, can, type Template } from '../api'
import type { PageProps } from '../App'
import { PageBody, PageHeader } from '../components/Shell'
import { Button, CodeBlock, EmptyState, ErrorNote, Loading, Sheet, SheetFooter, SheetHeader, useAction, useLoad } from '../components/ui'
import { formatDate } from '../format'

type Draft = { id?: number; name: string; description: string; sql: string }

export function Templates({ session }: PageProps) {
  const { data, error, loading, reload } = useLoad(api.templates, [])
  const [editing, setEditing] = useState<Draft | null>(null)
  const [removing, setRemoving] = useState<Template | null>(null)
  const action = useAction()
  const operate = can.operate(session.role)

  return (
    <>
      <PageHeader
        title="Plantillas"
        subtitle="SQL que se ejecuta al crear una base, con los permisos de su dueño."
        actions={
          operate && (
            <Button variant="primary" onClick={() => setEditing({ name: '', description: '', sql: '' })}>
              <AddCircleIcon size={16} aria-hidden />
              Nueva plantilla
            </Button>
          )
        }
      />
      <PageBody>
        {loading && !data && <Loading label="Cargando plantillas…" />}
        {(error || action.error) && <ErrorNote message={error || action.error} />}
        {data && data.length === 0 && (
          <div className="group-box">
            <EmptyState title="Sin plantillas">Guarda aquí el esquema base de tus proyectos para crear bases listas para usar.</EmptyState>
          </div>
        )}
        <div className="grid gap-4 lg:grid-cols-2">
          {(data ?? []).map((t) => (
            <article key={t.id} className="group-box flex flex-col">
              <header className="flex items-start justify-between gap-3 px-5 pt-4">
                <div className="min-w-0">
                  <h2 className="text-[15px] font-semibold">{t.name}</h2>
                  <p className="mt-0.5 text-[12.5px] text-ink-2">{t.description || 'Sin descripción.'}</p>
                </div>
                <span className="shrink-0 text-[12px] text-ink-3">{formatDate(t.updatedAt)}</span>
              </header>
              <div className="flex-1 px-5 py-3">
                <CodeBlock code={t.sql.length > 600 ? `${t.sql.slice(0, 600)}\n…` : t.sql} label={`SQL de ${t.name}`} />
              </div>
              {operate && (
                <footer className="flex gap-2 px-5 pt-1 pb-4">
                  <Button size="sm" onClick={() => setEditing({ id: t.id, name: t.name, description: t.description, sql: t.sql })}>
                    Editar
                  </Button>
                  <Button size="sm" variant="ghost" className="hover:text-danger" onClick={() => setRemoving(t)}>
                    Eliminar
                  </Button>
                </footer>
              )}
            </article>
          ))}
        </div>
      </PageBody>

      {editing && (
        <TemplateEditor
          key={editing.id ?? 'new'}
          draft={editing}
          onClose={() => setEditing(null)}
          onSaved={() => {
            setEditing(null)
            reload()
          }}
        />
      )}
      <Sheet open={removing !== null} onClose={() => setRemoving(null)} labelledBy="rm-tpl">
        {removing && (
          <>
            <SheetHeader id="rm-tpl" title={`¿Eliminar "${removing.name}"?`} subtitle="Las bases ya creadas con ella no cambian." />
            <SheetFooter>
              <Button onClick={() => setRemoving(null)}>Cancelar</Button>
              <Button
                variant="danger"
                disabled={action.busy}
                onClick={async () => {
                  await action.run(() => api.deleteTemplate(removing.id))
                  setRemoving(null)
                  reload()
                }}
              >
                Eliminar plantilla
              </Button>
            </SheetFooter>
          </>
        )}
      </Sheet>
    </>
  )
}

function TemplateEditor({ draft, onClose, onSaved }: { draft: Draft; onClose: () => void; onSaved: () => void }) {
  const [f, setForm] = useState<Draft>(draft)
  const { busy, error, run } = useAction()

  async function submit(e: FormEvent) {
    e.preventDefault()
    const saved = await run(() => api.saveTemplate(f))
    if (saved) onSaved()
  }

  return (
    <Sheet open onClose={onClose} labelledBy="tpl-title" wide dismissable={!busy}>
      {(
        <form onSubmit={submit}>
          <SheetHeader id="tpl-title" title={f.id ? 'Editar plantilla' : 'Nueva plantilla'} subtitle="Puede tener varias sentencias. Si falla, la base no se crea." />
          <div className="space-y-4 px-6 py-5">
            <label className="block">
              <span className="label">Nombre</span>
              <input className="field" required maxLength={80} value={f.name} onChange={(e) => setForm({ ...f, name: e.target.value })} />
            </label>
            <label className="block">
              <span className="label">Descripción</span>
              <input className="field" value={f.description} onChange={(e) => setForm({ ...f, description: e.target.value })} />
            </label>
            <label className="block">
              <span className="label">SQL</span>
              <textarea className="field min-h-64 font-mono text-[12.5px]" required spellCheck={false} value={f.sql} onChange={(e) => setForm({ ...f, sql: e.target.value })} />
            </label>
            {error && <ErrorNote message={error} />}
          </div>
          <SheetFooter>
            <Button onClick={onClose} disabled={busy}>
              Cancelar
            </Button>
            <Button type="submit" variant="primary" disabled={busy}>
              {busy ? 'Guardando…' : 'Guardar plantilla'}
            </Button>
          </SheetFooter>
        </form>
      )}
    </Sheet>
  )
}
