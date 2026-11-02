package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/tecsharp/posterndb/internal/model"
)

const adminColumns = `id, username, password_hash, role, created_at, created_by, last_login_at`

// ListAdmins returns stored administrators (not the .env bootstrap owner).
func (m *Metadata) ListAdmins(ctx context.Context) ([]model.Admin, error) {
	rows, err := m.pool.Query(ctx, `SELECT `+adminColumns+` FROM admins ORDER BY username`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.Admin, error) { return scanAdmin(r) })
}

// GetAdmin finds one administrator by username.
func (m *Metadata) GetAdmin(ctx context.Context, username string) (model.Admin, error) {
	return scanAdmin(m.pool.QueryRow(ctx, `SELECT `+adminColumns+` FROM admins WHERE username = $1`, username))
}

// CreateAdmin stores a new administrator.
func (m *Metadata) CreateAdmin(ctx context.Context, a model.Admin) (model.Admin, error) {
	return scanAdmin(m.pool.QueryRow(ctx, `INSERT INTO admins (username, password_hash, role, created_by)
		VALUES ($1, $2, $3, $4) RETURNING `+adminColumns, a.Username, a.PasswordHash, a.Role, a.CreatedBy))
}

// UpdateAdminRole changes the role of an administrator.
func (m *Metadata) UpdateAdminRole(ctx context.Context, username, role string) error {
	return m.exec(ctx, `UPDATE admins SET role = $2 WHERE username = $1`, username, role)
}

// UpdateAdminPassword stores a new password hash.
func (m *Metadata) UpdateAdminPassword(ctx context.Context, username, hash string) error {
	return m.exec(ctx, `UPDATE admins SET password_hash = $2 WHERE username = $1`, username, hash)
}

// TouchAdminLogin records a successful login.
func (m *Metadata) TouchAdminLogin(ctx context.Context, username string, at time.Time) error {
	return m.exec(ctx, `UPDATE admins SET last_login_at = $2 WHERE username = $1`, username, at)
}

// DeleteAdmin removes an administrator.
func (m *Metadata) DeleteAdmin(ctx context.Context, username string) error {
	return m.exec(ctx, `DELETE FROM admins WHERE username = $1`, username)
}

func scanAdmin(row pgx.Row) (model.Admin, error) {
	var a model.Admin
	err := row.Scan(&a.ID, &a.Username, &a.PasswordHash, &a.Role, &a.CreatedAt, &a.CreatedBy, &a.LastLoginAt)
	return a, translate(err)
}
