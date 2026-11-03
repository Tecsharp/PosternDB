// Package httpapi exposes the JSON API used by the panel and serves the
// embedded React build.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/tecsharp/posterndb/internal/admins"
	"github.com/tecsharp/posterndb/internal/auth"
	"github.com/tecsharp/posterndb/internal/clients"
	"github.com/tecsharp/posterndb/internal/config"
	"github.com/tecsharp/posterndb/internal/model"
	"github.com/tecsharp/posterndb/internal/notify"
	"github.com/tecsharp/posterndb/internal/provision"
)

// Databases is the provisioning surface used by the handlers.
type Databases interface {
	List(ctx context.Context) ([]provision.DatabaseView, error)
	Get(ctx context.Context, slug string) (provision.DatabaseView, error)
	Create(ctx context.Context, req provision.CreateRequest, actor string) (model.Credentials, error)
	Delete(ctx context.Context, slug, actor string) error
	ConnectionInfo(db model.ClientDatabase) model.ConnectionInfo
	ResetPassword(ctx context.Context, slug, actor string) (model.Credentials, error)
	SetExpiry(ctx context.Context, slug string, req provision.ExpiryRequest, actor string) (provision.DatabaseView, error)
	Extend(ctx context.Context, slug string, days int, actor string) (provision.DatabaseView, error)
	GlobalSettings(ctx context.Context) (model.PlatformSettings, error)
	SetGlobalSettings(ctx context.Context, days, warnDays *int, actor string) (model.PlatformSettings, error)
	ListClients(ctx context.Context) ([]model.ClientUser, error)
	SetClientExpiry(ctx context.Context, id int64, never bool, maxIdleDays *int, actor string) (model.ClientUser, error)
	ClientDatabases(ctx context.Context, clientUserID int64) ([]provision.DatabaseView, error)
	CountByClient(ctx context.Context, clientUserID int64) (int, error)
	Stats(ctx context.Context, slug string) (model.DatabaseStats, error)
	Connections(ctx context.Context, slug string) ([]model.Connection, error)
	SharedTemplates(ctx context.Context) ([]model.Template, error)
	Schema(ctx context.Context, slug string) (model.Schema, error)
	Query(ctx context.Context, slug, sql, actor string) (model.QueryResult, error)
	Preview(ctx context.Context, slug, schema, table string) (model.QueryResult, error)
	CountRows(ctx context.Context, slug, schema, table string) (int64, error)
	Browse(ctx context.Context, slug string, req provision.BrowseRequest) (model.TableData, error)
	InsertRow(ctx context.Context, slug, schema, table string, values model.RowValues, actor string) error
	UpdateRow(ctx context.Context, slug, schema, table string, key, values model.RowValues, actor string) error
	DeleteRow(ctx context.Context, slug, schema, table string, key model.RowValues, actor string) error
	ListUsers(ctx context.Context, slug string) ([]model.DatabaseUser, error)
	CreateUser(ctx context.Context, slug, name, access, actor string) (model.Credentials, error)
	ResetUserPassword(ctx context.Context, slug, name, actor string) (model.Credentials, error)
	DeleteUser(ctx context.Context, slug, name, actor string) error
	Backup(ctx context.Context, slug, actor string) (model.Job, error)
	ListBackups(ctx context.Context, slug string) ([]model.Job, error)
	ListJobs(ctx context.Context, slug string) ([]model.Job, error)
	BackupPath(ctx context.Context, id int64) (string, model.Job, error)
	DeleteBackup(ctx context.Context, id int64, actor string) error
	ListTemplates(ctx context.Context) ([]model.Template, error)
	SaveTemplate(ctx context.Context, t model.Template, actor string) (model.Template, error)
	DeleteTemplate(ctx context.Context, id int64, actor string) error
	RecordAudit(ctx context.Context, actor, action, target string, details map[string]any)
}

// Admins manages panel administrators.
type Admins interface {
	List(ctx context.Context) ([]model.Admin, error)
	Create(ctx context.Context, username, role, actor string) (model.Admin, string, error)
	SetRole(ctx context.Context, username, role, actor string) error
	ResetPassword(ctx context.Context, username, actor string) (string, error)
	ChangeOwnPassword(ctx context.Context, username, current, next string) error
	Delete(ctx context.Context, username, actor string) error
}

// Clients manages self-service tenants (model.ClientUser): their account
// lifecycle (register/create/delete/password). Listing and the owner's
// expiry exception live on Databases (provision.Service), which already
// enriches each client with its database count.
type Clients interface {
	Register(ctx context.Context, username, password string) (model.ClientUser, error)
	Create(ctx context.Context, username, actor string) (model.ClientUser, string, error)
	ChangeOwnPassword(ctx context.Context, id int64, current, next string) error
	Delete(ctx context.Context, id int64, actor string) error
}

// AuditReader lists audit_log entries.
type AuditReader interface {
	ListAudit(ctx context.Context, limit int) ([]model.AuditEntry, error)
}

// Pinger reports database reachability for /health.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Notifier sends test notifications.
type Notifier interface {
	Send(ctx context.Context, m notify.Message) error
	Channels() []string
}

// Settings is the read-only configuration summary shown in the panel.
type Settings struct {
	BaseDomain         string   `json:"baseDomain"`
	PanelHostname      string   `json:"panelHostname"`
	ClientHostPrefix   string   `json:"clientHostPrefix"`
	ExampleHostname    string   `json:"exampleHostname"`
	ExpirationDays     int      `json:"expirationDays"`
	ExpiringSoonDays   int      `json:"expiringSoonDays"`
	CheckIntervalHours int      `json:"checkIntervalHours"`
	BackupBeforeDelete bool     `json:"backupBeforeDelete"`
	LocalMode          bool     `json:"localMode"`
	LocalDBPort        int      `json:"localDbPort"`
	ClientLocalPort    int      `json:"clientLocalPort"`
	NotifyChannels     []string `json:"notifyChannels"`
	// AllowSelfRegister and MaxDatabasesPerClient tell the frontend whether
	// to show the public sign-up link and how many databases a client may
	// create before hitting their quota.
	AllowSelfRegister     bool `json:"allowSelfRegister"`
	MaxDatabasesPerClient int  `json:"maxDatabasesPerClient"`
}

// Deps groups everything the server needs.
type Deps struct {
	Auth      *auth.Authenticator
	Limiter   *auth.RateLimiter
	Databases Databases
	Admins    Admins
	Clients   Clients
	Audit     AuditReader
	Health    Pinger
	Notifier  Notifier
	Settings  Settings
	Static    fs.FS
	Logger    *slog.Logger
}

type server struct {
	Deps
}

// NewRouter builds the HTTP handler tree. Every protected route declares
// the minimum role it needs: viewer (read-only), admin (operate databases)
// or owner (also manage administrators).
func NewRouter(d Deps) http.Handler {
	s := &server{Deps: d}
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.Recoverer, securityHeaders)
	r.Get(config.RouteHealth, s.health)
	r.Route(config.RouteAPI, func(api chi.Router) {
		api.Use(auth.RequireCSRFHeader)
		api.With(d.Limiter.Middleware).Post(config.RouteLogin, s.login)
		api.With(d.Limiter.Middleware).Post(config.RouteRegister, s.register)
		api.Get(config.RoutePublicConfig, s.publicConfig)
		api.Post(config.RouteLogout, s.logout)
		api.Group(func(p chi.Router) {
			p.Use(d.Auth.Middleware)
			p.Get(config.RouteSession, s.session)
			s.viewerRoutes(p.With(auth.RequireRole(config.RoleViewer)))
			s.adminRoutes(p.With(auth.RequireRole(config.RoleAdmin)))
			s.ownerRoutes(p.With(auth.RequireRole(config.RoleOwner)))
			s.clientRoutes(p.With(auth.RequireClient))
		})
	})
	r.NotFound(spaHandler(d.Static))
	return r
}

func (s *server) viewerRoutes(r chi.Router) {
	r.Post(config.RouteAuthPassword, s.changeOwnPassword)
	r.Get(config.RouteSettings, s.settings)
	r.Get(config.RouteDatabases, s.listDatabases)
	r.Get(config.RouteDatabase, s.getDatabase)
	r.Get(config.RouteConnection, s.connection)
	r.Get(config.RouteStats, s.stats)
	r.Get(config.RouteLiveConnections, s.liveConnections)
	r.Get(config.RouteSchema, s.schema)
	r.Get(config.RouteTableCount, s.countRows)
	r.Get(config.RouteDBUsers, s.listUsers)
	r.Get(config.RouteDBJobs, s.listJobs)
	r.Get(config.RouteDBBackups, s.listDatabaseBackups)
	r.Get(config.RouteBackups, s.listBackups)
	r.Get(config.RouteTemplates, s.listTemplates)
	r.Get(config.RouteAuditLog, s.listAudit)
}

func (s *server) adminRoutes(r chi.Router) {
	r.Post(config.RouteDatabases, s.createDatabase)
	r.Delete(config.RouteDatabase, s.deleteDatabase)
	r.Post(config.RouteDBPassword, s.resetPassword)
	r.Get(config.RouteTableRows, s.previewRows)
	r.Post(config.RouteQuery, s.query)
	r.Get(config.RouteTableData, s.browse)
	r.Post(config.RouteTableData, s.insertRow)
	r.Patch(config.RouteTableData, s.updateRow)
	r.Delete(config.RouteTableData, s.deleteRow)
	r.Post(config.RouteDBUsers, s.createUser)
	r.Delete(config.RouteDBUser, s.deleteUser)
	r.Post(config.RouteDBUserPass, s.resetUserPassword)
	r.Post(config.RouteDBBackups, s.backup)
	r.Get(config.RouteBackupFile, s.downloadBackup)
	r.Delete(config.RouteBackup, s.deleteBackup)
	r.Post(config.RouteBackupRest, s.restoreBackup)
	r.Post(config.RouteTemplates, s.createTemplate)
	r.Put(config.RouteTemplate, s.updateTemplate)
	r.Delete(config.RouteTemplate, s.deleteTemplate)
	r.Post(config.RouteNotifyTest, s.testNotification)
}

func (s *server) ownerRoutes(r chi.Router) {
	r.Get(config.RouteAdmins, s.listAdmins)
	r.Post(config.RouteAdmins, s.createAdmin)
	r.Patch(config.RouteAdmin, s.updateAdmin)
	r.Post(config.RouteAdminPass, s.resetAdminPassword)
	r.Delete(config.RouteAdmin, s.deleteAdmin)
	// Inactivity policy: only the operator running docker compose (the
	// panel owner) sets the global default and per-database exceptions.
	r.Put(config.RouteExpiry, s.setExpiry)
	r.Post(config.RouteExtend, s.extend)
	r.Get(config.RouteGlobalExpiry, s.getGlobalExpiry)
	r.Put(config.RouteGlobalExpiry, s.setGlobalExpiry)
	// Usuarios: self-service tenants. Only the owner adds one directly,
	// deletes one, or moves its expiry exception.
	r.Get(config.RouteClients, s.listClients)
	r.Post(config.RouteClients, s.createClient)
	r.Delete(config.RouteClient, s.deleteClient)
	r.Put(config.RouteClientExpiry, s.setClientExpiry)
}

// clientRoutes is the reduced "panel de usuario": a self-service client can
// only ever see and change their own databases. Every handler re-checks
// ownership server-side (see ownDatabase) — slug guessing never reaches
// another client's data.
func (s *server) clientRoutes(r chi.Router) {
	r.Post(config.RouteMyAccountPassword, s.changeOwnClientPassword)
	r.Get(config.RouteMyDatabases, s.listMyDatabases)
	r.Post(config.RouteMyDatabases, s.createMyDatabase)
	r.Get(config.RouteMyDatabase, s.getMyDatabase)
	r.Delete(config.RouteMyDatabase, s.deleteMyDatabase)
	r.Get(config.RouteMyConnection, s.myConnection)
	r.Get(config.RouteMyStats, s.myStats)
	r.Get(config.RouteMyLiveConnections, s.myLiveConnections)
	r.Get(config.RouteMyTemplates, s.myTemplates)
	r.Get(config.RouteMySchema, s.mySchema)
	r.Post(config.RouteMyDBPassword, s.resetMyPassword)
	r.Get(config.RouteMyTableData, s.browseMyTable)
	r.Post(config.RouteMyTableData, s.insertMyRow)
	r.Patch(config.RouteMyTableData, s.updateMyRow)
	r.Delete(config.RouteMyTableData, s.deleteMyRow)
	r.Get(config.RouteMyTableCount, s.countMyRows)
}

// securityHeaders hardens every response. Scripts and styles must come from
// our own origin; inline style *attributes* are allowed because the schema
// diagram positions nodes with them (no inline <script> or <style> blocks).
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; style-src-attr 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

func (s *server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if err := s.Health.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *server) settings(w http.ResponseWriter, r *http.Request) {
	st := s.Settings
	st.NotifyChannels = s.Notifier.Channels()
	// The owner can change the inactivity policy at runtime (Ajustes ->
	// Expiración); reflect that override here instead of the .env value
	// baked in at startup.
	if override, err := s.Databases.GlobalSettings(r.Context()); err == nil {
		if override.ExpirationDays != nil {
			st.ExpirationDays = *override.ExpirationDays
		}
		if override.ExpiringSoonDays != nil {
			st.ExpiringSoonDays = *override.ExpiringSoonDays
		}
	}
	writeJSON(w, http.StatusOK, st)
}

// publicConfig is the only unauthenticated config: the login screen needs to
// know whether to offer "Crear cuenta".
func (s *server) publicConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"allowSelfRegister": s.Settings.AllowSelfRegister})
}

func (s *server) listAudit(w http.ResponseWriter, r *http.Request) {
	entries, err := s.Audit.ListAudit(r.Context(), config.AuditLogPageSize)
	s.respond(w, "list audit", map[string]any{"entries": entries}, err)
}

func (s *server) testNotification(w http.ResponseWriter, r *http.Request) {
	err := s.Notifier.Send(r.Context(), notify.Message{
		Event: "test", Title: "Prueba de notificaciones",
		Text: "Si ves este mensaje, PosternDB puede avisarte antes de que una base expire.",
	})
	switch {
	case errors.Is(err, notify.ErrNoChannels):
		writeError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		writeError(w, http.StatusBadGateway, "no se pudo enviar: "+err.Error())
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// ---- helpers -----------------------------------------------------------------

func actor(r *http.Request) string {
	return auth.Subject(r.Context())
}

// respond writes v as JSON, or maps err to a status code.
func (s *server) respond(w http.ResponseWriter, op string, v any, err error) {
	s.respondStatus(w, http.StatusOK, op, v, err)
}

func (s *server) respondStatus(w http.ResponseWriter, status int, op string, v any, err error) {
	if err != nil {
		s.domainError(w, op, err)
		return
	}
	writeJSON(w, status, v)
}

func (s *server) noContent(w http.ResponseWriter, op string, err error) {
	if err != nil {
		s.domainError(w, op, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// secret answers with credentials that must never be cached.
func (s *server) secret(w http.ResponseWriter, status int, op string, v any, err error) {
	w.Header().Set("Cache-Control", "no-store")
	s.respondStatus(w, status, op, v, err)
}

func (s *server) domainError(w http.ResponseWriter, op string, err error) {
	var invalidInput *provision.InvalidInputError
	switch {
	case errors.As(err, &invalidInput):
		writeError(w, http.StatusBadRequest, invalidInput.Error())
	case isAdminValidation(err), isClientValidation(err):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, model.ErrConflict):
		writeError(w, http.StatusConflict, "ya existe un elemento con ese nombre")
	case errors.Is(err, model.ErrNotFound):
		writeError(w, http.StatusNotFound, "no encontrado")
	default:
		s.internalError(w, op, err)
	}
}

func isClientValidation(err error) bool {
	for _, e := range []error{clients.ErrUsername, clients.ErrWeakPassword, clients.ErrWrongPassword, clients.ErrRegisterDisabled} {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
}

func isAdminValidation(err error) bool {
	for _, e := range []error{admins.ErrUsername, admins.ErrRole, admins.ErrBootstrap, admins.ErrSelf,
		admins.ErrWeakPassword, admins.ErrWrongPassword} {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
}

// internalError logs the detail and returns a generic message: internal
// errors may contain infrastructure details that must not reach the browser.
func (s *server) internalError(w http.ResponseWriter, op string, err error) {
	s.Logger.Error("request failed", "op", op, "error", err)
	writeError(w, http.StatusInternalServerError, "error interno; revisa los logs del servidor")
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if !strings.HasPrefix(r.Header.Get(config.HeaderContentType), config.MimeJSON) {
		writeError(w, http.StatusUnsupportedMediaType, "se esperaba application/json")
		return false
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, config.MaxJSONBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "JSON inválido")
		return false
	}
	return true
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, config.URLParamID), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "id inválido")
		return 0, false
	}
	return id, true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set(config.HeaderContentType, config.MimeJSON)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// isHTTPS detects TLS terminated at Cloudflare. Requests through the local
// loopback port are plain HTTP and get a non-Secure cookie so local access
// keeps working.
func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") ||
		strings.Contains(r.Header.Get("Cf-Visitor"), `"https"`)
}
