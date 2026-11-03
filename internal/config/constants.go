// Package config centralises every environment variable name, default value
// and repeated literal used across PosternDB. Keeping them here avoids
// duplicated string literals (SonarQube go:S1192) and gives maintainers a
// single place to look when renaming a setting.
package config

import "time"

// Environment variable names. The .env file is the single source of truth.
const (
	EnvAdminUser             = "ADMIN_USER"
	EnvAdminPassword         = "ADMIN_PASSWORD"
	EnvJWTSecret             = "JWT_SECRET"
	EnvBaseDomain            = "BASE_DOMAIN"
	EnvPanelHostname         = "PANEL_HOSTNAME"
	EnvClientHostPrefix      = "CLIENT_HOST_PREFIX"
	EnvCloudflareAPIToken    = "CLOUDFLARE_API_TOKEN"
	EnvCloudflareZoneID      = "CLOUDFLARE_ZONE_ID"
	EnvCloudflareTunnelToken = "CLOUDFLARE_TUNNEL_TOKEN"
	EnvDBExpirationDays      = "DB_EXPIRATION_DAYS"
	EnvExpiringSoonDays      = "EXPIRING_SOON_DAYS"
	EnvCheckIntervalHours    = "CHECK_INTERVAL_HOURS"
	EnvBackupBeforeDelete    = "BACKUP_BEFORE_DELETE"
	EnvPostgresUser          = "POSTGRES_USER"
	EnvPostgresPassword      = "POSTGRES_PASSWORD"
	EnvPostgresDB            = "POSTGRES_DB"
	EnvPostgresHost          = "POSTGRES_HOST"
	EnvPostgresPort          = "POSTGRES_PORT"
	EnvHTTPAddr              = "HTTP_ADDR"
	EnvTunnelDBService       = "TUNNEL_DB_SERVICE"
	EnvTunnelPanelService    = "TUNNEL_PANEL_SERVICE"
	EnvClientDBPort          = "CLIENT_LOCAL_PORT"
	EnvLocalMode             = "LOCAL_MODE"
	EnvLocalDBPort           = "LOCAL_DB_PORT"
	EnvBackupDir             = "BACKUP_DIR"
	EnvAllowSelfRegister     = "ALLOW_SELF_REGISTER"
	EnvGoogleClientID        = "GOOGLE_CLIENT_ID"
	EnvGoogleClientSecret    = "GOOGLE_CLIENT_SECRET"
	EnvGoogleRedirectURL     = "GOOGLE_REDIRECT_URL"
	EnvPanelLocalPort        = "PANEL_LOCAL_PORT"
	EnvMaxDatabasesPerClient = "MAX_DATABASES_PER_CLIENT"
	EnvNotifyWebhookURL      = "NOTIFY_WEBHOOK_URL"
	EnvNotifyEmailTo         = "NOTIFY_EMAIL_TO"
	EnvSMTPHost              = "SMTP_HOST"
	EnvSMTPPort              = "SMTP_PORT"
	EnvSMTPUsername          = "SMTP_USERNAME"
	EnvSMTPPassword          = "SMTP_PASSWORD"
	EnvSMTPFrom              = "SMTP_FROM"
)

// Default values applied when the corresponding variable is empty.
const (
	DefaultDBExpirationDays   = 30
	DefaultExpiringSoonDays   = 7
	DefaultCheckIntervalHours = 6
	DefaultBackupBeforeDelete = true
	DefaultPostgresHost       = "db"
	DefaultPostgresPort       = 5432
	DefaultHTTPAddr           = ":8080"
	DefaultPanelSubdomain     = "postern"
	DefaultTunnelDBService    = "tcp://db:5432"
	DefaultTunnelPanelService = "http://app:8080"
	DefaultClientLocalPort    = 5432
	MinJWTSecretLength        = 32
	MinAdminPasswordLength    = 12
	MaxClientHostPrefixLength = 20
	DefaultLocalDBPort        = 25432
	DefaultBackupDir          = "/backups"
	DefaultSMTPPort           = 587
	LocalHostname             = "localhost"
	DefaultAllowSelfRegister  = true
	DefaultPanelLocalPort     = 28437
	DefaultMaxDBsPerClient    = 3
	MinClientPasswordLength   = 12
)

// Panel administrator roles, from most to least privileged.
const (
	RoleOwner  = "owner"
	RoleAdmin  = "admin"
	RoleViewer = "viewer"
)

// RoleClient identifies a self-service tenant (ClientUser), never part of
// the admin role hierarchy above: it only ever unlocks the reduced
// "panel de usuario" (/api/me/*), never an admin route.
const RoleClient = "client"

// Access levels for additional database users.
const (
	AccessRead  = "read"
	AccessWrite = "write"
)

// Backup sidecar job kinds and states.
const (
	JobDump       = "dump"
	JobArchive    = "archive"
	JobRestore    = "restore"
	JobClone      = "clone"
	JobDeleteFile = "delete_file"

	JobPending = "pending"
	JobRunning = "running"
	JobDone    = "done"
	JobFailed  = "failed"

	ReasonManual   = "manual"
	ReasonExpiry   = "expiry"
	ReasonDeletion = "deletion"
	DumpExtension  = ".dump"
)

// Connections PosternDB opens into client databases. The application_name
// lets the inactivity auditor ignore them in pg_stat_activity.
const (
	AppNameInspector = "posterndb-inspector"
	AppNameConsole   = "posterndb-console"
	ConsoleRole      = "posterndb_console"
	ConsoleRowLimit  = 500
	ConsoleTimeout   = 10 * time.Second
	PreviewRowLimit  = 50
	DataPageSize     = 50
	MaxDataPageSize  = 200
	MaxQueryLength   = 20000
	MaxTemplateSQL   = 200000
	// MaxConnectionQueryChars truncates the running statement shown in the live connections list.
	MaxConnectionQueryChars = 160
	ExtraUserSep            = "__"
)

// Client database lifecycle states as stored in the metadata table.
const (
	StateActive  = "active"
	StateExpired = "expired"
	StateDeleted = "deleted"
)

// Status labels exposed to the panel. "por_expirar" is never stored: it is
// derived from last_seen_at at read time so it always reflects the current
// configuration of DB_EXPIRATION_DAYS / EXPIRING_SOON_DAYS.
const (
	StatusActive   = "activa"
	StatusExpiring = "por_expirar"
	StatusExpired  = "expirada"
)

// Audit log actions.
const (
	ActionDBCreated             = "db_created"
	ActionDBDeleted             = "db_deleted"
	ActionDBExpired             = "db_expired"
	ActionDBArchived            = "db_archived_for_backup"
	ActionDBDropped             = "db_dropped"
	ActionRouteCreated          = "route_created"
	ActionRouteRemoved          = "route_removed"
	ActionActivitySeen          = "activity_detected"
	ActionAuditRun              = "inactivity_audit_run"
	ActionLoginSuccess          = "login_success"
	ActionLoginFailed           = "login_failed"
	ActionHostnameMigrated      = "hostname_migrated"
	ActionProvisionFailed       = "provision_failed"
	ActionPasswordReset         = "password_reset"
	ActionExpiryChanged         = "expiry_changed"
	ActionUserCreated           = "db_user_created"
	ActionUserDeleted           = "db_user_deleted"
	ActionBackupRequested       = "backup_requested"
	ActionRestoreRequested      = "restore_requested"
	ActionCloneRequested        = "clone_requested"
	ActionBackupDeleted         = "backup_deleted"
	ActionTemplateSaved         = "template_saved"
	ActionTemplateDeleted       = "template_deleted"
	ActionTemplateApplied       = "template_applied"
	ActionAdminCreated          = "admin_created"
	ActionAdminUpdated          = "admin_updated"
	ActionAdminDeleted          = "admin_deleted"
	ActionQueryExecuted         = "query_executed"
	ActionExpiryWarned          = "expiry_warning_sent"
	ActionNotifyFailed          = "notification_failed"
	ActionRowInserted           = "row_inserted"
	ActionRowUpdated            = "row_updated"
	ActionRowDeleted            = "row_deleted"
	ActionGlobalExpiryChanged   = "global_expiry_changed"
	ActionClientRegistered      = "client_registered"
	ActionClientCreated         = "client_created"
	ActionClientDeleted         = "client_deleted"
	ActionClientExpiryChanged   = "client_expiry_changed"
	ActionClientPasswordChanged = "client_password_changed"
	ActionClientPasswordReset   = "client_password_reset"
	ActionClientGoogleLinked    = "client_google_linked"
)

// Audit actor names.
const (
	ActorSystem  = "system"
	ActorAuditor = "inactivity-auditor"
)

// Naming rules for objects created inside the PostgreSQL cluster. Every
// client object carries a fixed prefix so it can never collide with
// "postgres", "template0/1" or the platform database itself.
const (
	ClientDBPrefix      = "pdb_"
	ArchivedDBPrefix    = "pdbarch_"
	ArchiveTimeLayout   = "20060102150405"
	GeneratedPasswordLn = 32
	ClientConnLimit     = 20
)

// HTTP routes.
const (
	RouteHealth          = "/health"
	RouteAPI             = "/api"
	RouteLogin           = "/auth/login"
	RouteRegister        = "/auth/register"
	RouteLogout          = "/auth/logout"
	RouteSession         = "/auth/session"
	RouteDatabases       = "/databases"
	RouteAuditLog        = "/audit"
	RouteSettings        = "/settings"
	RouteAuthPassword    = "/auth/password"
	RouteDatabase        = "/databases/{slug}"
	RouteConnection      = "/databases/{slug}/connection"
	RouteDBPassword      = "/databases/{slug}/password"
	RouteExpiry          = "/databases/{slug}/expiry"
	RouteExtend          = "/databases/{slug}/extend"
	RouteStats           = "/databases/{slug}/stats"
	RouteLiveConnections = "/databases/{slug}/live-connections"
	RoutePublicConfig    = "/auth/config"
	RouteSchema          = "/databases/{slug}/schema"
	RouteTableRows       = "/databases/{slug}/tables/{schema}/{table}/rows"
	RouteTableCount      = "/databases/{slug}/tables/{schema}/{table}/count"
	RouteTableData       = "/databases/{slug}/tables/{schema}/{table}/data"
	RouteQuery           = "/databases/{slug}/query"
	RouteDBUsers         = "/databases/{slug}/users"
	RouteDBUser          = "/databases/{slug}/users/{name}"
	RouteDBUserPass      = "/databases/{slug}/users/{name}/password"
	RouteDBJobs          = "/databases/{slug}/jobs"
	RouteDBBackups       = "/databases/{slug}/backups"
	RouteBackups         = "/backups"
	RouteBackup          = "/backups/{id}"
	RouteBackupFile      = "/backups/{id}/download"
	RouteBackupRest      = "/backups/{id}/restore"
	RouteTemplates       = "/templates"
	RouteTemplate        = "/templates/{id}"
	RouteAdmins          = "/admins"
	RouteAdmin           = "/admins/{name}"
	RouteAdminPass       = "/admins/{name}/password"
	RouteNotifyTest      = "/notifications/test"
	RouteGlobalExpiry    = "/settings/expiry"
	RouteClients         = "/clients"
	RouteClient          = "/clients/{id}"
	RouteClientExpiry    = "/clients/{id}/expiry"
	RouteClientPassword  = "/clients/{id}/password"
	RouteGoogleStart     = "/auth/google/start"
	RouteGoogleCallback  = "/auth/google/callback"
	URLParamSlug         = "slug"
	URLParamID           = "id"
	URLParamName         = "name"
	URLParamSchema       = "schema"
	URLParamTable        = "table"
)

// Routes under /api/me — the reduced, self-service panel a ClientUser lands
// on after logging in. Every one of them re-checks, server-side, that the
// requested database belongs to the authenticated client (see
// httpapi.ownershipMiddleware): a client can never reach another client's
// data by guessing a slug.
const (
	RouteMyAccountPassword = "/me/password"
	RouteMyDatabases       = "/me/databases"
	RouteMyDatabase        = "/me/databases/{slug}"
	RouteMyConnection      = "/me/databases/{slug}/connection"
	RouteMyStats           = "/me/databases/{slug}/stats"
	RouteMyLiveConnections = "/me/databases/{slug}/live-connections"
	RouteMyTemplates       = "/me/templates"
	RouteMySchema          = "/me/databases/{slug}/schema"
	RouteMyDBPassword      = "/me/databases/{slug}/password"
	RouteMyTableData       = "/me/databases/{slug}/tables/{schema}/{table}/data"
	RouteMyTableCount      = "/me/databases/{slug}/tables/{schema}/{table}/count"
)

// HTTP / session details.
const (
	SessionCookieName = "posterndb_session"
	// OAuthCookieName carries state/nonce/PKCE verifier between the Google
	// redirect and its callback; scoped to OAuthCookiePath only.
	OAuthCookieName     = "posterndb_oauth"
	OAuthCookiePath     = "/api/auth/google"
	SessionTTL          = 12 * time.Hour
	HeaderContentType   = "Content-Type"
	HeaderCFConnectIP   = "CF-Connecting-IP"
	HeaderRequestedWith = "X-Requested-With"
	RequestedWithValue  = "posterndb"
	MimeJSON            = "application/json"
	JWTIssuer           = "posterndb"
	LoginRatePerMinute  = 5
	LoginRateBurst      = 5
	MaxJSONBodyBytes    = 1 << 16
	ShutdownTimeout     = 15 * time.Second
	ReadHeaderTimeout   = 10 * time.Second
	ExternalCallTimeout = 30 * time.Second
	AuditLogPageSize    = 100
)

// Cloudflare details.
const (
	ProviderCloudflare   = "cloudflare"
	DNSRecordTypeCNAME   = "CNAME"
	TunnelCNAMESuffix    = ".cfargotunnel.com"
	IngressCatchAll      = "http_status:404"
	DNSRecordComment     = "managed by posterndb"
	CommandHealthcheck   = "healthcheck"
	CommandMigrateHosts  = "migrate-hostnames"
	CommandServe         = "serve"
	FlagDryRun           = "--dry-run"
	HealthcheckURLFormat = "http://127.0.0.1%s" + RouteHealth
)
