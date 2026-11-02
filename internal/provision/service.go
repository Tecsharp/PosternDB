// Package provision orchestrates the lifecycle of client databases across
// PostgreSQL, Cloudflare, the backup sidecar and the metadata store.
package provision

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/tecsharp/posterndb/internal/cloudflare"
	"github.com/tecsharp/posterndb/internal/config"
	"github.com/tecsharp/posterndb/internal/expiry"
	"github.com/tecsharp/posterndb/internal/model"
	"github.com/tecsharp/posterndb/internal/naming"
)

// Metadata is the subset of the metadata repository used here.
type Metadata interface {
	InsertDatabase(ctx context.Context, db model.ClientDatabase) (model.ClientDatabase, error)
	ListDatabases(ctx context.Context) ([]model.ClientDatabase, error)
	ListActive(ctx context.Context) ([]model.ClientDatabase, error)
	GetDatabase(ctx context.Context, slug string) (model.ClientDatabase, error)
	MarkExpired(ctx context.Context, id int64, archivedAs string, at time.Time) error
	MarkDeleted(ctx context.Context, id int64, archivedAs string, at time.Time) error
	UpdateHostname(ctx context.Context, id int64, hostname string) error
	SetExpiry(ctx context.Context, id int64, never bool, maxIdleDays *int, graceUntil *time.Time) error
	RecordAudit(ctx context.Context, e model.AuditEntry) error

	ListUsers(ctx context.Context, databaseID int64) ([]model.DatabaseUser, error)
	GetUser(ctx context.Context, databaseID int64, name string) (model.DatabaseUser, error)
	InsertUser(ctx context.Context, u model.DatabaseUser) (model.DatabaseUser, error)
	DeleteUser(ctx context.Context, id int64) error

	EnqueueJob(ctx context.Context, j model.Job) (model.Job, error)
	GetJob(ctx context.Context, id int64) (model.Job, error)
	ListBackups(ctx context.Context, slug string) ([]model.Job, error)
	ListJobs(ctx context.Context, slug string) ([]model.Job, error)

	ListTemplates(ctx context.Context) ([]model.Template, error)
	GetTemplate(ctx context.Context, id int64) (model.Template, error)
	SaveTemplate(ctx context.Context, t model.Template) (model.Template, error)
	DeleteTemplate(ctx context.Context, id int64) error
}

// Cluster is the subset of PostgreSQL server operations used here.
type Cluster interface {
	CreateRole(ctx context.Context, role, scramVerifier string) error
	DropRole(ctx context.Context, role string) error
	CreateDatabase(ctx context.Context, db, owner string) error
	DropClient(ctx context.Context, db, role string) error
	ArchiveClient(ctx context.Context, db, role, archiveName string) error
	GrantConsoleAccess(ctx context.Context, database, owner string) error
	SetPassword(ctx context.Context, role, scramVerifier string) error
	CreateUserRole(ctx context.Context, role, database, scramVerifier string) error
	DatabaseStats(ctx context.Context, database string) (model.DatabaseStats, error)
}

// Inspector reads and modifies the inside of client databases.
type Inspector interface {
	Schema(ctx context.Context, database string) (model.Schema, error)
	TableSummary(ctx context.Context, database string) (int, int64, []model.TableBrief, error)
	Query(ctx context.Context, database, owner, sql string) (model.QueryResult, error)
	Preview(ctx context.Context, database, owner, schema, table string) (model.QueryResult, error)
	CountRows(ctx context.Context, database, owner, schema, table string) (int64, error)
	ApplySQL(ctx context.Context, database, owner, sql string) error
	GrantUser(ctx context.Context, database, owner, user, access string) error
	DropUserObjects(ctx context.Context, database, owner, user string) error
}

// Secrets generates passwords and SCRAM verifiers.
type Secrets interface {
	Password(n int) (string, error)
	ScramVerifier(password string) (string, error)
}

// InvalidInputError wraps validation failures so the HTTP layer can answer 400.
type InvalidInputError struct{ Err error }

func (e *InvalidInputError) Error() string { return e.Err.Error() }
func (e *InvalidInputError) Unwrap() error { return e.Err }

func invalid(format string, args ...any) error {
	return &InvalidInputError{Err: fmt.Errorf(format, args...)}
}

// ErrNotActive is returned for operations that need a live database.
var ErrNotActive = errors.New("la base de datos ya no está activa")

// Options carries the configuration-derived settings of the Service.
type Options struct {
	Resolver           naming.Resolver
	Policy             expiry.Policy
	BackupBeforeDelete bool
	TunnelDBService    string
	ClientLocalPort    int
	LocalDBPort        int
	BackupDir          string
	Now                func() time.Time
	Logger             *slog.Logger
}

// Service is the provisioning orchestrator.
type Service struct {
	meta      Metadata
	cluster   Cluster
	router    cloudflare.Router
	inspector Inspector
	secrets   Secrets
	opts      Options
}

// New builds a Service.
func New(meta Metadata, cluster Cluster, router cloudflare.Router, inspector Inspector, secrets Secrets, opts Options) *Service {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Service{meta: meta, cluster: cluster, router: router, inspector: inspector, secrets: secrets, opts: opts}
}

// DatabaseView is a metadata row enriched with the computed status.
type DatabaseView struct {
	model.ClientDatabase
	Status    string     `json:"status"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

func (s *Service) view(db model.ClientDatabase) DatabaseView {
	v := DatabaseView{ClientDatabase: db, Status: s.opts.Policy.Status(db, s.opts.Now())}
	if at, ok := s.opts.Policy.ExpiresAt(db); ok {
		v.ExpiresAt = &at
	}
	return v
}

// List returns every non-deleted database with its current status.
func (s *Service) List(ctx context.Context) ([]DatabaseView, error) {
	dbs, err := s.meta.ListDatabases(ctx)
	if err != nil {
		return nil, err
	}
	views := make([]DatabaseView, 0, len(dbs))
	for _, db := range dbs {
		views = append(views, s.view(db))
	}
	return views, nil
}

// Get returns one database with its status.
func (s *Service) Get(ctx context.Context, slug string) (DatabaseView, error) {
	db, err := s.lookup(ctx, slug)
	if err != nil {
		return DatabaseView{}, err
	}
	return s.view(db), nil
}

// lookup validates the slug before touching the store.
func (s *Service) lookup(ctx context.Context, slug string) (model.ClientDatabase, error) {
	if err := naming.ValidateSlug(slug); err != nil {
		return model.ClientDatabase{}, &InvalidInputError{Err: err}
	}
	return s.meta.GetDatabase(ctx, slug)
}

// active is lookup restricted to live databases.
func (s *Service) active(ctx context.Context, slug string) (model.ClientDatabase, error) {
	db, err := s.lookup(ctx, slug)
	if err != nil {
		return db, err
	}
	if db.State != config.StateActive {
		return db, &InvalidInputError{Err: ErrNotActive}
	}
	return db, nil
}

// ConnectionInfo describes how clients reach a database.
func (s *Service) ConnectionInfo(db model.ClientDatabase) model.ConnectionInfo {
	info := model.ConnectionInfo{
		Hostname: db.Hostname, Database: db.DBName, Username: db.RoleName,
		Host: config.LocalHostname, Port: s.opts.ClientLocalPort, LocalMode: s.opts.Resolver.Local,
	}
	if info.LocalMode {
		info.Port = s.opts.LocalDBPort
	}
	return info
}

func (s *Service) credentialsFor(db model.ClientDatabase, username, password string) model.Credentials {
	info := s.ConnectionInfo(db)
	info.Username = username
	return model.Credentials{ConnectionInfo: info, Password: password}
}

// newSecret returns a password and its SCRAM verifier.
func (s *Service) newSecret() (string, string, error) {
	password, err := s.secrets.Password(config.GeneratedPasswordLn)
	if err != nil {
		return "", "", fmt.Errorf("generate password: %w", err)
	}
	verifier, err := s.secrets.ScramVerifier(password)
	if err != nil {
		return "", "", fmt.Errorf("hash password: %w", err)
	}
	return password, verifier, nil
}

// RecordAudit is exposed for other components (auth, auditor, admins).
func (s *Service) RecordAudit(ctx context.Context, actor, action, target string, details map[string]any) {
	s.recordAudit(ctx, actor, action, target, details)
}

// recordAudit never fails the caller: losing an audit line must not undo a
// completed operation, but it is logged loudly.
func (s *Service) recordAudit(ctx context.Context, actor, action, target string, details map[string]any) {
	err := s.meta.RecordAudit(ctx, model.AuditEntry{Actor: actor, Action: action, Target: target, Details: details})
	if err != nil {
		s.opts.Logger.Error("audit log write failed", "action", action, "target", target, "error", err)
	}
}

func (s *Service) timestamp() string {
	return s.opts.Now().UTC().Format(config.ArchiveTimeLayout)
}
