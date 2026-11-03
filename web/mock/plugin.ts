// Development-only mock of the PosternDB API so the panel can be designed
// and reviewed without Postgres or Cloudflare. Enabled by `npm run dev:mock`
// (login admin / admin). Never part of the production build.
import type { IncomingMessage, ServerResponse } from 'node:http'
import type { Plugin } from 'vite'

const DAY = 86_400_000
const now = Date.now()
const iso = (offsetDays: number) => new Date(now + offsetDays * DAY).toISOString()

const settings = {
  baseDomain: 'posterndb.com',
  panelHostname: 'app.posterndb.com',
  clientHostPrefix: 'postern-',
  exampleHostname: 'postern-cliente1.posterndb.com',
  expirationDays: 30,
  expiringSoonDays: 7,
  checkIntervalHours: 6,
  backupBeforeDelete: true,
  localMode: false,
  localDbPort: 25432,
  clientLocalPort: 5432,
  notifyChannels: ['webhook'],
  allowSelfRegister: true,
  maxDatabasesPerClient: 3,
}

type Row = Record<string, unknown> & { slug: string; status: string; id: number }
let nextId = 10
const mk = (id: number, slug: string, created: number, seen: number, status: string, extra = {}): Row => ({
  id,
  slug,
  database: `pdb_${slug.replaceAll('-', '_')}`,
  username: `pdb_${slug.replaceAll('-', '_')}`,
  hostname: `postern-${slug}.posterndb.com`,
  state: status === 'expirada' ? 'expired' : 'active',
  createdAt: iso(created),
  lastSeenAt: iso(seen),
  expiresAt: iso(seen + 30),
  neverExpires: false,
  status,
  ...extra,
})

let databases: Row[] = [
  mk(5, 'taller-motos', -2, -0.1, 'activa'),
  mk(4, 'catinv', -40, -0.3, 'activa', { neverExpires: true, expiresAt: undefined }),
  mk(3, 'triburide', -70, -1.5, 'activa', { maxIdleDays: 90, expiresAt: iso(88) }),
  mk(2, 'lubexy-staging', -60, -26, 'por_expirar'),
  mk(1, 'demo-feria', -90, -45, 'expirada', { expiredAt: iso(-15), archivedAs: 'pdbarch_demo_feria_20261018' }),
  mk(6, 'ana-tienda', -5, -0.2, 'activa', { clientUserId: 1 }),
]

const col = (name: string, type: string, extra: Record<string, unknown> = {}) => ({ name, type, nullable: true, primaryKey: false, ...extra })
const pk = (name = 'id', type = 'bigint') => col(name, type, { primaryKey: true, nullable: false, default: "nextval('seq'::regclass)" })
const fk = (name: string, columns: string[], refTable: string) => ({ name, columns, refSchema: 'public', refTable, refColumns: ['id'] })
const table = (name: string, rows: number, columns: unknown[], foreignKeys: unknown[] = [], kind = 'table') => ({
  schema: 'public',
  name,
  kind,
  rows,
  sizeBytes: rows * 180 + 16384,
  columns,
  foreignKeys,
  indexes: [{ name: `${name}_pkey`, definition: `CREATE UNIQUE INDEX ${name}_pkey ON public.${name} USING btree (id)`, unique: true }],
})

const schema = {
  schemas: ['public'],
  tables: [
    table('clubs', 12, [pk(), col('nombre', 'text', { nullable: false }), col('ciudad', 'text'), col('creado_en', 'timestamp with time zone', { nullable: false })]),
    table('miembros', 348, [pk(), col('club_id', 'bigint', { nullable: false }), col('nombre', 'text', { nullable: false }), col('email', 'text'), col('rol', 'text')], [fk('miembros_club_fk', ['club_id'], 'clubs')]),
    table('motos', 402, [pk(), col('miembro_id', 'bigint', { nullable: false }), col('marca', 'text'), col('modelo', 'text'), col('cc', 'integer')], [fk('motos_miembro_fk', ['miembro_id'], 'miembros')]),
    table('eventos', 57, [pk(), col('club_id', 'bigint', { nullable: false }), col('titulo', 'text', { nullable: false }), col('fecha', 'date'), col('ruta_km', 'numeric(6,1)')], [fk('eventos_club_fk', ['club_id'], 'clubs')]),
    table('asistencias', 1890, [pk(), col('evento_id', 'bigint', { nullable: false }), col('miembro_id', 'bigint', { nullable: false }), col('confirmada', 'boolean')], [fk('asist_evento_fk', ['evento_id'], 'eventos'), fk('asist_miembro_fk', ['miembro_id'], 'miembros')]),
    table('galeria', 2210, [pk(), col('evento_id', 'bigint'), col('url', 'text', { nullable: false }), col('subida_en', 'timestamp with time zone')], [fk('galeria_evento_fk', ['evento_id'], 'eventos')]),
    table('proximos_eventos', 0, [col('id', 'bigint'), col('titulo', 'text'), col('fecha', 'date')], [], 'view'),
  ],
}

const liveConnections = [
  { pid: 4121, user: 'pdb_taller', application: 'HikariPool-1', state: 'idle', connectedAt: iso(-0.3) },
  { pid: 4188, user: 'pdb_taller', application: 'psql', state: 'active', connectedAt: iso(-0.01), query: 'SELECT count(*) FROM miembros WHERE activo' },
  { pid: 4203, user: 'pdb_taller_ro', application: 'DBeaver 24.1', state: 'idle in transaction', connectedAt: iso(-0.05), query: 'UPDATE miembros SET activo = true WHERE id = 3' },
]
const stats = {
  sizeBytes: 9_420_800,
  openConnections: 4,
  totalSessions: 1832,
  commits: 92_114,
  rollbacks: 12,
  tableCount: 6,
  totalRows: 4919,
  topTables: schema.tables.filter((t) => t.kind === 'table').map((t) => ({ schema: t.schema, name: t.name, rows: t.rows, sizeBytes: t.sizeBytes })).sort((a, b) => b.sizeBytes - a.sizeBytes),
}

const users = [{ id: 1, name: 'bi', username: 'pdb_taller_motos__bi', access: 'read', createdAt: iso(-1) }]
let jobs = [
  { id: 3, kind: 'dump', status: 'done', slug: 'taller-motos', sourceDb: 'pdb_taller_motos', file: 'pdb_taller_motos_20261101120000.dump', bytes: 1_204_224, reason: 'manual', requestedBy: 'admin', createdAt: iso(-1) },
  { id: 2, kind: 'archive', status: 'done', slug: 'demo-feria', file: 'pdbarch_demo_feria_20261018.dump', bytes: 88_064, reason: 'expiry', requestedBy: 'inactivity-auditor', createdAt: iso(-15) },
  { id: 1, kind: 'clone', status: 'failed', slug: 'taller-motos', error: 'pg_restore: error: could not connect', requestedBy: 'admin', createdAt: iso(-2) },
]
let templates = [
  { id: 1, name: 'Contactos (demo)', description: 'Tabla de contactos con datos de ejemplo.', sql: 'CREATE TABLE contactos (\n  id BIGSERIAL PRIMARY KEY,\n  nombre TEXT NOT NULL\n);', shared: true, updatedAt: iso(-3) },
  { id: 2, name: 'Tareas (to-do)', description: 'Lista de tareas con estado y fecha límite.', sql: 'CREATE TABLE tareas (\n  id BIGSERIAL PRIMARY KEY,\n  titulo TEXT NOT NULL,\n  completada BOOLEAN NOT NULL DEFAULT false\n);', shared: true, updatedAt: iso(-3) },
  { id: 3, name: 'Tienda en línea', description: 'Productos, clientes y pedidos con sus líneas.', sql: 'CREATE TABLE productos (...);\nCREATE TABLE clientes (...);\nCREATE TABLE pedidos (...);', shared: true, updatedAt: iso(-3) },
  { id: 4, name: 'Esquema interno', description: 'Solo para administradores.', sql: 'CREATE TABLE interna (id int);', shared: false, updatedAt: iso(-3) },
]
let admins = [
  { id: 0, username: 'admin', role: 'owner', createdAt: iso(-30), createdBy: '.env', bootstrap: true },
  { id: 1, username: 'ana', role: 'admin', createdAt: iso(-10), createdBy: 'admin', lastLoginAt: iso(-0.2), bootstrap: false },
  { id: 2, username: 'bi-lector', role: 'viewer', createdAt: iso(-4), createdBy: 'admin', bootstrap: false },
]
const audit = [
  { id: 7, occurredAt: iso(-0.01), actor: 'admin', action: 'query_executed', target: 'taller-motos', details: { sql: 'SELECT count(*) FROM miembros' } },
  { id: 6, occurredAt: iso(-0.05), actor: 'inactivity-auditor', action: 'inactivity_audit_run', target: '', details: { checked: 4, activeSeen: 3, expired: 0 } },
  { id: 5, occurredAt: iso(-0.06), actor: 'inactivity-auditor', action: 'activity_detected', target: 'catinv', details: { openConnections: 4, newSessions: 12 } },
  { id: 4, occurredAt: iso(-2), actor: 'admin', action: 'db_created', target: 'taller-motos', details: { hostname: 'postern-taller-motos.posterndb.com' } },
  { id: 3, occurredAt: iso(-2.1), actor: 'system', action: 'login_failed', target: '', details: { ip: '189.203.10.4' } },
  { id: 2, occurredAt: iso(-15), actor: 'inactivity-auditor', action: 'db_expired', target: 'demo-feria', details: {} },
  { id: 1, occurredAt: iso(-15), actor: 'backup-sidecar', action: 'backup_completed', target: 'demo-feria', details: { file: '/backups/pdbarch_demo_feria_20261018.dump' } },
]

const dataRows: Record<string, (string | null)[][]> = {}
let globalExpiry: { expirationDays: number | null; expiringSoonDays: number | null; updatedAt: string; updatedBy?: string } = {
  expirationDays: null, expiringSoonDays: null, updatedAt: new Date().toISOString(),
}
type ClientRow = {
  id: number
  username: string
  email?: string
  fullName?: string
  googleLinked: boolean
  hasPassword: boolean
  needsUsername?: boolean
  code: string
  neverExpires: boolean
  maxIdleDays?: number
  createdAt: string
  createdBy: string
  updatedAt: string
  databaseCount: number
  lastLoginAt?: string
}
let clients: ClientRow[] = [{ id: 1, username: 'ana@ejemplo.com', email: 'ana@ejemplo.com', fullName: 'Ana López', code: 'k7x9qa', googleLinked: false, hasPassword: true, neverExpires: false, createdAt: iso(-20), createdBy: '', updatedAt: iso(-20), databaseCount: 1, lastLoginAt: iso(-0.5) }]
let nextClientId = 2
const newCode = () => {
  const letters = 'abcdefghijklmnopqrstuvwxyz'
  const all = letters + '0123456789'
  return letters[Math.floor(Math.random() * 26)] + Array.from({ length: 5 }, () => all[Math.floor(Math.random() * 36)]).join('')
}

let myBackups = [
  { id: 101, kind: 'dump', status: 'done', slug: 'ana-tienda', file: 'pdb_ana_tienda_20261101.dump', bytes: 48_128, reason: 'manual', requestedBy: 'ana@ejemplo.com', createdAt: iso(-1) },
  { id: 102, kind: 'dump', status: 'done', slug: 'ana-tienda', file: 'pdb_ana_tienda_20261020.dump', bytes: 41_984, reason: 'manual', requestedBy: 'ana@ejemplo.com', createdAt: iso(-13) },
]
const myActivity = [
  { id: 9, occurredAt: iso(-0.02), actor: 'ana@ejemplo.com', action: 'backup_requested', target: 'ana-tienda', details: { file: 'pdb_ana_tienda_20261101.dump' } },
  { id: 8, occurredAt: iso(-0.5), actor: 'ana@ejemplo.com', action: 'login_success', target: '', details: { ip: '189.203.10.4' } },
  { id: 7, occurredAt: iso(-3), actor: 'administrador', action: 'client_password_reset', target: 'ana@ejemplo.com' },
  { id: 6, occurredAt: iso(-5), actor: 'ana@ejemplo.com', action: 'db_created', target: 'ana-tienda', details: { hostname: 'postern-ana-tienda.posterndb.com' } },
  { id: 5, occurredAt: iso(-20), actor: 'ana@ejemplo.com', action: 'client_registered', target: 'ana@ejemplo.com', details: { method: 'password' } },
]
let loggedIn = false
let currentRole: 'owner' | 'client' = 'owner'
let currentClientId = 0
const creds = (row: Row, username = row.username as string) => ({
  hostname: row.hostname, database: row.database, username, host: 'localhost', port: 5432, localMode: false,
  password: 'q7Rk2mZp9XwL4vNc8BsT3hYd6FjG1aEu',
})

function send(res: ServerResponse, status: number, body?: unknown) {
  res.statusCode = status
  if (body === undefined) return res.end()
  res.setHeader('Content-Type', 'application/json')
  res.end(JSON.stringify(body))
}

async function readJSON(req: IncomingMessage): Promise<Record<string, unknown>> {
  let raw = ''
  for await (const chunk of req) raw += chunk
  return raw ? JSON.parse(raw) : {}
}

export function mockApi(): Plugin {
  return {
    name: 'posterndb-mock-api',
    configureServer(server) {
      server.middlewares.use(async (req, res, next) => {
        const url = (req.url ?? '').split('?')[0]
        if (!url.startsWith('/api/')) return next()
        const path = url.slice(4)
        const m = req.method ?? 'GET'
        const parts = path.split('/').filter(Boolean).map(decodeURIComponent)

        if (path === '/auth/login') {
          const { username, password } = await readJSON(req)
          if (username === 'admin' && password === 'admin') {
            loggedIn = true
            currentRole = 'owner'
            return send(res, 200, { user: 'admin', role: 'owner' })
          }
          const client = clients.find((c) => c.username === username || c.email === username)
          if (client && client.hasPassword && password === 'ana-password-123') {
            loggedIn = true
            currentRole = 'client'
            currentClientId = client.id
            return send(res, 200, { user: client.username, role: 'client' })
          }
          return send(res, 401, { error: 'usuario o contraseña incorrectos' })
        }
        if (path === '/auth/config') return send(res, 200, { allowSelfRegister: settings.allowSelfRegister, googleEnabled: true, passwordEnabled: true })
        if (path === '/auth/google/start') {
          // Stand-in for Google: signs in a Google-only account and goes back to the panel.
          let g = clients.find((c) => c.email === 'laura@gmail.com')
          if (!g) {
            g = { id: nextClientId++, username: 'laura@gmail.com', email: 'laura@gmail.com', fullName: 'Laura Gómez', code: newCode(), googleLinked: true, hasPassword: false, needsUsername: true, neverExpires: false, createdAt: new Date().toISOString(), createdBy: '', updatedAt: new Date().toISOString(), databaseCount: 0 }
            clients = [...clients, g]
          }
          loggedIn = true
          currentRole = 'client'
          currentClientId = g.id
          res.statusCode = 302
          res.setHeader('Location', '/')
          return res.end()
        }
        if (path === '/auth/register') {
          const { email, fullName, password, acceptEmailUse } = (await readJSON(req)) as { email: string; fullName: string; password: string; acceptEmailUse: boolean }
          if (!settings.allowSelfRegister) return send(res, 400, { error: 'el registro de usuarios está desactivado' })
          if (!/^[^@\s]+@[^@\s]+\.[^@\s]+$/.test(String(email))) return send(res, 400, { error: 'escribe un correo válido' })
          if (String(fullName).trim().length < 2) return send(res, 400, { error: 'escribe tu nombre completo (de 2 a 80 caracteres)' })
          if (!acceptEmailUse) return send(res, 400, { error: 'debes aceptar cómo usaremos tu correo para crear la cuenta' })
          if (password.length < 12) return send(res, 400, { error: 'la contraseña debe tener al menos 12 caracteres' })
          const mail = String(email).trim().toLowerCase()
          if (clients.some((c) => c.email === mail)) return send(res, 409, { error: 'ya existe una cuenta con ese correo: entra con tu contraseña o con Google' })
          const c: ClientRow = { id: nextClientId++, username: mail, email: mail, fullName: String(fullName).trim(), code: newCode(), googleLinked: false, hasPassword: true, neverExpires: false, createdAt: new Date().toISOString(), createdBy: '', updatedAt: new Date().toISOString(), databaseCount: 0 }
          clients = [...clients, c]
          loggedIn = true
          currentRole = 'client'
          currentClientId = c.id
          return send(res, 201, { user: c.username, role: 'client' })
        }
        if (path === '/auth/logout') {
          loggedIn = false
          return send(res, 204)
        }
        if (!loggedIn) return send(res, 401, { error: 'Unauthorized' })
        if (path === '/auth/session') {
          if (currentRole === 'client') {
            const c = clients.find((x) => x.id === currentClientId)
            return send(res, 200, { user: c?.username ?? 'cliente', role: 'client', bootstrap: false, email: c?.email, fullName: c?.fullName, hasPassword: c?.hasPassword ?? true, needsUsername: c?.needsUsername ?? false, googleLinked: c?.googleLinked ?? false })
          }
          return send(res, 200, { user: 'admin', role: 'owner', bootstrap: true })
        }
        if (path === '/me/password' && m === 'POST') {
          const c = clients.find((x) => x.id === currentClientId)
          if (c) c.hasPassword = true
          return send(res, 204)
        }
        if (path === '/settings') return send(res, 200, { ...settings, expirationDays: globalExpiry.expirationDays ?? settings.expirationDays, expiringSoonDays: globalExpiry.expiringSoonDays ?? settings.expiringSoonDays })
        if (path === '/settings/expiry' && m === 'GET') return send(res, 200, globalExpiry)
        if (path === '/settings/expiry' && m === 'PUT') {
          const { expirationDays, expiringSoonDays } = (await readJSON(req)) as { expirationDays: number | null; expiringSoonDays: number | null }
          globalExpiry = { expirationDays, expiringSoonDays, updatedAt: new Date().toISOString(), updatedBy: 'admin' }
          return send(res, 200, globalExpiry)
        }
        if (path === '/audit') return send(res, 200, { entries: audit })
        if (path === '/notifications/test') return send(res, 204)
        if (path === '/backups' && m === 'GET') return send(res, 200, { backups: jobs.filter((j) => j.status === 'done') })
        if (parts[0] === 'backups' && parts[2] === 'restore') {
          const { name } = await readJSON(req)
          const row = mk(nextId++, String(name), 0, 0, 'activa')
          databases = [row, ...databases]
          return send(res, 201, creds(row))
        }
        if (parts[0] === 'backups' && m === 'DELETE') {
          jobs = jobs.filter((j) => j.id !== Number(parts[1]))
          return send(res, 204)
        }
        if (path === '/templates' && m === 'GET') return send(res, 200, { templates })
        if (parts[0] === 'templates') {
          if (m === 'DELETE') {
            templates = templates.filter((t) => t.id !== Number(parts[1]))
            return send(res, 204)
          }
          const body = (await readJSON(req)) as { name: string; description: string; sql: string; shared: boolean }
          const t = { id: parts[1] ? Number(parts[1]) : nextId++, ...body, updatedAt: new Date().toISOString() }
          templates = [...templates.filter((x) => x.id !== t.id), t]
          return send(res, parts[1] ? 200 : 201, t)
        }
        if (path === '/clients' && m === 'GET') return send(res, 200, { clients })
        if (path === '/clients' && m === 'POST') {
          const { email, fullName } = (await readJSON(req)) as { email: string; fullName: string }
          const c: ClientRow = { id: nextClientId++, username: String(email).toLowerCase(), email: String(email).toLowerCase(), fullName: String(fullName), code: newCode(), googleLinked: false, hasPassword: true, neverExpires: false, createdAt: new Date().toISOString(), createdBy: 'admin', updatedAt: new Date().toISOString(), databaseCount: 0 }
          clients = [...clients, c]
          return send(res, 201, { client: c, password: 'Zx81PqLm0vRt5Kd2Wn7Y' })
        }
        if (parts[0] === 'clients' && parts[1]) {
          const id = Number(parts[1])
          if (parts[2] === 'expiry' && m === 'PUT') {
            const { neverExpires, maxIdleDays } = (await readJSON(req)) as { neverExpires: boolean; maxIdleDays: number | null }
            clients = clients.map((c) => (c.id === id ? { ...c, neverExpires, maxIdleDays: maxIdleDays ?? undefined, updatedAt: new Date().toISOString() } : c))
            return send(res, 200, clients.find((c) => c.id === id))
          }
          if (parts[2] === 'password' && m === 'POST') {
            if (!clients.some((c) => c.id === id)) return send(res, 404, { error: 'no encontrado' })
            clients = clients.map((c) => (c.id === id ? { ...c, hasPassword: true } : c))
            return send(res, 200, { password: 'Qm7tVx92LrBn4Zc8Ysp3' })
          }
          if (m === 'DELETE') {
            if (!clients.some((c) => c.id === id)) return send(res, 404, { error: 'no encontrado' })
            clients = clients.filter((c) => c.id !== id)
            return send(res, 204)
          }
        }
        if (path === '/me/templates') return send(res, 200, { templates: templates.filter((t) => t.shared) })
        if (path === '/me/account') {
          const c = clients.find((x) => x.id === currentClientId)
          if (!c) return send(res, 404, { error: 'no encontrado' })
          if (m === 'PATCH') {
            const { username, fullName } = (await readJSON(req)) as { username?: string; fullName?: string }
            if (username !== undefined) {
              const name = String(username).trim().toLowerCase()
              if (!/^[a-z][a-z0-9._-]{2,31}$/.test(name)) return send(res, 400, { error: 'el usuario debe empezar con una letra y tener de 3 a 32 caracteres: a-z, 0-9, punto, guion o guion bajo' })
              if (name === 'admin' || clients.some((x) => x.id !== c.id && x.username === name)) return send(res, 409, { error: 'ese nombre de usuario ya está en uso' })
              c.username = name
              c.needsUsername = false
            }
            if (fullName !== undefined) {
              if (String(fullName).trim().length < 2) return send(res, 400, { error: 'escribe tu nombre completo (de 2 a 80 caracteres)' })
              c.fullName = String(fullName).trim()
            }
          }
          const count = databases.filter((d) => d.clientUserId === c.id).length
          return send(res, 200, { username: c.username, email: c.email ?? '', fullName: c.fullName ?? '', googleLinked: c.googleLinked, hasPassword: c.hasPassword, needsUsername: c.needsUsername ?? false, createdAt: c.createdAt, maxDatabases: settings.maxDatabasesPerClient, databaseCount: count, code: c.code, hostPrefix: settings.clientHostPrefix, baseDomain: settings.baseDomain, localMode: false })
        }
        if (path === '/me/google/link') {
          const c = clients.find((x) => x.id === currentClientId)
          if (c) c.googleLinked = true
          res.statusCode = 302
          res.setHeader('Location', '/seguridad?google=linked')
          return res.end()
        }
        if (path === '/me/activity') return send(res, 200, { entries: myActivity })
        if (path === '/me/backups' && m === 'GET') return send(res, 200, { backups: myBackups })
        if (parts[0] === 'me' && parts[1] === 'backups' && parts[2]) {
          const id = Number(parts[2])
          const job = myBackups.find((j) => j.id === id)
          if (!job) return send(res, 404, { error: 'no encontrado' })
          if (parts[3] === 'download') {
            res.setHeader('Content-Type', 'application/octet-stream')
            res.setHeader('Content-Disposition', `attachment; filename="${job.file}"`)
            return res.end('mock backup')
          }
          if (parts[3] === 'restore') {
            const { name } = await readJSON(req)
            const owner = clients.find((x) => x.id === currentClientId)
            const row = mk(nextId++, `${owner?.code}-${String(name).trim().toLowerCase()}`, 0, 0, 'activa', { clientUserId: currentClientId })
            databases = [row, ...databases]
            return send(res, 201, creds(row))
          }
          if (m === 'DELETE') {
            myBackups = myBackups.filter((j) => j.id !== id)
            return send(res, 204)
          }
        }
        if (parts[0] === 'me' && parts[1] === 'databases') {
          const mine = () => databases.filter((d) => d.clientUserId === currentClientId)
          if (!parts[2] && m === 'GET') return send(res, 200, { databases: mine() })
          if (!parts[2] && m === 'POST') {
            const { name } = await readJSON(req)
            const owner = clients.find((x) => x.id === currentClientId)
            const typed = String(name).trim().toLowerCase()
            const room = 40 - (owner?.code.length ?? 6) - 1
            if (typed.length > room) return send(res, 400, { error: `el nombre puede tener hasta ${room} caracteres (tu código ${owner?.code} ocupa ${(owner?.code.length ?? 6) + 1})` })
            if (!/^[a-z][a-z0-9]*(-[a-z0-9]+)*$/.test(typed) || typed.length < 3) return send(res, 400, { error: 'el nombre debe tener entre 3 y 40 caracteres, empezar con una letra y usar solo a-z, 0-9 y guiones sueltos' })
            const slug = `${owner?.code}-${typed}`
            if (databases.some((d) => d.slug === slug)) return send(res, 409, { error: 'ya existe un elemento con ese nombre' })
            const row = mk(nextId++, slug, 0, 0, 'activa', { clientUserId: currentClientId })
            databases = [row, ...databases]
            return send(res, 201, creds(row))
          }
          const row = mine().find((d) => d.slug === parts[2])
          if (!row) return send(res, 404, { error: 'no encontrado' })
          const sub = parts[3]
          if (!sub && m === 'GET') return send(res, 200, row)
          if (!sub && m === 'DELETE') {
            databases = databases.filter((d) => d !== row)
            return send(res, 204)
          }
          if (sub === 'connection') return send(res, 200, { ...creds(row), password: undefined })
          if (sub === 'password') return send(res, 200, creds(row))
          if (sub === 'backups' && m === 'POST') {
            if (myBackups.filter((j) => j.slug === row.slug).length >= 5) return send(res, 403, { error: 'ya tienes el máximo de respaldos de esta base: elimina uno para crear otro' })
            const job = { id: nextId++, kind: 'dump', status: 'pending', slug: row.slug, file: `pdb_${String(row.slug).replace(/-/g, '_')}_${Date.now()}.dump`, bytes: 52_224, reason: 'manual', requestedBy: 'ana@ejemplo.com', createdAt: new Date().toISOString() }
            // The real sidecar finishes a few seconds later.
            setTimeout(() => myBackups.unshift({ ...job, status: 'done' }), 3000)
            return send(res, 202, job)
          }
          if (sub === 'live-connections') return send(res, 200, { connections: liveConnections })
          if (sub === 'stats') return send(res, 200, stats)
          if (sub === 'schema') return send(res, 200, row.slug === 'taller-motos' || row.slug === 'ana-tienda' ? schema : { schemas: [], tables: [] })
          if (sub === 'tables' && parts[6] === 'data') {
            const t = schema.tables.find((x) => x.name === parts[5])
            if (!t) return send(res, 400, { error: 'la tabla no existe' })
            const cols = (t.columns as { name: string; type: string; nullable: boolean; primaryKey: boolean; default?: string }[]).map((c) => ({
              name: c.name, type: c.type, nullable: c.nullable, primaryKey: c.primaryKey, hasDefault: Boolean(c.default), generated: false,
            }))
            const rows = (dataRows[`me:${t.name}`] ??= Array.from({ length: Math.min(t.rows, 120) }, (_, i) =>
              cols.map((c) => (c.primaryKey ? String(i + 1) : c.name.endsWith('_id') ? String((i % 12) + 1) : c.type === 'text' ? (i % 7 === 3 ? null : `${c.name} ${i + 1}`) : c.type.startsWith('timestamp') ? '2026-10-0' + ((i % 9) + 1) + ' 10:00:00+00' : String(i * 3))),
            ))
            if (m === 'GET') {
              const q = new URL(req.url ?? '', 'http://x').searchParams
              const page = Number(q.get('page') ?? 1)
              return send(res, 200, { columns: cols, rows: rows.slice((page - 1) * 50, page * 50), total: rows.length, page, pageSize: 50, editable: t.kind === 'table' })
            }
            const body = (await readJSON(req)) as { key?: Record<string, string>; values?: Record<string, string | null> }
            const idx = rows.findIndex((r) => body.key && r[0] === body.key.id)
            if (m === 'POST') rows.push(cols.map((c, i) => (c.primaryKey ? String(rows.length + 1) : body.values?.[c.name] ?? (i ? null : ''))))
            if (m === 'PATCH' && idx >= 0) cols.forEach((c, i) => { if (body.values && c.name in body.values) rows[idx][i] = body.values[c.name] })
            if (m === 'DELETE' && idx >= 0) rows.splice(idx, 1)
            return send(res, 204)
          }
          if (sub === 'tables' && parts[6] === 'count') return send(res, 200, { rows: 349 })
        }
        if (path === '/admins' && m === 'GET') return send(res, 200, { admins })
        if (path === '/admins' && m === 'POST') {
          const { username, role } = await readJSON(req)
          const a = { id: nextId++, username: String(username), role: String(role), createdAt: new Date().toISOString(), createdBy: 'admin', bootstrap: false }
          admins = [...admins, a]
          return send(res, 201, { admin: a, password: 'Zx81PqLm0vRt5Kd2Wn7Y' })
        }
        if (parts[0] === 'admins') {
          if (parts[2] === 'password') return send(res, 200, { password: 'Nw4Hq8Tz2Lp6Vb1Xc9Rm' })
          if (m === 'DELETE') admins = admins.filter((a) => a.username !== parts[1])
          if (m === 'PATCH') {
            const { role } = await readJSON(req)
            admins = admins.map((a) => (a.username === parts[1] ? { ...a, role: String(role) } : a))
          }
          return send(res, 204)
        }
        if (path === '/databases' && m === 'GET') return send(res, 200, { databases })
        if (path === '/databases' && m === 'POST') {
          const { name } = await readJSON(req)
          if (databases.some((d) => d.slug === name)) return send(res, 409, { error: 'ya existe un elemento con ese nombre' })
          const row = mk(nextId++, String(name), 0, 0, 'activa')
          databases = [row, ...databases]
          return send(res, 201, creds(row))
        }
        if (parts[0] === 'databases' && parts[1]) {
          const row = databases.find((d) => d.slug === parts[1])
          if (!row) return send(res, 404, { error: 'no encontrado' })
          const sub = parts[2]
          if (!sub && m === 'GET') return send(res, 200, row)
          if (!sub && m === 'DELETE') {
            databases = databases.filter((d) => d !== row)
            return send(res, 204)
          }
          if (sub === 'connection') return send(res, 200, { ...creds(row), password: undefined })
          if (sub === 'password') return send(res, 200, creds(row))
          if (sub === 'live-connections') return send(res, 200, { connections: liveConnections })
          if (sub === 'stats') return send(res, 200, stats)
          if (sub === 'schema') return send(res, 200, row.slug === 'taller-motos' ? schema : { schemas: [], tables: [] })
          if (sub === 'users' && m === 'GET') return send(res, 200, { users: row.slug === 'taller-motos' ? users : [] })
          if (sub === 'users' && m === 'POST') {
            const { name } = await readJSON(req)
            return send(res, 201, creds(row, `${row.database}__${name}`))
          }
          if (sub === 'users') return send(res, parts[4] === 'password' ? 200 : 204, parts[4] === 'password' ? creds(row, `${row.database}__${parts[3]}`) : undefined)
          if (sub === 'jobs') return send(res, 200, { jobs: jobs.filter((j) => j.slug === row.slug) })
          if (sub === 'backups' && m === 'GET') return send(res, 200, { backups: jobs.filter((j) => j.slug === row.slug && j.status === 'done') })
          if (sub === 'backups' && m === 'POST') {
            const j = { id: nextId++, kind: 'dump', status: 'done', slug: row.slug, file: `${row.database}_manual.dump`, bytes: 1_300_000, reason: 'manual', requestedBy: 'admin', createdAt: new Date().toISOString() }
            jobs = [j as (typeof jobs)[number], ...jobs]
            return send(res, 202, j)
          }
          if (sub === 'expiry') {
            const { neverExpires, maxIdleDays } = await readJSON(req)
            Object.assign(row, { neverExpires, maxIdleDays: maxIdleDays ?? undefined, expiresAt: neverExpires ? undefined : iso(Number(maxIdleDays ?? 30)) })
            return send(res, 200, row)
          }
          if (sub === 'extend') return send(res, 200, row)
          if (sub === 'query') {
            const { sql } = await readJSON(req)
            if (String(sql).toLowerCase().startsWith('delete')) return send(res, 400, { error: 'ERROR: cannot execute DELETE in a read-only transaction (SQLSTATE 25006)' })
            return send(res, 200, { columns: ['id', 'nombre', 'email', 'rol'], rows: [['1', 'Ana López', 'ana@example.com', 'presidenta'], ['2', 'Bruno Díaz', null, 'miembro'], ['3', 'Carla Ruiz', 'carla@example.com', 'tesorera']], rowCount: 3, truncated: false, durationMs: 4 })
          }
          if (sub === 'tables' && parts[5] === 'data') {
            const t = schema.tables.find((x) => x.name === parts[4])
            if (!t) return send(res, 400, { error: 'la tabla no existe' })
            const cols = (t.columns as { name: string; type: string; nullable: boolean; primaryKey: boolean; default?: string }[]).map((c) => ({
              name: c.name, type: c.type, nullable: c.nullable, primaryKey: c.primaryKey, hasDefault: Boolean(c.default), generated: false,
            }))
            const rows = (dataRows[t.name] ??= Array.from({ length: Math.min(t.rows, 120) }, (_, i) =>
              cols.map((c) => (c.primaryKey ? String(i + 1) : c.name.endsWith('_id') ? String((i % 12) + 1) : c.type === 'text' ? (i % 7 === 3 ? null : `${c.name} ${i + 1}`) : c.type.startsWith('timestamp') ? '2026-10-0' + ((i % 9) + 1) + ' 10:00:00+00' : String(i * 3))),
            ))
            if (m === 'GET') {
              const q = new URL(req.url ?? '', 'http://x').searchParams
              const page = Number(q.get('page') ?? 1)
              return send(res, 200, { columns: cols, rows: rows.slice((page - 1) * 50, page * 50), total: rows.length, page, pageSize: 50, editable: t.kind === 'table' })
            }
            const body = (await readJSON(req)) as { key?: Record<string, string>; values?: Record<string, string | null> }
            const idx = rows.findIndex((r) => body.key && r[0] === body.key.id)
            if (m === 'POST') rows.push(cols.map((c, i) => (c.primaryKey ? String(rows.length + 1) : body.values?.[c.name] ?? (i ? null : ''))))
            if (m === 'PATCH' && idx >= 0) cols.forEach((c, i) => { if (body.values && c.name in body.values) rows[idx][i] = body.values[c.name] })
            if (m === 'DELETE' && idx >= 0) rows.splice(idx, 1)
            return send(res, 204)
          }
          if (sub === 'tables' && parts[5] === 'count') return send(res, 200, { rows: 349 })
          if (sub === 'tables') return send(res, 200, { columns: ['id', 'nombre', 'email'], rows: [['1', 'Ana López', 'ana@example.com'], ['2', 'Bruno Díaz', null]], rowCount: 2, truncated: false, durationMs: 2 })
        }
        return send(res, 404, { error: 'not found' })
      })
    },
  }
}
