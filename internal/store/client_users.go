package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/tecsharp/posterndb/internal/model"
)

const clientUserColumns = `id, username, password_hash, never_expires, max_idle_days,
	created_at, created_by, updated_at, updated_by, last_login_at`

// ListClientUsers returns every registered client, for the owner-only
// "Usuarios" page. Does not include each one's database count; the caller
// composes that from ListDatabases/ListByClientUser to avoid an N+1 query.
func (m *Metadata) ListClientUsers(ctx context.Context) ([]model.ClientUser, error) {
	rows, err := m.pool.Query(ctx, `SELECT `+clientUserColumns+` FROM client_users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.ClientUser, error) { return scanClientUser(r) })
}

// GetClientUser finds one client by id.
func (m *Metadata) GetClientUser(ctx context.Context, id int64) (model.ClientUser, error) {
	return scanClientUser(m.pool.QueryRow(ctx, `SELECT `+clientUserColumns+` FROM client_users WHERE id = $1`, id))
}

// GetClientUserByUsername is used at login.
func (m *Metadata) GetClientUserByUsername(ctx context.Context, username string) (model.ClientUser, error) {
	return scanClientUser(m.pool.QueryRow(ctx, `SELECT `+clientUserColumns+` FROM client_users WHERE username = $1`, username))
}

// CreateClientUser registers a new client. createdBy is "" for self-service
// sign-up, or the admin username when the owner adds one directly.
func (m *Metadata) CreateClientUser(ctx context.Context, username, passwordHash, createdBy string) (model.ClientUser, error) {
	return scanClientUser(m.pool.QueryRow(ctx, `INSERT INTO client_users (username, password_hash, created_by)
		VALUES ($1, $2, $3) RETURNING `+clientUserColumns, username, passwordHash, createdBy))
}

// UpdateClientUserPassword stores a new password hash.
func (m *Metadata) UpdateClientUserPassword(ctx context.Context, id int64, passwordHash string) error {
	return m.exec(ctx, `UPDATE client_users SET password_hash = $2 WHERE id = $1`, id, passwordHash)
}

// TouchClientUserLogin records a successful login.
func (m *Metadata) TouchClientUserLogin(ctx context.Context, id int64, at time.Time) error {
	return m.exec(ctx, `UPDATE client_users SET last_login_at = $2 WHERE id = $1`, id, at)
}

// SetClientUserExpiry stores the owner's exception for this client, applied
// to every database they own that has no exception of its own.
func (m *Metadata) SetClientUserExpiry(ctx context.Context, id int64, never bool, maxIdleDays *int, actor string) (model.ClientUser, error) {
	return scanClientUser(m.pool.QueryRow(ctx, `UPDATE client_users SET never_expires = $2, max_idle_days = $3,
		updated_at = now(), updated_by = $4 WHERE id = $1 RETURNING `+clientUserColumns, id, never, maxIdleDays, actor))
}

// DeleteClientUser removes a client account. Their databases are not
// touched; client_databases.client_user_id is set to NULL (ON DELETE SET
// NULL) and they fall back to the global expiry policy.
func (m *Metadata) DeleteClientUser(ctx context.Context, id int64) error {
	return m.exec(ctx, `DELETE FROM client_users WHERE id = $1`, id)
}

func scanClientUser(row pgx.Row) (model.ClientUser, error) {
	var u model.ClientUser
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.NeverExpires, &u.MaxIdleDays,
		&u.CreatedAt, &u.CreatedBy, &u.UpdatedAt, &u.UpdatedBy, &u.LastLoginAt)
	return u, translate(err)
}
