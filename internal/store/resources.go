package store

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/tecsharp/posterndb/internal/config"
	"github.com/tecsharp/posterndb/internal/model"
)

// ---- additional database users ---------------------------------------------

const userColumns = `id, database_id, name, role_name, access, created_at`

// ListUsers returns the additional users of a database.
func (m *Metadata) ListUsers(ctx context.Context, databaseID int64) ([]model.DatabaseUser, error) {
	rows, err := m.pool.Query(ctx, `SELECT `+userColumns+` FROM database_users WHERE database_id = $1 ORDER BY name`, databaseID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.DatabaseUser, error) { return scanUser(r) })
}

// GetUser finds one additional user.
func (m *Metadata) GetUser(ctx context.Context, databaseID int64, name string) (model.DatabaseUser, error) {
	return scanUser(m.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM database_users
		WHERE database_id = $1 AND name = $2`, databaseID, name))
}

// InsertUser records an additional user.
func (m *Metadata) InsertUser(ctx context.Context, u model.DatabaseUser) (model.DatabaseUser, error) {
	return scanUser(m.pool.QueryRow(ctx, `INSERT INTO database_users (database_id, name, role_name, access)
		VALUES ($1, $2, $3, $4) RETURNING `+userColumns, u.DatabaseID, u.Name, u.RoleName, u.Access))
}

// DeleteUser removes an additional user record.
func (m *Metadata) DeleteUser(ctx context.Context, id int64) error {
	return m.exec(ctx, `DELETE FROM database_users WHERE id = $1`, id)
}

func scanUser(row pgx.Row) (model.DatabaseUser, error) {
	var u model.DatabaseUser
	err := row.Scan(&u.ID, &u.DatabaseID, &u.Name, &u.RoleName, &u.Access, &u.CreatedAt)
	return u, translate(err)
}

// ---- backup sidecar jobs ----------------------------------------------------

const jobColumns = `id, kind, status, slug, source_db, target_db, target_role, file, bytes, reason, error,
	requested_by, created_at, finished_at`

// EnqueueJob adds work for the backup sidecar.
func (m *Metadata) EnqueueJob(ctx context.Context, j model.Job) (model.Job, error) {
	return scanJob(m.pool.QueryRow(ctx, `INSERT INTO jobs (kind, slug, source_db, target_db, target_role, file, reason, requested_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING `+jobColumns,
		j.Kind, j.Slug, j.SourceDB, j.TargetDB, j.TargetRole, j.File, j.Reason, j.RequestedBy))
}

// GetJob finds one job.
func (m *Metadata) GetJob(ctx context.Context, id int64) (model.Job, error) {
	return scanJob(m.pool.QueryRow(ctx, `SELECT `+jobColumns+` FROM jobs WHERE id = $1`, id))
}

// ListBackups returns finished dump/archive jobs whose file still exists
// (no later successful delete_file job for it), newest first. An empty slug
// lists backups of every database.
func (m *Metadata) ListBackups(ctx context.Context, slug string) ([]model.Job, error) {
	return m.queryJobs(ctx, `SELECT `+jobColumns+` FROM jobs j
		WHERE kind IN ($1, $2) AND status = $3 AND ($4 = '' OR slug = $4)
		  AND NOT EXISTS (SELECT 1 FROM jobs d WHERE d.kind = $5 AND d.file = j.file AND d.status <> $6)
		ORDER BY created_at DESC LIMIT 200`,
		config.JobDump, config.JobArchive, config.JobDone, slug, config.JobDeleteFile, config.JobFailed)
}

// ListJobs returns recent jobs of a database (any kind and state).
func (m *Metadata) ListJobs(ctx context.Context, slug string) ([]model.Job, error) {
	return m.queryJobs(ctx, `SELECT `+jobColumns+` FROM jobs WHERE slug = $1 ORDER BY created_at DESC LIMIT 50`, slug)
}

func (m *Metadata) queryJobs(ctx context.Context, sql string, args ...any) ([]model.Job, error) {
	rows, err := m.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.Job, error) { return scanJob(r) })
}

func scanJob(row pgx.Row) (model.Job, error) {
	var j model.Job
	err := row.Scan(&j.ID, &j.Kind, &j.Status, &j.Slug, &j.SourceDB, &j.TargetDB, &j.TargetRole, &j.File,
		&j.Bytes, &j.Reason, &j.Error, &j.RequestedBy, &j.CreatedAt, &j.FinishedAt)
	return j, translate(err)
}

// ---- templates --------------------------------------------------------------

const templateColumns = `id, name, description, sql, shared, updated_at`

// ListTemplates returns every template.
func (m *Metadata) ListTemplates(ctx context.Context) ([]model.Template, error) {
	rows, err := m.pool.Query(ctx, `SELECT `+templateColumns+` FROM templates ORDER BY name`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.Template, error) { return scanTemplate(r) })
}

// GetTemplate finds one template.
func (m *Metadata) GetTemplate(ctx context.Context, id int64) (model.Template, error) {
	return scanTemplate(m.pool.QueryRow(ctx, `SELECT `+templateColumns+` FROM templates WHERE id = $1`, id))
}

// SaveTemplate inserts (ID == 0) or updates a template.
func (m *Metadata) SaveTemplate(ctx context.Context, t model.Template) (model.Template, error) {
	if t.ID == 0 {
		return scanTemplate(m.pool.QueryRow(ctx, `INSERT INTO templates (name, description, sql, shared)
			VALUES ($1, $2, $3, $4) RETURNING `+templateColumns, t.Name, t.Description, t.SQL, t.Shared))
	}
	return scanTemplate(m.pool.QueryRow(ctx, `UPDATE templates SET name = $2, description = $3, sql = $4,
		shared = $5, updated_at = now() WHERE id = $1 RETURNING `+templateColumns, t.ID, t.Name, t.Description, t.SQL, t.Shared))
}

// DeleteTemplate removes a template.
func (m *Metadata) DeleteTemplate(ctx context.Context, id int64) error {
	return m.exec(ctx, `DELETE FROM templates WHERE id = $1`, id)
}

func scanTemplate(row pgx.Row) (model.Template, error) {
	var t model.Template
	err := row.Scan(&t.ID, &t.Name, &t.Description, &t.SQL, &t.Shared, &t.UpdatedAt)
	return t, translate(err)
}
