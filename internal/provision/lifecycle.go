package provision

import (
	"context"
	"errors"
	"fmt"

	"github.com/tecsharp/posterndb/internal/config"
	"github.com/tecsharp/posterndb/internal/model"
	"github.com/tecsharp/posterndb/internal/naming"
)

// CreateRequest describes a new database and, optionally, where its
// initial content comes from. At most one source may be set.
type CreateRequest struct {
	Name            string `json:"name"`
	TemplateID      int64  `json:"templateId,omitempty"`
	CloneFrom       string `json:"cloneFrom,omitempty"`
	RestoreBackupID int64  `json:"restoreBackupId,omitempty"`
	// ClientUserID, when set, makes the new database belong to that
	// self-service client (see model.ClientUser) rather than being an
	// admin-panel database. Never read from the request body: the HTTP
	// layer sets it from the authenticated identity.
	ClientUserID *int64 `json:"-"`
}

func (r CreateRequest) sources() int {
	n := 0
	for _, set := range []bool{r.TemplateID != 0, r.CloneFrom != "", r.RestoreBackupID != 0} {
		if set {
			n++
		}
	}
	return n
}

// compensation undoes one completed provisioning step.
type compensation struct {
	name string
	undo func(context.Context) error
}

// provisioning carries the state of one Create call.
type provisioning struct {
	req      CreateRequest
	names    naming.ClientNames
	password string
	verifier string
	template *model.Template
	job      *model.Job // clone/restore job to enqueue once the DB exists
}

// Create provisions a client database end to end.
//
// CREATE DATABASE cannot run inside a transaction and Cloudflare is an
// external system, so atomicity is achieved with the saga pattern: each step
// registers a compensation and, if a later step fails, completed steps are
// undone in reverse order. The metadata row is written last, so the panel
// never lists a half-provisioned database.
func (s *Service) Create(ctx context.Context, req CreateRequest, actor string) (model.Credentials, error) {
	p, err := s.prepare(ctx, req, actor)
	if err != nil {
		return model.Credentials{}, err
	}
	var done []compensation
	steps := []func(context.Context, *provisioning) (*compensation, error){
		s.createRole, s.createDatabase, s.grantConsole, s.applyTemplate, s.createTunnelEndpoint, s.enqueueContent,
	}
	for _, step := range steps {
		comp, err := step(ctx, p)
		if err != nil {
			s.rollback(ctx, p.names, done, err)
			return model.Credentials{}, err
		}
		if comp != nil {
			done = append(done, *comp)
		}
	}
	db, err := s.recordDatabase(ctx, p.names, actor, p.req.ClientUserID)
	if err != nil {
		s.rollback(ctx, p.names, done, err)
		return model.Credentials{}, err
	}
	s.recordAudit(ctx, actor, config.ActionDBCreated, p.names.Slug, map[string]any{
		"database": p.names.Database, "hostname": p.names.Hostname, "source": sourceLabel(req),
	})
	return s.credentialsFor(db, db.RoleName, p.password), nil
}

func sourceLabel(r CreateRequest) string {
	switch {
	case r.TemplateID != 0:
		return fmt.Sprintf("template:%d", r.TemplateID)
	case r.CloneFrom != "":
		return "clone:" + r.CloneFrom
	case r.RestoreBackupID != 0:
		return fmt.Sprintf("backup:%d", r.RestoreBackupID)
	default:
		return "empty"
	}
}

// prepare validates everything that can be checked before touching the
// cluster, so most mistakes fail fast with nothing to roll back.
func (s *Service) prepare(ctx context.Context, req CreateRequest, actor string) (*provisioning, error) {
	names, err := s.opts.Resolver.Resolve(naming.Normalize(req.Name))
	if err != nil {
		return nil, &InvalidInputError{Err: err}
	}
	if req.sources() > 1 {
		return nil, invalid("elige solo un origen: plantilla, clonar o restaurar")
	}
	if err := s.ensureSlugFree(ctx, names.Slug); err != nil {
		return nil, err
	}
	p := &provisioning{req: req, names: names}
	if p.password, p.verifier, err = s.newSecret(); err != nil {
		return nil, err
	}
	if err := s.resolveSource(ctx, p, actor); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Service) resolveSource(ctx context.Context, p *provisioning, actor string) error {
	switch {
	case p.req.TemplateID != 0:
		t, err := s.meta.GetTemplate(ctx, p.req.TemplateID)
		if err != nil {
			return notFoundAsInvalid(err, "la plantilla no existe")
		}
		p.template = &t
	case p.req.CloneFrom != "":
		src, err := s.active(ctx, naming.Normalize(p.req.CloneFrom))
		if err != nil {
			return notFoundAsInvalid(err, "la base a clonar no existe")
		}
		p.job = &model.Job{Kind: config.JobClone, SourceDB: src.DBName}
	case p.req.RestoreBackupID != 0:
		b, err := s.backup(ctx, p.req.RestoreBackupID)
		if err != nil {
			return err
		}
		p.job = &model.Job{Kind: config.JobRestore, File: b.File}
	default:
		return nil
	}
	if p.job != nil {
		p.job.Slug, p.job.TargetDB, p.job.TargetRole, p.job.RequestedBy = p.names.Slug, p.names.Database, p.names.Role, actor
	}
	return nil
}

func notFoundAsInvalid(err error, msg string) error {
	if errors.Is(err, model.ErrNotFound) {
		return invalid("%s", msg)
	}
	return err
}

func (s *Service) ensureSlugFree(ctx context.Context, slug string) error {
	_, err := s.meta.GetDatabase(ctx, slug)
	if err == nil {
		return fmt.Errorf("database %q: %w", slug, model.ErrConflict)
	}
	if errors.Is(err, model.ErrNotFound) {
		return nil
	}
	return err
}

func (s *Service) createRole(ctx context.Context, p *provisioning) (*compensation, error) {
	if err := s.cluster.CreateRole(ctx, p.names.Role, p.verifier); err != nil {
		return nil, fmt.Errorf("create role: %w", err)
	}
	return &compensation{name: "drop role", undo: func(ctx context.Context) error {
		return s.cluster.DropRole(ctx, p.names.Role)
	}}, nil
}

func (s *Service) createDatabase(ctx context.Context, p *provisioning) (*compensation, error) {
	if err := s.cluster.CreateDatabase(ctx, p.names.Database, p.names.Role); err != nil {
		return nil, fmt.Errorf("create database: %w", err)
	}
	return &compensation{name: "drop database", undo: func(ctx context.Context) error {
		return s.cluster.DropClient(ctx, p.names.Database, p.names.Role)
	}}, nil
}

func (s *Service) grantConsole(ctx context.Context, p *provisioning) (*compensation, error) {
	if err := s.cluster.GrantConsoleAccess(ctx, p.names.Database, p.names.Role); err != nil {
		return nil, fmt.Errorf("grant console access: %w", err)
	}
	return nil, nil
}

func (s *Service) applyTemplate(ctx context.Context, p *provisioning) (*compensation, error) {
	if p.template == nil {
		return nil, nil
	}
	if err := s.inspector.ApplySQL(ctx, p.names.Database, p.names.Role, p.template.SQL); err != nil {
		return nil, invalid("la plantilla %q falló: %v", p.template.Name, err)
	}
	return nil, nil
}

func (s *Service) createTunnelEndpoint(ctx context.Context, p *provisioning) (*compensation, error) {
	if p.names.Hostname == "" {
		return nil, nil
	}
	if err := s.router.EnsureRoute(ctx, p.names.Hostname, s.opts.TunnelDBService); err != nil {
		return nil, fmt.Errorf("publish hostname: %w", err)
	}
	return &compensation{name: "remove route", undo: func(ctx context.Context) error {
		return s.router.RemoveRoute(ctx, p.names.Hostname)
	}}, nil
}

// enqueueContent hands clone/restore to the backup sidecar. The database is
// usable immediately; its content appears when the job finishes.
func (s *Service) enqueueContent(ctx context.Context, p *provisioning) (*compensation, error) {
	if p.job == nil {
		return nil, nil
	}
	if _, err := s.meta.EnqueueJob(ctx, *p.job); err != nil {
		return nil, fmt.Errorf("queue %s: %w", p.job.Kind, err)
	}
	return nil, nil
}

func (s *Service) recordDatabase(ctx context.Context, n naming.ClientNames, actor string, clientUserID *int64) (model.ClientDatabase, error) {
	db, err := s.meta.InsertDatabase(ctx, model.ClientDatabase{
		Slug: n.Slug, DBName: n.Database, RoleName: n.Role, Hostname: n.Hostname, LastSeenAt: s.opts.Now(), ClientUserID: clientUserID,
	})
	if err != nil {
		return db, fmt.Errorf("record metadata: %w", err)
	}
	return db, nil
}

// rollback runs compensations in reverse order. It uses a context detached
// from the request so a client disconnect cannot leave orphaned resources.
func (s *Service) rollback(ctx context.Context, n naming.ClientNames, done []compensation, cause error) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), config.ExternalCallTimeout)
	defer cancel()
	failed := []string{}
	for i := len(done) - 1; i >= 0; i-- {
		if err := done[i].undo(cleanupCtx); err != nil {
			failed = append(failed, done[i].name)
			s.opts.Logger.Error("rollback step failed", "slug", n.Slug, "step", done[i].name, "error", err)
		}
	}
	s.recordAudit(cleanupCtx, config.ActorSystem, config.ActionProvisionFailed, n.Slug, map[string]any{
		"error": cause.Error(), "failedRollbackSteps": failed,
	})
}

// Delete removes a database requested from the panel. For an expired
// database the resources are already gone and only the row is hidden.
func (s *Service) Delete(ctx context.Context, slug, actor string) error {
	db, err := s.lookup(ctx, slug)
	if err != nil {
		return err
	}
	archivedAs := ""
	if db.State == config.StateActive {
		if archivedAs, err = s.retire(ctx, db, config.ReasonDeletion, actor); err != nil {
			return err
		}
	}
	if err := s.meta.MarkDeleted(ctx, db.ID, archivedAs, s.opts.Now()); err != nil {
		return err
	}
	s.recordAudit(ctx, actor, config.ActionDBDeleted, db.Slug, map[string]any{"archivedAs": archivedAs})
	return nil
}

// Expire retires a database that exceeded its inactivity limit.
func (s *Service) Expire(ctx context.Context, db model.ClientDatabase, actor string) error {
	archivedAs, err := s.retire(ctx, db, config.ReasonExpiry, actor)
	if err != nil {
		return err
	}
	if err := s.meta.MarkExpired(ctx, db.ID, archivedAs, s.opts.Now()); err != nil {
		return err
	}
	s.recordAudit(ctx, actor, config.ActionDBExpired, db.Slug, map[string]any{
		"lastSeenAt": db.LastSeenAt, "archivedAs": archivedAs,
	})
	return nil
}

// retire unpublishes the hostname first (so no new tunnel connections can
// arrive), removes additional users and then either archives the database
// for the backup sidecar or drops it, depending on BACKUP_BEFORE_DELETE.
func (s *Service) retire(ctx context.Context, db model.ClientDatabase, reason, actor string) (string, error) {
	if db.Hostname != "" {
		if err := s.router.RemoveRoute(ctx, db.Hostname); err != nil {
			return "", fmt.Errorf("unpublish hostname: %w", err)
		}
		s.recordAudit(ctx, actor, config.ActionRouteRemoved, db.Slug, map[string]any{"hostname": db.Hostname})
	}
	users, err := s.meta.ListUsers(ctx, db.ID)
	if err != nil {
		return "", err
	}
	if !s.opts.BackupBeforeDelete {
		return "", s.dropAll(ctx, db, users, actor)
	}
	return s.archive(ctx, db, users, reason, actor)
}

func (s *Service) dropAll(ctx context.Context, db model.ClientDatabase, users []model.DatabaseUser, actor string) error {
	if err := s.cluster.DropClient(ctx, db.DBName, db.RoleName); err != nil {
		return fmt.Errorf("drop database: %w", err)
	}
	// With the database gone, extra roles hold no privileges and can be dropped.
	for _, u := range users {
		if err := s.removeUserRecord(ctx, u); err != nil {
			return err
		}
	}
	s.recordAudit(ctx, actor, config.ActionDBDropped, db.Slug, map[string]any{"database": db.DBName})
	return nil
}

func (s *Service) archive(ctx context.Context, db model.ClientDatabase, users []model.DatabaseUser, reason, actor string) (string, error) {
	for _, u := range users {
		if err := s.inspector.DropUserObjects(ctx, db.DBName, db.RoleName, u.RoleName); err != nil {
			return "", fmt.Errorf("revoke user %s: %w", u.Name, err)
		}
		if err := s.removeUserRecord(ctx, u); err != nil {
			return "", err
		}
	}
	archive := naming.ArchiveName(db.Slug, s.opts.Now())
	if err := s.cluster.ArchiveClient(ctx, db.DBName, db.RoleName, archive); err != nil {
		return "", fmt.Errorf("archive database: %w", err)
	}
	job := model.Job{Kind: config.JobArchive, Slug: db.Slug, SourceDB: archive, File: archive + config.DumpExtension,
		Reason: reason, RequestedBy: actor}
	if _, err := s.meta.EnqueueJob(ctx, job); err != nil {
		// The sidecar also adopts orphaned pdbarch_* databases, so the
		// backup still happens; only the reason/slug labels are lost.
		s.opts.Logger.Error("queue archive job", "slug", db.Slug, "error", err)
	}
	s.recordAudit(ctx, actor, config.ActionDBArchived, db.Slug, map[string]any{"database": db.DBName, "archivedAs": archive})
	return archive, nil
}

func (s *Service) removeUserRecord(ctx context.Context, u model.DatabaseUser) error {
	if err := s.cluster.DropRole(ctx, u.RoleName); err != nil {
		return fmt.Errorf("drop user %s: %w", u.Name, err)
	}
	return s.meta.DeleteUser(ctx, u.ID)
}

// HostnameChange describes one database whose hostname differs from the
// scheme currently configured in .env.
type HostnameChange struct {
	Slug string `json:"slug"`
	From string `json:"from"`
	To   string `json:"to"`
}

// MigrateHostnames moves every active database to the hostname scheme in the
// current configuration (e.g. postern-acme.midominio.com -> acme.midominio.com).
// The new route is published before the old one is removed.
func (s *Service) MigrateHostnames(ctx context.Context, dryRun bool, actor string) ([]HostnameChange, error) {
	if s.opts.Resolver.Local {
		return nil, invalid("en modo local no hay hostnames que migrar")
	}
	active, err := s.meta.ListActive(ctx)
	if err != nil {
		return nil, err
	}
	var changes []HostnameChange
	for _, db := range active {
		target := s.opts.Resolver.Hostname(db.Slug)
		if target == db.Hostname {
			continue
		}
		change := HostnameChange{Slug: db.Slug, From: db.Hostname, To: target}
		changes = append(changes, change)
		if dryRun {
			continue
		}
		if err := s.migrateOne(ctx, db, change, actor); err != nil {
			return changes, fmt.Errorf("migrate %s: %w", db.Slug, err)
		}
	}
	return changes, nil
}

func (s *Service) migrateOne(ctx context.Context, db model.ClientDatabase, c HostnameChange, actor string) error {
	if _, err := s.opts.Resolver.Resolve(db.Slug); err != nil {
		return &InvalidInputError{Err: err}
	}
	if err := s.router.EnsureRoute(ctx, c.To, s.opts.TunnelDBService); err != nil {
		return err
	}
	if err := s.meta.UpdateHostname(ctx, db.ID, c.To); err != nil {
		return err
	}
	if c.From != "" {
		if err := s.router.RemoveRoute(ctx, c.From); err != nil {
			return err
		}
	}
	s.recordAudit(ctx, actor, config.ActionHostnameMigrated, db.Slug, map[string]any{"from": c.From, "to": c.To})
	return nil
}
