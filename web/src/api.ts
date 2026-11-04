// Typed client for the PosternDB JSON API.

export type Status = 'activa' | 'por_expirar' | 'expirada'
export type Role = 'owner' | 'admin' | 'viewer' | 'client'
export type Access = 'read' | 'write'

export interface Session {
  user: string
  role: Role
  bootstrap: boolean
  // Only for role "client".
  email?: string
  fullName?: string
  hasPassword?: boolean
  needsUsername?: boolean
  googleLinked?: boolean
}

/** What a registered user sees and edits about their own account. */
export interface Account {
  username: string
  email: string
  fullName: string
  googleLinked: boolean
  hasPassword: boolean
  needsUsername: boolean
  createdAt: string
  maxDatabases: number
  databaseCount: number
  /** Leads the name of the user's databases (abcd12-tienda). */
  code: string
  hostPrefix: string
  baseDomain: string
  localMode: boolean
}

export interface RegisterForm {
  email: string
  fullName: string
  password: string
  acceptEmailUse: boolean
}

export interface PublicConfig {
  allowSelfRegister: boolean
  googleEnabled: boolean
  /** false when AUTH_MODE=google: registered users have no password login. */
  passwordEnabled: boolean
  /** Public site with the privacy policy and the terms; empty = no links. */
  landingUrl: string
}

export interface Database {
  clientUserId?: number
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

export interface DataColumn {
  name: string
  type: string
  nullable: boolean
  primaryKey: boolean
  hasDefault: boolean
  generated: boolean
}

export interface TableData {
  columns: DataColumn[]
  rows: (string | null)[][]
  total: number
  page: number
  pageSize: number
  editable: boolean
}

export type RowValues = Record<string, string | null>

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
  shared: boolean
  updatedAt: string
}

export interface Connection {
  pid: number
  user: string
  application: string
  state: string
  connectedAt: string
  stateSince?: string
  query?: string
}

export interface AuditEntry {
  id: number
  occurredAt: string
  actor: string
  action: string
  target: string
  details?: Record<string, unknown>
}

/** What an account is: a plain user, or someone who also opens the admin panel. */
export type AccountRole = 'user' | 'viewer' | 'admin'

export interface ClientUser {
  id: number
  username: string
  role: AccountRole
  email?: string
  fullName?: string
  googleLinked: boolean
  hasPassword: boolean
  neverExpires: boolean
  maxIdleDays?: number
  createdAt: string
  createdBy?: string
  updatedAt: string
  updatedBy?: string
  databaseCount?: number
  lastLoginAt?: string
  code?: string
}

export interface PlatformSettings {
  expirationDays: number | null
  expiringSoonDays: number | null
  updatedAt: string
  updatedBy?: string
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
  allowSelfRegister: boolean
  maxDatabasesPerClient: number
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
const tablePath = (slug: string, schema: string, table: string) =>
  `${db(slug)}/tables/${encodeURIComponent(schema)}/${encodeURIComponent(table)}`

export const api = {
  session: () => request<Session>('GET', '/auth/session'),
  login: (username: string, password: string, remember = false) => request<Session>('POST', '/auth/login', { username, password, remember }),
  register: (form: RegisterForm) => request<Session>('POST', '/auth/register', form),
  logout: () => request<void>('POST', '/auth/logout'),
  publicConfig: () => request<PublicConfig>('GET', '/auth/config'),
  changePassword: (current: string, next: string) => request<void>('POST', '/auth/password', { current, next }),
  settings: () => request<Settings>('GET', '/settings'),
  testNotification: () => request<void>('POST', '/notifications/test'),
  globalExpiry: () => request<PlatformSettings>('GET', '/settings/expiry'),
  setGlobalExpiry: (expirationDays: number | null, expiringSoonDays: number | null) =>
    request<PlatformSettings>('PUT', '/settings/expiry', { expirationDays, expiringSoonDays }),
  clients: () => request<{ clients: ClientUser[] }>('GET', '/clients').then((r) => r.clients),
  createClient: (email: string, fullName: string) => request<{ client: ClientUser; password: string }>('POST', '/clients', { email, fullName }),
  resetClientPassword: (id: number) => request<{ password: string }>('POST', `/clients/${id}/password`),
  setClientExpiry: (id: number, neverExpires: boolean, maxIdleDays: number | null) =>
    request<ClientUser>('PUT', `/clients/${id}/expiry`, { neverExpires, maxIdleDays }),
  deleteClient: (id: number) => request<void>('DELETE', `/clients/${id}`),

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
  liveConnections: (slug: string) =>
    request<{ connections: Connection[] }>('GET', `${db(slug)}/live-connections`).then((r) => r.connections),
  schema: (slug: string) => request<Schema>('GET', `${db(slug)}/schema`),
  query: (slug: string, sql: string) => request<QueryResult>('POST', `${db(slug)}/query`, { sql }),
  preview: (slug: string, schema: string, table: string) =>
    request<QueryResult>('GET', `${db(slug)}/tables/${encodeURIComponent(schema)}/${encodeURIComponent(table)}/rows`),
  count: (slug: string, schema: string, table: string) =>
    request<{ rows: number }>('GET', `${db(slug)}/tables/${encodeURIComponent(schema)}/${encodeURIComponent(table)}/count`).then(
      (r) => r.rows,
    ),
  // knownTotal skips the server's row count (a full table scan) when it is already known.
  tableData: (slug: string, schema: string, table: string, page: number, sort?: string, desc?: boolean, knownTotal?: number) =>
    request<TableData>(
      'GET',
      `${tablePath(slug, schema, table)}/data?page=${page}&size=50${sort ? `&sort=${encodeURIComponent(sort)}&dir=${desc ? 'desc' : 'asc'}` : ''}${knownTotal === undefined ? '' : `&total=${knownTotal}`}`,
    ),
  insertRow: (slug: string, schema: string, table: string, values: RowValues) =>
    request<void>('POST', `${tablePath(slug, schema, table)}/data`, { values }),
  updateRow: (slug: string, schema: string, table: string, key: RowValues, values: RowValues) =>
    request<void>('PATCH', `${tablePath(slug, schema, table)}/data`, { key, values }),
  deleteRow: (slug: string, schema: string, table: string, key: RowValues) =>
    request<void>('DELETE', `${tablePath(slug, schema, table)}/data`, { key }),
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
  saveTemplate: (t: Pick<Template, 'name' | 'description' | 'sql' | 'shared'> & { id?: number }) => {
    const body = { name: t.name, description: t.description, sql: t.sql, shared: t.shared }
    return t.id ? request<Template>('PUT', `/templates/${t.id}`, body) : request<Template>('POST', '/templates', body)
  },
  deleteTemplate: (id: number) => request<void>('DELETE', `/templates/${id}`),

  audit: () => request<{ entries: AuditEntry[] }>('GET', '/audit').then((r) => r.entries),

  /** People with an admin role (accounts), plus the owner from .env. */
  admins: () => request<{ owner: { username: string }; admins: ClientUser[] }>('GET', '/admins'),
  setClientRole: (id: number, role: AccountRole) => request<ClientUser>('PUT', `/clients/${id}/role`, { role }),
  /** The signed-in person's own activity. */
  myActivity: () => request<{ entries: AuditEntry[] }>('GET', '/activity/me').then((r) => r.entries),
  /** Activity of one person (user or administrator). */
  clientActivity: (id: number) => request<{ entries: AuditEntry[] }>('GET', `/clients/${id}/activity`).then((r) => r.entries),
}

// ---- "Mis bases": the reduced panel a self-service client lands on ------------------------

const myDb = (slug: string) => `/me/databases/${encodeURIComponent(slug)}`
const myTablePath = (slug: string, schema: string, table: string) =>
  `${myDb(slug)}/tables/${encodeURIComponent(schema)}/${encodeURIComponent(table)}`

export const me = {
  changePassword: (current: string, next: string) => request<void>('POST', '/me/password', { current, next }),
  account: () => request<Account>('GET', '/me/account'),
  updateAccount: (patch: { username?: string; fullName?: string }) => request<Account>('PATCH', '/me/account', patch),
  googleLinkUrl: '/api/me/google/link',
  backups: () => request<{ backups: Job[] }>('GET', '/me/backups').then((r) => r.backups),
  createBackup: (slug: string) => request<Job>('POST', `${myDb(slug)}/backups`),
  backupDownloadUrl: (id: number) => `/api/me/backups/${id}/download`,
  deleteBackup: (id: number) => request<void>('DELETE', `/me/backups/${id}`),
  restoreBackup: (id: number, name: string) => request<Credentials>('POST', `/me/backups/${id}/restore`, { name }),
  activity: () => request<{ entries: AuditEntry[] }>('GET', '/me/activity').then((r) => r.entries),
  databases: () => request<{ databases: Database[] }>('GET', '/me/databases').then((r) => r.databases),
  templates: () => request<{ templates: Template[] }>('GET', '/me/templates').then((r) => r.templates),
  create: (name: string, templateId?: number) => request<Credentials>('POST', '/me/databases', { name, templateId }),
  database: (slug: string) => request<Database>('GET', myDb(slug)),
  remove: (slug: string) => request<void>('DELETE', myDb(slug), { confirm: slug }),
  connection: (slug: string) => request<ConnectionInfo>('GET', `${myDb(slug)}/connection`),
  resetPassword: (slug: string) => request<Credentials>('POST', `${myDb(slug)}/password`),
  stats: (slug: string) => request<Stats>('GET', `${myDb(slug)}/stats`),
  liveConnections: (slug: string) =>
    request<{ connections: Connection[] }>('GET', `${myDb(slug)}/live-connections`).then((r) => r.connections),
  schema: (slug: string) => request<Schema>('GET', `${myDb(slug)}/schema`),
  count: (slug: string, schema: string, table: string) =>
    request<{ rows: number }>('GET', `${myTablePath(slug, schema, table)}/count`).then((r) => r.rows),
  tableData: (slug: string, schema: string, table: string, page: number, sort?: string, desc?: boolean, knownTotal?: number) =>
    request<TableData>(
      'GET',
      `${myTablePath(slug, schema, table)}/data?page=${page}&size=50${sort ? `&sort=${encodeURIComponent(sort)}&dir=${desc ? 'desc' : 'asc'}` : ''}${knownTotal === undefined ? '' : `&total=${knownTotal}`}`,
    ),
  insertRow: (slug: string, schema: string, table: string, values: RowValues) =>
    request<void>('POST', `${myTablePath(slug, schema, table)}/data`, { values }),
  updateRow: (slug: string, schema: string, table: string, key: RowValues, values: RowValues) =>
    request<void>('PATCH', `${myTablePath(slug, schema, table)}/data`, { key, values }),
  deleteRow: (slug: string, schema: string, table: string, key: RowValues) =>
    request<void>('DELETE', `${myTablePath(slug, schema, table)}/data`, { key }),
}

export const can = {
  operate: (role: Role) => role === 'owner' || role === 'admin',
  /** Users and who is an administrator: the owner and administrators. */
  manageUsers: (role: Role) => role === 'owner' || role === 'admin',
  manageExpiry: (role: Role) => role === 'owner',
}
