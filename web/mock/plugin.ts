// Development-only mock of the PosternDB API so the panel can be designed
// and reviewed without Postgres or Cloudflare. Enabled by `npm run dev:mock`
// (login admin / admin). Never part of the production build.
import type { IncomingMessage, ServerResponse } from 'node:http'
import type { Plugin } from 'vite'

const DAY = 86_400_000
const now = Date.now()
const iso = (offsetDays: number) => new Date(now + offsetDays * DAY).toISOString()

const settings = {
  baseDomain: 'pachislab.com',
  panelHostname: 'postern.pachislab.com',
  clientHostPrefix: 'postern-',
  exampleHostname: 'postern-cliente1.pachislab.com',
  expirationDays: 30,
  expiringSoonDays: 7,
  checkIntervalHours: 6,
  backupBeforeDelete: true,
  localMode: false,
  localDbPort: 25432,
  clientLocalPort: 5432,
  notifyChannels: ['webhook'],
}

type Row = Record<string, unknown> & { slug: string; status: string; id: number }
let nextId = 10
const mk = (id: number, slug: string, created: number, seen: number, status: string, extra = {}): Row => ({
  id,
  slug,
  database: `pdb_${slug.replaceAll('-', '_')}`,
  username: `pdb_${slug.replaceAll('-', '_')}`,
  hostname: `postern-${slug}.pachislab.com`,
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
  { id: 1, name: 'Contactos (demo)', description: 'Tabla de contactos con datos de ejemplo.', sql: 'CREATE TABLE contactos (\n  id BIGSERIAL PRIMARY KEY,\n  nombre TEXT NOT NULL\n);', updatedAt: iso(-3) },
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
  { id: 4, occurredAt: iso(-2), actor: 'admin', action: 'db_created', target: 'taller-motos', details: { hostname: 'postern-taller-motos.pachislab.com' } },
  { id: 3, occurredAt: iso(-2.1), actor: 'system', action: 'login_failed', target: '', details: { ip: '189.203.10.4' } },
  { id: 2, occurredAt: iso(-15), actor: 'inactivity-auditor', action: 'db_expired', target: 'demo-feria', details: {} },
  { id: 1, occurredAt: iso(-15), actor: 'backup-sidecar', action: 'backup_completed', target: 'demo-feria', details: { file: '/backups/pdbarch_demo_feria_20261018.dump' } },
]

let loggedIn = false
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
            return send(res, 200, { user: 'admin', role: 'owner' })
          }
          return send(res, 401, { error: 'usuario o contraseña incorrectos' })
        }
        if (path === '/auth/logout') {
          loggedIn = false
          return send(res, 204)
        }
        if (!loggedIn) return send(res, 401, { error: 'Unauthorized' })
        if (path === '/auth/session') return send(res, 200, { user: 'admin', role: 'owner', bootstrap: true })
        if (path === '/settings') return send(res, 200, settings)
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
          const body = (await readJSON(req)) as { name: string; description: string; sql: string }
          const t = { id: parts[1] ? Number(parts[1]) : nextId++, ...body, updatedAt: new Date().toISOString() }
          templates = [...templates.filter((x) => x.id !== t.id), t]
          return send(res, parts[1] ? 200 : 201, t)
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
          if (sub === 'tables' && parts[5] === 'count') return send(res, 200, { rows: 349 })
          if (sub === 'tables') return send(res, 200, { columns: ['id', 'nombre', 'email'], rows: [['1', 'Ana López', 'ana@example.com'], ['2', 'Bruno Díaz', null]], rowCount: 2, truncated: false, durationMs: 2 })
        }
        return send(res, 404, { error: 'not found' })
      })
    },
  }
}
