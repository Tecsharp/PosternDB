package provision

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tecsharp/posterndb/internal/config"
	"github.com/tecsharp/posterndb/internal/model"
	"github.com/tecsharp/posterndb/internal/naming"
)

const (
	maxTemplateName   = 80
	maxExtendDays     = 3650
	auditSQLMaxLength = 300
)

// ---- passwords ---------------------------------------------------------------

// ResetPassword gives the owner role a new password and returns it once.
// Data is untouched; existing sessions keep working until they reconnect.
func (s *Service) ResetPassword(ctx context.Context, slug, actor string) (model.Credentials, error) {
	db, err := s.active(ctx, slug)
	if err != nil {
		return model.Credentials{}, err
	}
	password, verifier, err := s.newSecret()
	if err != nil {
		return model.Credentials{}, err
	}
	if err := s.cluster.SetPassword(ctx, db.RoleName, verifier); err != nil {
		return model.Credentials{}, err
	}
	s.recordAudit(ctx, actor, config.ActionPasswordReset, db.Slug, map[string]any{"username": db.RoleName})
	return s.credentialsFor(db, db.RoleName, password), nil
}

// ---- additional users ----------------------------------------------------------

// ListUsers returns the additional users of a database.
func (s *Service) ListUsers(ctx context.Context, slug string) ([]model.DatabaseUser, error) {
	db, err := s.lookup(ctx, slug)
	if err != nil {
		return nil, err
	}
	return s.meta.ListUsers(ctx, db.ID)
}

// CreateUser adds a login role with read or write access to every table.
func (s *Service) CreateUser(ctx context.Context, slug, name, access, actor string) (model.Credentials, error) {
	db, err := s.active(ctx, slug)
	if err != nil {
		return model.Credentials{}, err
	}
	name = naming.Normalize(name)
	if err := naming.ValidateUserName(name); err != nil {
		return model.Credentials{}, &InvalidInputError{Err: err}
	}
	if access != config.AccessRead && access != config.AccessWrite {
		return model.Credentials{}, invalid("el acceso debe ser lectura o escritura")
	}
	if _, err := s.meta.GetUser(ctx, db.ID, name); err == nil {
		return model.Credentials{}, fmt.Errorf("user %q: %w", name, model.ErrConflict)
	}
	role := naming.ExtraRoleName(db.DBName, name)
	password, verifier, err := s.newSecret()
	if err != nil {
		return model.Credentials{}, err
	}
	if err := s.cluster.CreateUserRole(ctx, role, db.DBName, verifier); err != nil {
		return model.Credentials{}, fmt.Errorf("create user role: %w", err)
	}
	if err := s.grantAndRecord(ctx, db, name, role, access); err != nil {
		_ = s.inspector.DropUserObjects(context.WithoutCancel(ctx), db.DBName, db.RoleName, role)
		_ = s.cluster.DropRole(context.WithoutCancel(ctx), role)
		return model.Credentials{}, err
	}
	s.recordAudit(ctx, actor, config.ActionUserCreated, db.Slug, map[string]any{"username": role, "access": access})
	return s.credentialsFor(db, role, password), nil
}

func (s *Service) grantAndRecord(ctx context.Context, db model.ClientDatabase, name, role, access string) error {
	if err := s.inspector.GrantUser(ctx, db.DBName, db.RoleName, role, access); err != nil {
		return fmt.Errorf("grant privileges: %w", err)
	}
	_, err := s.meta.InsertUser(ctx, model.DatabaseUser{DatabaseID: db.ID, Name: name, RoleName: role, Access: access})
	return err
}

// ResetUserPassword gives an additional user a new password.
func (s *Service) ResetUserPassword(ctx context.Context, slug, name, actor string) (model.Credentials, error) {
	db, u, err := s.user(ctx, slug, name)
	if err != nil {
		return model.Credentials{}, err
	}
	password, verifier, err := s.newSecret()
	if err != nil {
		return model.Credentials{}, err
	}
	if err := s.cluster.SetPassword(ctx, u.RoleName, verifier); err != nil {
		return model.Credentials{}, err
	}
	s.recordAudit(ctx, actor, config.ActionPasswordReset, db.Slug, map[string]any{"username": u.RoleName})
	return s.credentialsFor(db, u.RoleName, password), nil
}

// DeleteUser revokes and drops an additional user.
func (s *Service) DeleteUser(ctx context.Context, slug, name, actor string) error {
	db, u, err := s.user(ctx, slug, name)
	if err != nil {
		return err
	}
	if err := s.inspector.DropUserObjects(ctx, db.DBName, db.RoleName, u.RoleName); err != nil {
		return fmt.Errorf("revoke privileges: %w", err)
	}
	if err := s.removeUserRecord(ctx, u); err != nil {
		return err
	}
	s.recordAudit(ctx, actor, config.ActionUserDeleted, db.Slug, map[string]any{"username": u.RoleName})
	return nil
}

func (s *Service) user(ctx context.Context, slug, name string) (model.ClientDatabase, model.DatabaseUser, error) {
	db, err := s.active(ctx, slug)
	if err != nil {
		return db, model.DatabaseUser{}, err
	}
	if err := naming.ValidateUserName(name); err != nil {
		return db, model.DatabaseUser{}, &InvalidInputError{Err: err}
	}
	u, err := s.meta.GetUser(ctx, db.ID, name)
	return db, u, err
}

// ---- expiry control -------------------------------------------------------------

// ExpiryRequest updates the per-database inactivity policy.
type ExpiryRequest struct {
	NeverExpires bool `json:"neverExpires"`
	// MaxIdleDays overrides DB_EXPIRATION_DAYS; nil restores the default.
	MaxIdleDays *int `json:"maxIdleDays"`
}

// SetExpiry stores the policy of one database.
func (s *Service) SetExpiry(ctx context.Context, slug string, req ExpiryRequest, actor string) (DatabaseView, error) {
	db, err := s.active(ctx, slug)
	if err != nil {
		return DatabaseView{}, err
	}
	if req.MaxIdleDays != nil && (*req.MaxIdleDays < 1 || *req.MaxIdleDays > maxExtendDays) {
		return DatabaseView{}, invalid("los días deben estar entre 1 y %d", maxExtendDays)
	}
	if err := s.meta.SetExpiry(ctx, db.ID, req.NeverExpires, req.MaxIdleDays, db.GraceUntil); err != nil {
		return DatabaseView{}, err
	}
	s.recordAudit(ctx, actor, config.ActionExpiryChanged, db.Slug, map[string]any{
		"neverExpires": req.NeverExpires, "maxIdleDays": req.MaxIdleDays,
	})
	return s.Get(ctx, slug)
}

// Extend pushes the expiry date days into the future (from the current
// deadline, or from now if that already passed).
func (s *Service) Extend(ctx context.Context, slug string, days int, actor string) (DatabaseView, error) {
	db, err := s.active(ctx, slug)
	if err != nil {
		return DatabaseView{}, err
	}
	if days < 1 || days > maxExtendDays {
		return DatabaseView{}, invalid("los días deben estar entre 1 y %d", maxExtendDays)
	}
	from := s.opts.Now()
	if at, ok := s.opts.Policy.ExpiresAt(db); ok && at.After(from) {
		from = at
	}
	grace := from.Add(time.Duration(days) * 24 * time.Hour)
	if err := s.meta.SetExpiry(ctx, db.ID, db.NeverExpires, db.MaxIdleDays, &grace); err != nil {
		return DatabaseView{}, err
	}
	s.recordAudit(ctx, actor, config.ActionExpiryChanged, db.Slug, map[string]any{"extendedDays": days, "graceUntil": grace})
	return s.Get(ctx, slug)
}

// ---- metrics, schema and console ---------------------------------------------------

// Stats returns size and usage metrics of a live database.
func (s *Service) Stats(ctx context.Context, slug string) (model.DatabaseStats, error) {
	db, err := s.active(ctx, slug)
	if err != nil {
		return model.DatabaseStats{}, err
	}
	st, err := s.cluster.DatabaseStats(ctx, db.DBName)
	if err != nil {
		return st, err
	}
	st.TableCount, st.TotalRows, st.TopTables, err = s.inspector.TableSummary(ctx, db.DBName)
	return st, err
}

// Connections lists the sessions open right now on a live database.
func (s *Service) Connections(ctx context.Context, slug string) ([]model.Connection, error) {
	db, err := s.active(ctx, slug)
	if err != nil {
		return nil, err
	}
	return s.cluster.DatabaseConnections(ctx, db.DBName)
}

// Schema returns the structure of a live database.
func (s *Service) Schema(ctx context.Context, slug string) (model.Schema, error) {
	db, err := s.active(ctx, slug)
	if err != nil {
		return model.Schema{}, err
	}
	return s.inspector.Schema(ctx, db.DBName)
}

// Query runs a read-only console statement and records it in the audit log.
func (s *Service) Query(ctx context.Context, slug, sql, actor string) (model.QueryResult, error) {
	db, err := s.active(ctx, slug)
	if err != nil {
		return model.QueryResult{}, err
	}
	res, err := s.inspector.Query(ctx, db.DBName, db.RoleName, sql)
	s.recordAudit(ctx, actor, config.ActionQueryExecuted, db.Slug, map[string]any{
		"sql": truncate(strings.TrimSpace(sql), auditSQLMaxLength), "ok": err == nil,
	})
	if err != nil {
		return res, &InvalidInputError{Err: err}
	}
	return res, nil
}

// Preview returns the first rows of a table.
func (s *Service) Preview(ctx context.Context, slug, schema, table string) (model.QueryResult, error) {
	db, err := s.active(ctx, slug)
	if err != nil {
		return model.QueryResult{}, err
	}
	res, err := s.inspector.Preview(ctx, db.DBName, db.RoleName, schema, table)
	if err != nil {
		return res, &InvalidInputError{Err: err}
	}
	return res, nil
}

// CountRows returns the exact row count of a table.
func (s *Service) CountRows(ctx context.Context, slug, schema, table string) (int64, error) {
	db, err := s.active(ctx, slug)
	if err != nil {
		return 0, err
	}
	n, err := s.inspector.CountRows(ctx, db.DBName, db.RoleName, schema, table)
	if err != nil {
		return 0, &InvalidInputError{Err: err}
	}
	return n, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ---- backups -------------------------------------------------------------------------

// Backup queues a manual dump of a live database.
func (s *Service) Backup(ctx context.Context, slug, actor string) (model.Job, error) {
	db, err := s.active(ctx, slug)
	if err != nil {
		return model.Job{}, err
	}
	job, err := s.meta.EnqueueJob(ctx, model.Job{
		Kind: config.JobDump, Slug: db.Slug, SourceDB: db.DBName, Reason: config.ReasonManual, RequestedBy: actor,
		File: db.DBName + "_" + s.timestamp() + config.DumpExtension,
	})
	if err != nil {
		return job, err
	}
	s.recordAudit(ctx, actor, config.ActionBackupRequested, db.Slug, map[string]any{"file": job.File})
	return job, nil
}

// ListBackups lists available dumps (slug "" = all databases).
func (s *Service) ListBackups(ctx context.Context, slug string) ([]model.Job, error) {
	return s.meta.ListBackups(ctx, slug)
}

// ListJobs lists recent sidecar jobs of a database.
func (s *Service) ListJobs(ctx context.Context, slug string) ([]model.Job, error) {
	if err := naming.ValidateSlug(slug); err != nil {
		return nil, &InvalidInputError{Err: err}
	}
	return s.meta.ListJobs(ctx, slug)
}

// backup returns a finished dump that still exists.
func (s *Service) backup(ctx context.Context, id int64) (model.Job, error) {
	j, err := s.meta.GetJob(ctx, id)
	if errors.Is(err, model.ErrNotFound) || (err == nil && !isAvailableBackup(j)) {
		return model.Job{}, invalid("el respaldo no existe o todavía no termina")
	}
	return j, err
}

func isAvailableBackup(j model.Job) bool {
	return (j.Kind == config.JobDump || j.Kind == config.JobArchive) && j.Status == config.JobDone
}

// BackupPath returns the on-disk path of a backup for download. The file
// name comes from our own job row and is re-checked to be a plain name, so
// a crafted value can never escape the backups directory.
func (s *Service) BackupPath(ctx context.Context, id int64) (string, model.Job, error) {
	j, err := s.backup(ctx, id)
	if err != nil {
		return "", j, err
	}
	if j.File == "" || filepath.Base(j.File) != j.File || strings.HasPrefix(j.File, ".") {
		return "", j, invalid("nombre de respaldo inválido")
	}
	return filepath.Join(s.opts.BackupDir, j.File), j, nil
}

// DeleteBackup queues removal of a dump file.
func (s *Service) DeleteBackup(ctx context.Context, id int64, actor string) error {
	j, err := s.backup(ctx, id)
	if err != nil {
		return err
	}
	if _, err := s.meta.EnqueueJob(ctx, model.Job{Kind: config.JobDeleteFile, Slug: j.Slug, File: j.File, RequestedBy: actor}); err != nil {
		return err
	}
	s.recordAudit(ctx, actor, config.ActionBackupDeleted, j.Slug, map[string]any{"file": j.File})
	return nil
}

// ---- templates -----------------------------------------------------------------------

// ListTemplates returns every template.
func (s *Service) ListTemplates(ctx context.Context) ([]model.Template, error) {
	return s.meta.ListTemplates(ctx)
}

// ClientBackups lists the backups of every database a registered user owns,
// newest first.
func (s *Service) ClientBackups(ctx context.Context, clientUserID int64) ([]model.Job, error) {
	dbs, err := s.meta.ListByClientUser(ctx, clientUserID)
	if err != nil {
		return nil, err
	}
	all := []model.Job{}
	for _, d := range dbs {
		jobs, err := s.meta.ListBackups(ctx, d.Slug)
		if err != nil {
			return nil, err
		}
		all = append(all, jobs...)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].CreatedAt.After(all[j].CreatedAt) })
	return all, nil
}

// SharedTemplates returns the templates offered to self-service users.
func (s *Service) SharedTemplates(ctx context.Context) ([]model.Template, error) {
	all, err := s.meta.ListTemplates(ctx)
	if err != nil {
		return nil, err
	}
	shared := make([]model.Template, 0, len(all))
	for _, t := range all {
		if t.Shared {
			shared = append(shared, t)
		}
	}
	return shared, nil
}

// SaveTemplate creates or updates a template.
func (s *Service) SaveTemplate(ctx context.Context, t model.Template, actor string) (model.Template, error) {
	t.Name = strings.TrimSpace(t.Name)
	t.Description = strings.TrimSpace(t.Description)
	if t.Name == "" || len(t.Name) > maxTemplateName {
		return t, invalid("el nombre de la plantilla debe tener entre 1 y %d caracteres", maxTemplateName)
	}
	if strings.TrimSpace(t.SQL) == "" || len(t.SQL) > config.MaxTemplateSQL {
		return t, invalid("el SQL de la plantilla está vacío o es demasiado largo")
	}
	saved, err := s.meta.SaveTemplate(ctx, t)
	if err != nil {
		return saved, err
	}
	s.recordAudit(ctx, actor, config.ActionTemplateSaved, saved.Name, map[string]any{"id": saved.ID})
	return saved, nil
}

// DeleteTemplate removes a template.
func (s *Service) DeleteTemplate(ctx context.Context, id int64, actor string) error {
	t, err := s.meta.GetTemplate(ctx, id)
	if err != nil {
		return err
	}
	if err := s.meta.DeleteTemplate(ctx, id); err != nil {
		return err
	}
	s.recordAudit(ctx, actor, config.ActionTemplateDeleted, t.Name, map[string]any{"id": id})
	return nil
}
