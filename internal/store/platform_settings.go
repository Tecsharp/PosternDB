package store

import (
	"context"

	"github.com/tecsharp/posterndb/internal/model"
)

// GetPlatformSettings returns the global expiry override (row id=1, seeded
// by the migration). Fields are nil when the operator has not overridden
// the corresponding .env default.
func (m *Metadata) GetPlatformSettings(ctx context.Context) (model.PlatformSettings, error) {
	var s model.PlatformSettings
	err := m.pool.QueryRow(ctx, `SELECT expiration_days, expiring_soon_days, updated_at, updated_by
		FROM platform_settings WHERE id = 1`).Scan(&s.ExpirationDays, &s.ExpiringSoonDays, &s.UpdatedAt, &s.UpdatedBy)
	return s, translate(err)
}

// SetPlatformSettings stores the global expiry override. A nil argument
// resets that field back to the .env default.
func (m *Metadata) SetPlatformSettings(ctx context.Context, days, warnDays *int, actor string) (model.PlatformSettings, error) {
	var s model.PlatformSettings
	err := m.pool.QueryRow(ctx, `UPDATE platform_settings SET expiration_days = $1, expiring_soon_days = $2,
		updated_at = now(), updated_by = $3 WHERE id = 1
		RETURNING expiration_days, expiring_soon_days, updated_at, updated_by`, days, warnDays, actor).
		Scan(&s.ExpirationDays, &s.ExpiringSoonDays, &s.UpdatedAt, &s.UpdatedBy)
	return s, translate(err)
}
