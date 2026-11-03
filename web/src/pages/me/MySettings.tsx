import { useEffect, useState, type FormEvent } from 'react'
import { me } from '../../api'
import { useSession } from '../../components/SessionContext'
import { PageBody, PageHeader } from '../../components/Shell'
import { Button, ButtonLink, ErrorNote, KeyValue, Loading, Panel, useAction, useLoad } from '../../components/ui'
import { formatDate } from '../../format'

export function MySettings() {
  const { refresh } = useSession()
  const account = useLoad(me.account, [])
  const [fullName, setFullName] = useState('')
  const [saved, setSaved] = useState(false)
  const save = useAction()
  const a = account.data

  useEffect(() => {
    if (a) setFullName(a.fullName)
  }, [a])

  if (account.loading && !a) return <Loading label="Cargando tu cuenta…" />
  if (account.error || !a) {
    return (
      <PageBody>
        <div className="pt-8">
          <ErrorNote message={account.error || 'No disponible.'} />
        </div>
      </PageBody>
    )
  }

  async function submit(e: FormEvent) {
    e.preventDefault()
    setSaved(false)
    const ok = await save.run(() => me.updateAccount({ fullName }).then(() => true))
    if (ok) {
      setSaved(true)
      account.reload()
      await refresh()
    }
  }

  const ways = [a.email && 'tu correo', a.username !== a.email && 'tu nombre de usuario', a.hasPassword && 'tu contraseña', a.googleLinked && 'Google'].filter(Boolean)

  return (
    <>
      <PageHeader title="Ajustes" subtitle="Tu perfil y los límites de tu cuenta." />
      <PageBody>
        <Panel title="Perfil">
          <form onSubmit={submit} className="space-y-4 px-5 py-4">
            <label className="block">
              <span className="label">Nombre completo</span>
              <input className="field max-w-md" required minLength={2} maxLength={80} value={fullName} onChange={(e) => setFullName(e.target.value)} />
            </label>
            <div>
              <span className="label">Correo</span>
              <p className="font-mono text-[0.95rem]">{a.email}</p>
              <p className="mt-1 text-[0.85rem] text-ink-3">El correo es parte de tu cuenta y no se puede quitar.</p>
            </div>
            {save.error && <ErrorNote message={save.error} />}
            <div className="flex items-center gap-3">
              <Button type="submit" variant="primary" disabled={save.busy || fullName.trim() === a.fullName}>
                {save.busy ? 'Guardando…' : 'Guardar'}
              </Button>
              {saved && (
                <span role="status" className="text-[0.9rem] text-accent-text">
                  Guardado.
                </span>
              )}
            </div>
          </form>
        </Panel>

        <Panel title="Tu cuenta">
          <KeyValue
            rows={[
              ['Bases de datos', `${a.databaseCount} de ${a.maxDatabases}`],
              ['Tu código', <span className="font-mono">{a.code}</span>],
              ['Cuenta creada', formatDate(a.createdAt)],
              ['Entras con', ways.join(', ')],
            ]}
          />
          <p className="px-5 pb-4 text-[0.85rem] text-ink-2">Tu código va al inicio del nombre de tus bases de datos (<span className="font-mono">{a.code}-tienda</span>), así nadie más puede usar el mismo nombre. Cada base expira si nadie se conecta durante un tiempo; lo ves en su resumen.</p>
        </Panel>

        <Panel title="Ayuda">
          <div className="flex flex-wrap items-center justify-between gap-3 px-5 py-4">
            <p className="text-[0.95rem] text-ink-2">Aprende a instalar cloudflared y abrir el puente para conectar tu aplicación.</p>
            <ButtonLink href="/ayuda" target="_blank" rel="noopener noreferrer">
              Ver tutorial
            </ButtonLink>
          </div>
        </Panel>
      </PageBody>
    </>
  )
}
