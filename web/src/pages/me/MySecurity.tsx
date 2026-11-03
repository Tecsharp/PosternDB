import { useEffect, useState, type FormEvent } from 'react'
import { CheckCircleIcon } from '@solar-icons/react/linear'
import { api, me } from '../../api'
import { ChangePasswordSheet } from '../../components/ChangePasswordSheet'
import { useSession } from '../../components/SessionContext'
import { PageBody, PageHeader } from '../../components/Shell'
import { Button, ButtonLink, ErrorNote, Loading, Notice, Panel, useAction, useLoad } from '../../components/ui'
import { USERNAME_HINT, usernameProblem } from './UsernameSheet'

const googleMessages: Record<string, { ok: boolean; text: string }> = {
  linked: { ok: true, text: 'Listo: tu cuenta quedó vinculada a Google. Desde ahora puedes entrar con un clic.' },
  taken: { ok: false, text: 'Esa cuenta de Google ya está vinculada a otro usuario.' },
  mismatch: { ok: false, text: 'Tu cuenta ya tiene otra cuenta de Google vinculada.' },
  cancelled: { ok: false, text: 'Cancelaste la vinculación con Google.' },
  unverified: { ok: false, text: 'Google no confirmó el correo de esa cuenta.' },
  failed: { ok: false, text: 'No pudimos vincular Google. Inténtalo de nuevo.' },
}

/** Google redirects back to "/seguridad?google=<reason>" after linking. */
function takeGoogleResult(): { ok: boolean; text: string } | null {
  const reason = new URLSearchParams(window.location.search).get('google')
  if (!reason) return null
  window.history.replaceState(null, '', window.location.pathname)
  return googleMessages[reason] ?? googleMessages.failed
}

export function MySecurity() {
  const { refresh } = useSession()
  const account = useLoad(me.account, [])
  const config = useLoad(api.publicConfig, [])
  const [result] = useState(takeGoogleResult)
  const [changingPassword, setChangingPassword] = useState(false)
  const [username, setUsername] = useState('')
  const rename = useAction()
  const a = account.data

  useEffect(() => {
    if (a) setUsername(a.needsUsername ? '' : a.username)
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
  const problem = usernameProblem(username)

  async function saveUsername(e: FormEvent) {
    e.preventDefault()
    if (problem || !username.trim()) return
    const ok = await rename.run(() => me.updateAccount({ username }).then(() => true))
    if (ok) {
      account.reload()
      await refresh()
    }
  }

  return (
    <>
      <PageHeader title="Seguridad" subtitle="Cómo entras a tu cuenta: usuario, contraseña y Google." />
      <PageBody>
        {result && (
          <div role="status">
            {result.ok ? (
              <p className="flex items-center gap-2 rounded-[10px] bg-surface px-3.5 py-3 text-[0.95rem] text-accent-text">
                <CheckCircleIcon size={18} aria-hidden />
                {result.text}
              </p>
            ) : (
              <ErrorNote message={result.text} />
            )}
          </div>
        )}

        <Panel title="Nombre de usuario">
          <form onSubmit={saveUsername} className="space-y-3 px-5 py-4">
            <p className="text-[0.95rem] text-ink-2">
              Puedes entrar con tu nombre de usuario o con tu correo <span className="font-mono text-ink">{a.email}</span>, que siempre se conserva.
            </p>
            <div className="flex flex-wrap items-end gap-3">
              <label className="block">
                <span className="label">Nombre de usuario</span>
                <input
                  className="field w-72 font-mono"
                  maxLength={32}
                  spellCheck={false}
                  autoCapitalize="none"
                  placeholder="ana.lopez"
                  value={username}
                  aria-invalid={problem ? 'true' : 'false'}
                  onChange={(e) => setUsername(e.target.value)}
                />
              </label>
              <Button type="submit" variant="primary" disabled={rename.busy || !username.trim() || !!problem || username.trim().toLowerCase() === a.username}>
                {rename.busy ? 'Guardando…' : 'Guardar'}
              </Button>
            </div>
            <p className={`text-[0.85rem] ${problem ? 'text-danger' : 'text-ink-3'}`}>{USERNAME_HINT}</p>
            {rename.error && <ErrorNote message={rename.error} />}
          </form>
        </Panel>

        <Panel title="Contraseña">
          <div className="flex flex-wrap items-center justify-between gap-3 px-5 py-4">
            <p className="text-[0.95rem] text-ink-2">
              {a.hasPassword ? 'Tu cuenta tiene una contraseña.' : 'Tu cuenta entra solo con Google. Puedes crear también una contraseña.'}
            </p>
            <Button onClick={() => setChangingPassword(true)}>{a.hasPassword ? 'Cambiar contraseña' : 'Crear contraseña'}</Button>
          </div>
        </Panel>

        {config.data?.googleEnabled && (
          <Panel title="Google">
            <div className="flex flex-wrap items-center justify-between gap-3 px-5 py-4">
              {a.googleLinked ? (
                <p className="flex items-center gap-2 text-[0.95rem]">
                  <CheckCircleIcon size={18} className="text-accent-text" aria-hidden />
                  Tu cuenta está vinculada a Google. Puedes entrar con un clic desde la pantalla de acceso.
                </p>
              ) : (
                <>
                  <p className="max-w-xl text-[0.95rem] text-ink-2">
                    Vincula tu cuenta de Google para entrar con un clic y recuperar tu acceso si olvidas la contraseña. Solo tú puedes hacerlo, desde aquí.
                  </p>
                  <ButtonLink variant="primary" href={me.googleLinkUrl}>
                    Vincular cuenta de Google
                  </ButtonLink>
                </>
              )}
            </div>
          </Panel>
        )}
        {config.data && !config.data.googleEnabled && <Notice tone="info">El acceso con Google no está activado en este servicio.</Notice>}
      </PageBody>

      <ChangePasswordSheet
        open={changingPassword}
        onClose={() => setChangingPassword(false)}
        changePassword={me.changePassword}
        needsCurrent={a.hasPassword}
        onChanged={() => {
          account.reload()
          void refresh()
        }}
      />
    </>
  )
}
