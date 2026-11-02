// Typed client for the PosternDB JSON API.

export type Status = 'activa' | 'por_expirar' | 'expirada'
export type Role = 'owner' | 'admin' | 'viewer'
export type Access = 'read' | 'write'

export interface Session {
  user: string
  role: Role
  bootstrap: boolean
}

export interface Database {
  id: number
  slug: string
  database: string
  username: string
  hostname: string
  state: string
  createdAt: string
  lastSeenAt: string
  expiresAt?: string
  expiredAt?: string
  archivedAs?: string
  neverExpires: boolean
  maxIdleDays?: number
  graceUntil?: string
  status: Status
}

export interface ConnectionInfo {
  hostname: string
  database: string
  username: string
  host: string
  port: number
  localMode: boolean
}

export interface Credentials extends ConnectionInfo {
  password: string
}

export interface TableBrief {
  schema: string
  name: string
  rows: number
  sizeBytes: number
}

export interface Stats {
  sizeBytes: number
  openConnections: number
  totalSessions: number
  commits: number
  rollbacks: number
  tableCount: number
  totalRows: number
  topTables: TableBrief[]
}

export interface Column {
  name: string
  type: string
  nullable: boolean
  default?: string
  primaryKey: boolean
}

export interface ForeignKey {
  name: string
  columns: string[]
  refSchema: string
  refTable: string
  refColumns: string[]
}

export interface Index {
  name: string
  definition: string
  unique: boolean
}

export interface Table {
  schema: string
  name: string
  kind: 'table' | 'view' | 'materialized_view'
  rows: number
  sizeBytes: number
  comment?: string
  columns: Column[]
  foreignKeys: ForeignKey[]
  indexes: Index[]
}

export interface Schema {
  schemas: string[]
  tables: Table[]
}

export interface QueryResult {
  columns: string[]
  rows: (string | null)[][]
  rowCount: number
  truncated: boolean
  durationMs: number
}

export interface DatabaseUser {
  id: number
  name: string
  username: string
  access: Access
  createdAt: string
}

export interface Job {
  id: number
  kind: 'dump' | 'archive' | 'restore' | 'clone' | 'delete_file'
  status: 'pending' | 'running' | 'done' | 'failed'
  slug: string
  sourceDb?: string
  targetDb?: string
  file?: string
  bytes?: number
  reason?: string
  error?: string
  requestedBy: string
  createdAt: string
  finishedAt?: string
}

export interface Template {
  id: number
  name: string
  description: string
  sql: string
  updatedAt: string
}

export interface Admin {
  id: number
  username: string
  role: Role
  createdAt: string
  createdBy: string
  lastLoginAt?: string
  bootstrap: boolean
}

export interface AuditEntry {
  id: number
  occurredAt: string
  actor: string
  action: string
  target: string
  details?: Record<string, unknown>
}

export interface Settings {
  baseDomain: string
  panelHostname: string
  clientHostPrefix: string
  exampleHostname: string
  expirationDays: number
  expiringSoonDays: number
  checkIntervalHours: number
  backupBeforeDelete: boolean
  localMode: boolean
  localDbPort: number
  clientLocalPort: number
  notifyChannels: string[]
}

export interface CreateRequest {
  name: string
  templateId?: number
  cloneFrom?: string
  restoreBackupId?: number
}

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message)
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(`/api${path}`, {
    method,
    credentials: 'same-origin',
    headers: {
      // Required by the server's CSRF check on state-changing requests.
      'X-Requested-With': 'posterndb',
      ...(body === undefined ? {} : { 'Content-Type': 'application/json' }),
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  if (res.status === 204) return undefined as T
  const data = await res.json().catch(() => ({}))
  if (!res.ok) {
    const fallback =
      res.status === 429
        ? 'Demasiados intentos. Espera un minuto.'
        : res.status === 403
          ? 'Tu rol no permite esta acción.'
          : `Error ${res.status}`
    throw new ApiError(res.status, (data as { error?: string }).error ?? fallback)
  }
  return data as T
}

const db = (slug: string) => `/databases/${encodeURIComponent(slug)}`

export const api = {
  session: () => request<Session>('GET', '/auth/session'),
  login: (username: string, password: string) => request<Session>('POST', '/auth/login', { username, password }),
  logout: () => request<void>('POST', '/auth/logout'),
  changePassword: (current: string, next: string) => request<void>('POST', '/auth/password', { current, next }),
  settings: () => request<Settings>('GET', '/settings'),
  testNotification: () => request<void>('POST', '/notifications/test'),

  databases: () => request<{ databases: Database[] }>('GET', '/databases').then((r) => r.databases),
  database: (slug: string) => request<Database>('GET', db(slug)),
  create: (req: CreateRequest) => request<Credentials>('POST', '/databases', req),
  remove: (slug: string) => request<void>('DELETE', db(slug), { confirm: slug }),
  connection: (slug: string) => request<ConnectionInfo>('GET', `${db(slug)}/connection`),
  resetPassword: (slug: string) => request<Credentials>('POST', `${db(slug)}/password`),
  setExpiry: (slug: string, neverExpires: boolean, maxIdleDays: number | null) =>
    request<Database>('PUT', `${db(slug)}/expiry`, { neverExpires, maxIdleDays }),
  extend: (slug: string, days: number) => request<Database>('POST', `${db(slug)}/extend`, { days }),
  stats: (slug: string) => request<Stats>('GET', `${db(slug)}/stats`),
  schema: (slug: string) => request<Schema>('GET', `${db(slug)}/schema`),
  query: (slug: string, sql: string) => request<QueryResult>('POST', `${db(slug)}/query`, { sql }),
  preview: (slug: string, schema: string, table: string) =>
    request<QueryResult>('GET', `${db(slug)}/tables/${encodeURIComponent(schema)}/${encodeURIComponent(table)}/rows`),
  count: (slug: string, schema: string, table: string) =>
    request<{ rows: number }>('GET', `${db(slug)}/tables/${encodeURIComponent(schema)}/${encodeURIComponent(table)}/count`).then(
      (r) => r.rows,
    ),
  users: (slug: string) => request<{ users: DatabaseUser[] }>('GET', `${db(slug)}/users`).then((r) => r.users),
  createUser: (slug: string, name: string, access: Access) =>
    request<Credentials>('POST', `${db(slug)}/users`, { name, access }),
  resetUserPassword: (slug: string, name: string) =>
    request<Credentials>('POST', `${db(slug)}/users/${encodeURIComponent(name)}/password`),
  deleteUser: (slug: string, name: string) => request<void>('DELETE', `${db(slug)}/users/${encodeURIComponent(name)}`),
  jobs: (slug: string) => request<{ jobs: Job[] }>('GET', `${db(slug)}/jobs`).then((r) => r.jobs),
  backup: (slug: string) => request<Job>('POST', `${db(slug)}/backups`),
  databaseBackups: (slug: string) =>
    request<{ backups: Job[] }>('GET', `${db(slug)}/backups`).then((r) => r.backups),

  backups: () => request<{ backups: Job[] }>('GET', '/backups').then((r) => r.backups),
  backupDownloadUrl: (id: number) => `/api/backups/${id}/download`,
  deleteBackup: (id: number) => request<void>('DELETE', `/backups/${id}`),
  restoreBackup: (id: number, name: string) => request<Credentials>('POST', `/backups/${id}/restore`, { name }),

  templates: () => request<{ templates: Template[] }>('GET', '/templates').then((r) => r.templates),
  saveTemplate: (t: Pick<Template, 'name' | 'description' | 'sql'> & { id?: number }) =>
    t.id
      ? request<Template>('PUT', `/templates/${t.id}`, { name: t.name, description: t.description, sql: t.sql })
      : request<Template>('POST', '/templates', { name: t.name, description: t.description, sql: t.sql }),
  deleteTemplate: (id: number) => request<void>('DELETE', `/templates/${id}`),

  audit: () => request<{ entries: AuditEntry[] }>('GET', '/audit').then((r) => r.entries),

  admins: () => request<{ admins: Admin[] }>('GET', '/admins').then((r) => r.admins),
  createAdmin: (username: string, role: Role) =>
    request<{ admin: Admin; password: string }>('POST', '/admins', { username, role }),
  setAdminRole: (username: string, role: Role) =>
    request<void>('PATCH', `/admins/${encodeURIComponent(username)}`, { username, role }),
  resetAdminPassword: (username: string) =>
    request<{ password: string }>('POST', `/admins/${encodeURIComponent(username)}/password`),
  deleteAdmin: (username: string) => request<void>('DELETE', `/admins/${encodeURIComponent(username)}`),
}

export const can = {
  operate: (role: Role) => role === 'owner' || role === 'admin',
  manageAdmins: (role: Role) => role === 'owner',
}
