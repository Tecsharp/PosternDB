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
