// Package expiry contains the pure date arithmetic behind the inactivity
// policy, kept free of I/O so it can be unit tested exhaustively.
package expiry

import (
	"time"

	"github.com/tecsharp/posterndb/internal/config"
	"github.com/tecsharp/posterndb/internal/model"
)

const day = 24 * time.Hour

// Policy is the global inactivity policy from .env.
type Policy struct {
	// MaxIdle is DB_EXPIRATION_DAYS: idle time after which a DB expires.
	MaxIdle time.Duration
	// WarnBefore is EXPIRING_SOON_DAYS: window before expiry in which the
	// panel shows the database as "por expirar" and warnings are sent.
	WarnBefore time.Duration
}

// NewPolicy builds a Policy from configuration.
func NewPolicy(cfg *config.Config) Policy {
	return Policy{MaxIdle: cfg.DBExpiration, WarnBefore: cfg.ExpiringSoon}
}

// WithOverride applies the operator's runtime override (from the owner-only
// Ajustes -> Expiración panel) on top of the .env defaults. A nil field in
// override leaves the corresponding default untouched.
func (p Policy) WithOverride(override model.PlatformSettings) Policy {
	out := p
	if override.ExpirationDays != nil {
		out.MaxIdle = time.Duration(*override.ExpirationDays) * day
	}
	if override.ExpiringSoonDays != nil {
		out.WarnBefore = time.Duration(*override.ExpiringSoonDays) * day
	}
	return out
}

// MaxIdleFor returns the idle limit of one database: its own max_idle_days
// when set, otherwise the global DB_EXPIRATION_DAYS.
func (p Policy) MaxIdleFor(db model.ClientDatabase) time.Duration {
	if db.MaxIdleDays != nil {
		return time.Duration(*db.MaxIdleDays) * day
	}
	return p.MaxIdle
}

// ExpiresAt is the instant db expires if no new activity is seen. The
// second value is false for databases marked "never expires". A grace
// period ("extend N days") can only push the date later, never earlier.
func (p Policy) ExpiresAt(db model.ClientDatabase) (time.Time, bool) {
	if db.NeverExpires {
		return time.Time{}, false
	}
	at := db.LastSeenAt.Add(p.MaxIdleFor(db))
	if db.GraceUntil != nil && db.GraceUntil.After(at) {
		at = *db.GraceUntil
	}
	return at, true
}

// IsExpired reports whether db is past its expiry instant.
func (p Policy) IsExpired(db model.ClientDatabase, now time.Time) bool {
	at, ok := p.ExpiresAt(db)
	return ok && now.After(at)
}

// InWarningWindow reports whether db expires within WarnBefore.
func (p Policy) InWarningWindow(db model.ClientDatabase, now time.Time) bool {
	at, ok := p.ExpiresAt(db)
	return ok && !now.After(at) && at.Sub(now) <= p.WarnBefore
}

// ApplyClientOverride resolves a database's effective exception before
// Policy.Status/ExpiresAt/IsExpired run on it:
//
//  1. The database's own exception (never_expires / max_idle_days), if set,
//     always wins — it is the most specific and most recently chosen.
//  2. Otherwise, its owning client's exception, if the owner set one
//     ("usuario3 nunca expira en todas sus bases de datos").
//  3. Otherwise the database is returned unchanged, so Policy falls back to
//     the global default (platform_settings, then .env). Databases with no
//     client_user_id (created through the admin panel) always land here.
//
// users is keyed by client_users.id; callers fetch it once per batch (List,
// the auditor's run) rather than per database.
func ApplyClientOverride(db model.ClientDatabase, users map[int64]model.ClientUser) model.ClientDatabase {
	if db.NeverExpires || db.MaxIdleDays != nil {
		return db
	}
	if db.ClientUserID == nil {
		return db
	}
	if u, ok := users[*db.ClientUserID]; ok {
		db.NeverExpires = u.NeverExpires
		db.MaxIdleDays = u.MaxIdleDays
	}
	return db
}

// Status returns the label shown in the panel. Expired is final; active
// databases are split into "activa" and "por_expirar".
func (p Policy) Status(db model.ClientDatabase, now time.Time) string {
	if db.State == config.StateExpired || p.IsExpired(db, now) {
		return config.StatusExpired
	}
	if p.InWarningWindow(db, now) {
		return config.StatusExpiring
	}
	return config.StatusActive
}
