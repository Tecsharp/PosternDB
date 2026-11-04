// Package auditor runs the periodic inactivity audit.
//
// Every CHECK_INTERVAL_HOURS it:
//  1. Reads a snapshot of PostgreSQL activity statistics for all client DBs.
//  2. Refreshes last_seen_at for every database that shows activity.
//  3. Sends a warning (webhook/email) once a database enters the
//     EXPIRING_SOON_DAYS window, re-armed whenever activity is seen.
//  4. Expires databases idle for longer than their limit (unpublish the
//     hostname, then archive for backup or drop) and notifies about it.
//
// Each run, each activity refresh and each expiration is written to
// audit_log so an operator can always answer "why did this DB disappear?".
package auditor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/tecsharp/posterndb/internal/config"
	"github.com/tecsharp/posterndb/internal/expiry"
	"github.com/tecsharp/posterndb/internal/model"
	"github.com/tecsharp/posterndb/internal/notify"
)

// Metadata is the subset of the repository the auditor needs.
type Metadata interface {
	ListActive(ctx context.Context) ([]model.ClientDatabase, error)
	RecordActivity(ctx context.Context, id int64, lastSeen time.Time, sessions, internal int64) error
	UpdateBaseline(ctx context.Context, id int64, sessions, internal int64) error
	MarkWarned(ctx context.Context, id int64, at time.Time) error
}

// Notifier delivers operator notifications.
type Notifier interface {
	Send(ctx context.Context, m notify.Message) error
}

// ActivitySource returns per-database activity statistics.
type ActivitySource interface {
	ActivitySnapshot(ctx context.Context) (map[string]model.Activity, error)
}

// Lifecycle expires databases and writes audit entries.
type Lifecycle interface {
	Expire(ctx context.Context, db model.ClientDatabase, actor string) error
	RecordAudit(ctx context.Context, actor, action, target string, details map[string]any)
	// EffectivePolicy merges the .env defaults with the owner's runtime
	// override (Ajustes -> Expiración), falling back to the .env defaults
	// on read error.
	EffectivePolicy(ctx context.Context) expiry.Policy
	// ClientOverrides lists every client's exception, keyed by
	// client_users.id, so each database can apply its owning client's
	// exception (see expiry.ApplyClientOverride) without a query per
	// database.
	ClientOverrides(ctx context.Context) map[int64]model.ClientUser
}

// Auditor wires the dependencies together.
type Auditor struct {
	meta     Metadata
	activity ActivitySource
	life     Lifecycle
	notifier Notifier
	interval time.Duration
	now      func() time.Time
	log      *slog.Logger
}

// New builds an Auditor.
func New(meta Metadata, activity ActivitySource, life Lifecycle, notifier Notifier, interval time.Duration, logger *slog.Logger) *Auditor {
	return &Auditor{meta: meta, activity: activity, life: life, notifier: notifier, interval: interval, now: time.Now, log: logger}
}

// Result summarises one audit run.
type Result struct {
	Checked int `json:"checked"`
	Active  int `json:"activeSeen"`
	Expired int `json:"expired"`
	Warned  int `json:"warned"`
	Errors  int `json:"errors"`
}

// Run executes an audit immediately and then on every tick until ctx ends.
func (a *Auditor) Run(ctx context.Context) {
	ticker := time.NewTicker(a.interval)
	defer ticker.Stop()
	for {
		a.RunOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// RunOnce performs a single audit pass.
func (a *Auditor) RunOnce(ctx context.Context) Result {
	var res Result
	dbs, err := a.meta.ListActive(ctx)
	if err != nil {
		a.log.Error("inactivity audit: list databases", "error", err)
		return Result{Errors: 1}
	}
	snapshot, err := a.activity.ActivitySnapshot(ctx)
	if err != nil {
		// Without statistics we cannot tell idle from busy. Expiring on
		// missing data would delete live databases, so skip this run.
		a.log.Error("inactivity audit: read pg_stat_activity", "error", err)
		return Result{Errors: 1}
	}
	now := a.now()
	policy := a.life.EffectivePolicy(ctx)
	overrides := a.life.ClientOverrides(ctx)
	for _, db := range dbs {
		res.Checked++
		a.inspect(ctx, expiry.ApplyClientOverride(db, overrides), snapshot[db.DBName], policy, now, &res)
	}
	a.life.RecordAudit(ctx, config.ActorAuditor, config.ActionAuditRun, "", map[string]any{
		"checked": res.Checked, "activeSeen": res.Active, "expired": res.Expired, "warned": res.Warned, "errors": res.Errors,
	})
	return res
}

func (a *Auditor) inspect(ctx context.Context, db model.ClientDatabase, act model.Activity, policy expiry.Policy, now time.Time, res *Result) {
	seen, err := a.refreshActivity(ctx, db, act, now)
	if err != nil {
		res.Errors++
		a.log.Error("inactivity audit: update activity", "slug", db.Slug, "error", err)
		return
	}
	if seen {
		res.Active++
		return
	}
	if policy.InWarningWindow(db, now) && db.ExpiryWarnedAt == nil {
		a.warn(ctx, db, policy, now, res)
		return
	}
	if !policy.IsExpired(db, now) {
		return
	}
	if err := a.life.Expire(ctx, db, config.ActorAuditor); err != nil {
		res.Errors++
		a.log.Error("inactivity audit: expire database", "slug", db.Slug, "error", err)
		return
	}
	res.Expired++
	a.notify(ctx, notify.Message{
		Event: "expired", Database: db.Slug,
		Title: fmt.Sprintf("La base %s expiró", db.Slug),
		Text:  fmt.Sprintf("Sin conexiones desde %s. Se retiró %s y la base se respaldó o eliminó según BACKUP_BEFORE_DELETE.", db.LastSeenAt.Format(time.DateOnly), db.Hostname),
	})
}

// warn sends the "expiring soon" notice once per idle period.
func (a *Auditor) warn(ctx context.Context, db model.ClientDatabase, policy expiry.Policy, now time.Time, res *Result) {
	at, _ := policy.ExpiresAt(db)
	days := int(at.Sub(now).Hours()/24) + 1
	sent := a.notify(ctx, notify.Message{
		Event: "expiring", Database: db.Slug,
		Title: fmt.Sprintf("La base %s expira en %d días", db.Slug, days),
		Text: fmt.Sprintf("Última conexión: %s. Expira el %s si nadie se conecta. Puedes extender el plazo desde el panel.",
			db.LastSeenAt.Format(time.DateOnly), at.Format(time.DateOnly)),
	})
	if !sent {
		return
	}
	if err := a.meta.MarkWarned(ctx, db.ID, now); err != nil {
		res.Errors++
		return
	}
	res.Warned++
	a.life.RecordAudit(ctx, config.ActorAuditor, config.ActionExpiryWarned, db.Slug, map[string]any{"expiresAt": at})
}

// notify returns true when the message was delivered. Missing channels are
// not an error: notifications are optional.
func (a *Auditor) notify(ctx context.Context, m notify.Message) bool {
	err := a.notifier.Send(ctx, m)
	if errors.Is(err, notify.ErrNoChannels) {
		return false
	}
	if err != nil {
		a.log.Error("notification failed", "event", m.Event, "database", m.Database, "error", err)
		a.life.RecordAudit(ctx, config.ActorAuditor, config.ActionNotifyFailed, m.Database, map[string]any{"event": m.Event})
		return false
	}
	return true
}

// refreshActivity decides whether db was used since the previous run and
// persists the outcome. It returns true when activity was detected.
//
// Sessions PosternDB opened itself (schema explorer, console, grants) are
// counted in internal_sessions and subtracted, so looking at a database in
// the panel never keeps it alive.
func (a *Auditor) refreshActivity(ctx context.Context, db model.ClientDatabase, act model.Activity, now time.Time) (bool, error) {
	internal := max(db.InternalSessions-db.InternalBaseline, 0)
	fresh := max(newSessions(db.SessionsBaseline, act.Sessions)-internal, 0)
	if act.Connections == 0 && fresh == 0 {
		if act.Sessions == db.SessionsBaseline && internal == 0 {
			return false, nil
		}
		return false, a.meta.UpdateBaseline(ctx, db.ID, act.Sessions, db.InternalSessions)
	}
	if err := a.meta.RecordActivity(ctx, db.ID, now, act.Sessions, db.InternalSessions); err != nil {
		return false, err
	}
	return true, nil
}

// newSessions returns how many sessions were opened since the baseline. If
// the counter went backwards, statistics were reset (crash recovery or
// pg_stat_reset), so everything counted since the reset is new.
func newSessions(baseline, current int64) int64 {
	if current < baseline {
		return current
	}
	return current - baseline
}
