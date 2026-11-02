package expiry

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/tecsharp/posterndb/internal/config"
	"github.com/tecsharp/posterndb/internal/model"
)

var (
	policy = Policy{MaxIdle: 30 * day, WarnBefore: 7 * day}
	now    = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
)

func active(lastSeen time.Time) model.ClientDatabase {
	return model.ClientDatabase{State: config.StateActive, LastSeenAt: lastSeen}
}

func ptr[T any](v T) *T { return &v }

func TestExpiresAtUsesGlobalPolicy(t *testing.T) {
	at, ok := policy.ExpiresAt(active(now))
	assert.True(t, ok)
	assert.Equal(t, now.Add(30*day), at)
}

func TestPerDatabaseMaxIdle(t *testing.T) {
	db := active(now)
	db.MaxIdleDays = ptr(90)
	at, _ := policy.ExpiresAt(db)
	assert.Equal(t, now.Add(90*day), at)
	assert.Equal(t, 90*day, policy.MaxIdleFor(db))
}

func TestGraceOnlyExtends(t *testing.T) {
	db := active(now)
	db.GraceUntil = ptr(now.Add(45 * day))
	at, _ := policy.ExpiresAt(db)
	assert.Equal(t, now.Add(45*day), at)

	db.GraceUntil = ptr(now.Add(5 * day))
	at, _ = policy.ExpiresAt(db)
	assert.Equal(t, now.Add(30*day), at, "an older grace date never shortens the deadline")
}

func TestNeverExpires(t *testing.T) {
	db := active(now.Add(-400 * day))
	db.NeverExpires = true
	_, ok := policy.ExpiresAt(db)
	assert.False(t, ok)
	assert.False(t, policy.IsExpired(db, now))
	assert.False(t, policy.InWarningWindow(db, now))
	assert.Equal(t, config.StatusActive, policy.Status(db, now))
}

func TestIsExpiredBoundary(t *testing.T) {
	assert.False(t, policy.IsExpired(active(now.Add(-30*day)), now), "exactly at the limit is not expired yet")
	assert.True(t, policy.IsExpired(active(now.Add(-30*day-time.Second)), now))
}

func TestStatus(t *testing.T) {
	cases := map[string]struct {
		db   model.ClientDatabase
		want string
	}{
		"fresh":             {active(now.Add(-1 * day)), config.StatusActive},
		"just outside warn": {active(now.Add(-22*day - time.Hour)), config.StatusActive},
		"warning window":    {active(now.Add(-23 * day)), config.StatusExpiring},
		"overdue":           {active(now.Add(-31 * day)), config.StatusExpired},
		"stored expired":    {model.ClientDatabase{State: config.StateExpired, LastSeenAt: now}, config.StatusExpired},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, policy.Status(tc.db, now))
		})
	}
}

func TestInWarningWindowExcludesExpired(t *testing.T) {
	assert.False(t, policy.InWarningWindow(active(now.Add(-40*day)), now))
	assert.True(t, policy.InWarningWindow(active(now.Add(-25*day)), now))
}

func TestNewPolicy(t *testing.T) {
	p := NewPolicy(&config.Config{DBExpiration: 5 * day, ExpiringSoon: day})
	assert.Equal(t, Policy{MaxIdle: 5 * day, WarnBefore: day}, p)
}
