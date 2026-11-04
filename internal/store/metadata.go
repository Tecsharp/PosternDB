package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tecsharp/posterndb/internal/config"
	"github.com/tecsharp/posterndb/internal/model"
)

const clientColumns = `id, slug, db_name, role_name, hostname, state, created_at,
	last_seen_at, expired_at, COALESCE(archived_as, ''), sessions_baseline,
	internal_sessions, internal_baseline, never_expires, max_idle_days, grace_until, expiry_warned_at,
	client_user_id`

// Metadata is the repository for client_databases and audit_log.
type Metadata struct {
	pool *pgxpool.Pool
}

// NewMetadata returns a repository bound to the platform database.
func NewMetadata(pool *pgxpool.Pool) *Metadata {
	return &Metadata{pool: pool}
}

// InsertDatabase records a freshly provisioned database. last_seen_at starts
// at creation time so a new database gets the full grace period.
func (m *Metadata) InsertDatabase(ctx context.Context, db model.ClientDatabase) (model.ClientDatabase, error) {
	row := m.pool.QueryRow(ctx, `INSERT INTO client_databases
		(slug, db_name, role_name, hostname, state, last_seen_at, sessions_baseline, client_user_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING `+clientColumns,
		db.Slug, db.DBName, db.RoleName, db.Hostname, config.StateActive, db.LastSeenAt, db.SessionsBaseline, db.ClientUserID)
	return scanClient(row)
}

// ListDatabases returns every database that is not deleted, newest first.
func (m *Metadata) ListDatabases(ctx context.Context) ([]model.ClientDatabase, error) {
	return m.queryClients(ctx, `SELECT `+clientColumns+` FROM client_databases
		WHERE state <> $1 ORDER BY created_at DESC`, config.StateDeleted)
}

// ListActive returns databases the inactivity auditor must inspect.
func (m *Metadata) ListActive(ctx context.Context) ([]model.ClientDatabase, error) {
	return m.queryClients(ctx, `SELECT `+clientColumns+` FROM client_databases
		WHERE state = $1 ORDER BY id`, config.StateActive)
}

// ListByClientUser returns the non-deleted databases owned by one client,
// for the reduced "panel de usuario".
func (m *Metadata) ListByClientUser(ctx context.Context, clientUserID int64) ([]model.ClientDatabase, error) {
	return m.queryClients(ctx, `SELECT `+clientColumns+` FROM client_databases
		WHERE client_user_id = $1 AND state <> $2 ORDER BY created_at DESC`, clientUserID, config.StateDeleted)
}

// CountByClientUser returns how many non-deleted databases a client owns,
// enforced against MAX_DATABASES_PER_CLIENT at creation time.
func (m *Metadata) CountByClientUser(ctx context.Context, clientUserID int64) (int, error) {
	var n int
	err := m.pool.QueryRow(ctx, `SELECT count(*) FROM client_databases
		WHERE client_user_id = $1 AND state <> $2`, clientUserID, config.StateDeleted).Scan(&n)
	return n, err
}

// GetDatabase finds the live (non-deleted) database for a slug.
func (m *Metadata) GetDatabase(ctx context.Context, slug string) (model.ClientDatabase, error) {
	row := m.pool.QueryRow(ctx, `SELECT `+clientColumns+` FROM client_databases
		WHERE slug = $1 AND state <> $2`, slug, config.StateDeleted)
	return scanClient(row)
}

// RecordActivity stores the newest activity timestamp and counter
// baselines, and re-arms the expiry warning.
func (m *Metadata) RecordActivity(ctx context.Context, id int64, lastSeen time.Time, sessions, internal int64) error {
	return m.exec(ctx, `UPDATE client_databases SET last_seen_at = $2, sessions_baseline = $3,
		internal_baseline = $4, expiry_warned_at = NULL WHERE id = $1`, id, lastSeen, sessions, internal)
}

// UpdateBaseline stores new counters without touching last_seen_at.
func (m *Metadata) UpdateBaseline(ctx context.Context, id int64, sessions, internal int64) error {
	return m.exec(ctx, `UPDATE client_databases SET sessions_baseline = $2, internal_baseline = $3 WHERE id = $1`,
		id, sessions, internal)
}

// IncrementInternalSessions counts a connection PosternDB is about to open
// into a client database (see store.Inspector).
func (m *Metadata) IncrementInternalSessions(ctx context.Context, database string) error {
	_, err := m.pool.Exec(ctx, `UPDATE client_databases SET internal_sessions = internal_sessions + 1
		WHERE db_name = $1 AND state = $2`, database, config.StateActive)
	return err
}

// SetExpiry stores the per-database expiry overrides.
func (m *Metadata) SetExpiry(ctx context.Context, id int64, never bool, maxIdleDays *int, graceUntil *time.Time) error {
	return m.exec(ctx, `UPDATE client_databases SET never_expires = $2, max_idle_days = $3, grace_until = $4,
		expiry_warned_at = NULL WHERE id = $1`, id, never, maxIdleDays, graceUntil)
}

// MarkWarned records that an expiry warning was sent.
func (m *Metadata) MarkWarned(ctx context.Context, id int64, at time.Time) error {
	return m.exec(ctx, `UPDATE client_databases SET expiry_warned_at = $2 WHERE id = $1`, id, at)
}

// MarkExpired flags a database as expired and remembers its archive name.
func (m *Metadata) MarkExpired(ctx context.Context, id int64, archivedAs string, at time.Time) error {
	return m.exec(ctx, `UPDATE client_databases SET state = $2, expired_at = $3, archived_as = NULLIF($4, '')
		WHERE id = $1`, id, config.StateExpired, at, archivedAs)
}

// MarkDeleted hides a database from the panel while keeping its history.
func (m *Metadata) MarkDeleted(ctx context.Context, id int64, archivedAs string, at time.Time) error {
	return m.exec(ctx, `UPDATE client_databases SET state = $2, deleted_at = $3,
		archived_as = COALESCE(NULLIF($4, ''), archived_as) WHERE id = $1`, id, config.StateDeleted, at, archivedAs)
}

// UpdateHostname is used by the hostname migration command.
func (m *Metadata) UpdateHostname(ctx context.Context, id int64, hostname string) error {
	return m.exec(ctx, `UPDATE client_databases SET hostname = $2 WHERE id = $1`, id, hostname)
}

// RecordAudit appends an entry to audit_log.
func (m *Metadata) RecordAudit(ctx context.Context, e model.AuditEntry) error {
	details, err := json.Marshal(nonNil(e.Details))
	if err != nil {
		return err
	}
	_, err = m.pool.Exec(ctx, `INSERT INTO audit_log (actor, action, target, details) VALUES ($1, $2, $3, $4)`,
		e.Actor, e.Action, e.Target, details)
	return err
}

// RenameAuditIdentity relabels every entry whose actor or target is from, so a
// name that someone gives up (or an account that is deleted) is never matched
// to whoever takes it next.
func (m *Metadata) RenameAuditIdentity(ctx context.Context, from, to string) error {
	return m.exec(ctx, `UPDATE audit_log SET
		actor = CASE WHEN actor = $1 THEN $2 ELSE actor END,
		target = CASE WHEN target = $1 THEN $2 ELSE target END
		WHERE actor = $1 OR target = $1`, from, to)
}

// ListAudit returns the most recent audit entries.
func (m *Metadata) ListAudit(ctx context.Context, limit int) ([]model.AuditEntry, error) {
	rows, err := m.pool.Query(ctx, `SELECT id, occurred_at, actor, action, target, details
		FROM audit_log ORDER BY occurred_at DESC, id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.AuditEntry, error) {
		var e model.AuditEntry
		var raw []byte
		if err := r.Scan(&e.ID, &e.OccurredAt, &e.Actor, &e.Action, &e.Target, &raw); err != nil {
			return e, err
		}
		return e, json.Unmarshal(raw, &e.Details)
	})
}

func (m *Metadata) exec(ctx context.Context, sql string, args ...any) error {
	tag, err := m.pool.Exec(ctx, sql, args...)
	if err != nil {
		return translate(err)
	}
	if tag.RowsAffected() == 0 {
		return model.ErrNotFound
	}
	return nil
}

func (m *Metadata) queryClients(ctx context.Context, sql string, args ...any) ([]model.ClientDatabase, error) {
	rows, err := m.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.ClientDatabase, error) {
		return scanClient(r)
	})
}

func scanClient(row pgx.Row) (model.ClientDatabase, error) {
	var c model.ClientDatabase
	err := row.Scan(&c.ID, &c.Slug, &c.DBName, &c.RoleName, &c.Hostname, &c.State, &c.CreatedAt,
		&c.LastSeenAt, &c.ExpiredAt, &c.ArchivedAs, &c.SessionsBaseline,
		&c.InternalSessions, &c.InternalBaseline, &c.NeverExpires, &c.MaxIdleDays, &c.GraceUntil, &c.ExpiryWarnedAt,
		&c.ClientUserID)
	return c, translate(err)
}

func nonNil(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}
