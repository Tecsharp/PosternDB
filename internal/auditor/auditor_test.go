package auditor

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tecsharp/posterndb/internal/config"
	"github.com/tecsharp/posterndb/internal/expiry"
	"github.com/tecsharp/posterndb/internal/model"
	"github.com/tecsharp/posterndb/internal/naming"
	"github.com/tecsharp/posterndb/internal/provision"
	"github.com/tecsharp/posterndb/internal/testsupport"
)

const day = 24 * time.Hour

var now = time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)

type harness struct {
	a        *Auditor
	meta     *testsupport.FakeMetadata
	cluster  *testsupport.FakeCluster
	cf       *testsupport.FakeCloudflareClient
	notifier *testsupport.FakeNotifier
}

func newHarness() harness {
	h := harness{meta: testsupport.NewFakeMetadata(), cluster: testsupport.NewFakeCluster(),
		cf: testsupport.NewFakeCloudflareClient(), notifier: &testsupport.FakeNotifier{}}
	policy := expiry.Policy{MaxIdle: 30 * day, WarnBefore: 7 * day}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := provision.New(h.meta, h.cluster, h.cf, testsupport.NewFakeInspector(), testsupport.FakeSecrets{}, provision.Options{
		Resolver: naming.Resolver{BaseDomain: "x.com"}, Policy: policy, BackupBeforeDelete: true,
		Now: func() time.Time { return now }, Logger: logger,
	})
	h.a = New(h.meta, h.cluster, svc, h.notifier, time.Hour, logger)
	h.a.now = func() time.Time { return now }
	return h
}

func (h harness) seed(slug string, lastSeen time.Time, baseline int64) model.ClientDatabase {
	db := naming.DatabaseName(slug)
	h.cluster.Databases[db] = db
	h.cf.Routes[slug+".x.com"] = "tcp://db:5432"
	return h.meta.Seed(model.ClientDatabase{Slug: slug, DBName: db, RoleName: db, Hostname: slug + ".x.com", LastSeenAt: lastSeen, SessionsBaseline: baseline})
}

func TestOpenConnectionRefreshesLastSeen(t *testing.T) {
	h := newHarness()
	db := h.seed("busy", now.Add(-40*day), 10)
	h.cluster.Activity[db.DBName] = model.Activity{Connections: 3, Sessions: 10}

	res := h.a.RunOnce(context.Background())

	assert.Equal(t, Result{Checked: 1, Active: 1}, res)
	assert.Equal(t, now, h.meta.Rows[db.ID].LastSeenAt)
	assert.Equal(t, config.StateActive, h.meta.Rows[db.ID].State, "activity saves it even when overdue")
	assert.Contains(t, h.meta.Actions(), config.ActionActivitySeen)
}

func TestNewSessionsSinceLastRunCountAsActivity(t *testing.T) {
	h := newHarness()
	db := h.seed("cron-job", now.Add(-5*day), 10)
	h.cluster.Activity[db.DBName] = model.Activity{Connections: 0, Sessions: 12}

	h.a.RunOnce(context.Background())

	assert.Equal(t, now, h.meta.Rows[db.ID].LastSeenAt)
	assert.Equal(t, int64(12), h.meta.Rows[db.ID].SessionsBaseline)
}

func TestIdleButWithinPolicyIsKept(t *testing.T) {
	h := newHarness()
	lastSeen := now.Add(-10 * day)
	db := h.seed("quiet", lastSeen, 5)
	h.cluster.Activity[db.DBName] = model.Activity{Sessions: 5}

	res := h.a.RunOnce(context.Background())

	assert.Equal(t, Result{Checked: 1}, res)
	assert.Equal(t, lastSeen, h.meta.Rows[db.ID].LastSeenAt)
}

func TestIdleBeyondPolicyExpires(t *testing.T) {
	h := newHarness()
	db := h.seed("abandoned", now.Add(-31*day), 5)
	h.cluster.Activity[db.DBName] = model.Activity{Sessions: 5}

	res := h.a.RunOnce(context.Background())

	assert.Equal(t, Result{Checked: 1, Expired: 1}, res)
	assert.Equal(t, config.StateExpired, h.meta.Rows[db.ID].State)
	assert.False(t, h.cf.HasRoute(db.Hostname))
	assert.NotEmpty(t, h.cluster.Archived)
	require.Len(t, h.notifier.Sent, 1)
	assert.Equal(t, "expired", h.notifier.Sent[0].Event)
	assert.Equal(t, config.ActionAuditRun, h.meta.Actions()[len(h.meta.Audit)-1])
}

func TestStatsResetWithoutNewSessionsOnlyRebaselines(t *testing.T) {
	h := newHarness()
	lastSeen := now.Add(-3 * day)
	db := h.seed("reset", lastSeen, 50)
	h.cluster.Activity[db.DBName] = model.Activity{Sessions: 0}

	h.a.RunOnce(context.Background())

	assert.Equal(t, lastSeen, h.meta.Rows[db.ID].LastSeenAt)
	assert.Equal(t, int64(0), h.meta.Rows[db.ID].SessionsBaseline)
}

func TestStatsResetWithNewSessionsIsActivity(t *testing.T) {
	h := newHarness()
	db := h.seed("reset-busy", now.Add(-3*day), 50)
	h.cluster.Activity[db.DBName] = model.Activity{Sessions: 2}

	h.a.RunOnce(context.Background())

	assert.Equal(t, now, h.meta.Rows[db.ID].LastSeenAt)
	assert.Equal(t, int64(2), h.meta.Rows[db.ID].SessionsBaseline)
}

func TestStatisticsFailureSkipsRunWithoutExpiring(t *testing.T) {
	h := newHarness()
	db := h.seed("abandoned", now.Add(-60*day), 0)
	h.cluster.FailStats = true

	res := h.a.RunOnce(context.Background())

	assert.Equal(t, Result{Errors: 1}, res)
	assert.Equal(t, config.StateActive, h.meta.Rows[db.ID].State, "never expire on missing data")
}

func TestListFailure(t *testing.T) {
	h := newHarness()
	h.meta.FailList = true
	assert.Equal(t, Result{Errors: 1}, h.a.RunOnce(context.Background()))
}

func TestUpdateAndExpireFailuresAreCounted(t *testing.T) {
	h := newHarness()
	db := h.seed("busy", now, 0)
	h.cluster.Activity[db.DBName] = model.Activity{Connections: 1}
	h.meta.FailUpdate = true
	assert.Equal(t, 1, h.a.RunOnce(context.Background()).Errors)

	h = newHarness()
	h.seed("old", now.Add(-60*day), 0)
	h.cf.FailRemove = true
	assert.Equal(t, 1, h.a.RunOnce(context.Background()).Errors)
}

func TestRunStopsWithContext(t *testing.T) {
	h := newHarness()
	h.a.interval = time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		h.a.Run(ctx)
		close(done)
	}()
	require.Eventually(t, func() bool { return len(h.meta.Actions()) >= 2 }, time.Second, time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not stop")
	}
}

func TestPanelSessionsAreNotActivity(t *testing.T) {
	h := newHarness()
	lastSeen := now.Add(-10 * day)
	db := h.seed("browsed", lastSeen, 10)
	h.meta.Rows[db.ID].InternalSessions = 3 // the panel opened 3 sessions
	h.cluster.Activity[db.DBName] = model.Activity{Sessions: 13}

	res := h.a.RunOnce(context.Background())

	assert.Equal(t, 0, res.Active)
	assert.Equal(t, lastSeen, h.meta.Rows[db.ID].LastSeenAt)
	assert.Equal(t, int64(13), h.meta.Rows[db.ID].SessionsBaseline)
	assert.Equal(t, int64(3), h.meta.Rows[db.ID].InternalBaseline)
}

func TestClientSessionsBesidesPanelSessionsAreActivity(t *testing.T) {
	h := newHarness()
	db := h.seed("mixed", now.Add(-10*day), 10)
	h.meta.Rows[db.ID].InternalSessions = 3
	h.cluster.Activity[db.DBName] = model.Activity{Sessions: 15}

	h.a.RunOnce(context.Background())
	assert.Equal(t, now, h.meta.Rows[db.ID].LastSeenAt)
}

func TestWarningSentOnceInWindow(t *testing.T) {
	h := newHarness()
	db := h.seed("soon", now.Add(-25*day), 0)

	res := h.a.RunOnce(context.Background())
	assert.Equal(t, 1, res.Warned)
	require.Len(t, h.notifier.Sent, 1)
	assert.Equal(t, "expiring", h.notifier.Sent[0].Event)
	assert.Contains(t, h.notifier.Sent[0].Title, "soon")
	assert.NotNil(t, h.meta.Rows[db.ID].ExpiryWarnedAt)

	res = h.a.RunOnce(context.Background())
	assert.Equal(t, 0, res.Warned, "not repeated")
	assert.Len(t, h.notifier.Sent, 1)
}

func TestWarningWithoutChannelsIsSilent(t *testing.T) {
	h := newHarness()
	h.notifier.Disabled = true
	db := h.seed("soon", now.Add(-25*day), 0)
	res := h.a.RunOnce(context.Background())
	assert.Equal(t, 0, res.Warned)
	assert.Equal(t, 0, res.Errors)
	assert.Nil(t, h.meta.Rows[db.ID].ExpiryWarnedAt, "retried once a channel is configured")
}

func TestWarningFailureIsAudited(t *testing.T) {
	h := newHarness()
	h.notifier.Err = assert.AnError
	h.seed("soon", now.Add(-25*day), 0)
	h.a.RunOnce(context.Background())
	assert.Contains(t, h.meta.Actions(), config.ActionNotifyFailed)
}

func TestWarnMarkFailureCountsError(t *testing.T) {
	h := newHarness()
	h.seed("soon", now.Add(-25*day), 0)
	h.meta.FailUpdate = true
	assert.Equal(t, 1, h.a.RunOnce(context.Background()).Errors)
}

func TestNeverExpiringDatabaseIsKept(t *testing.T) {
	h := newHarness()
	db := h.seed("forever", now.Add(-400*day), 0)
	h.meta.Rows[db.ID].NeverExpires = true
	res := h.a.RunOnce(context.Background())
	assert.Equal(t, 0, res.Expired+res.Warned)
}

func TestNewSessions(t *testing.T) {
	assert.Equal(t, int64(3), newSessions(10, 13))
	assert.Equal(t, int64(0), newSessions(10, 10))
	assert.Equal(t, int64(4), newSessions(10, 4))
}

func TestAuditorUsesGlobalOverride(t *testing.T) {
	h := newHarness()
	days := 10
	_, err := h.meta.SetPlatformSettings(context.Background(), &days, nil, "root")
	require.NoError(t, err)
	db := h.seed("short-lived", now.Add(-11*day), 0)

	res := h.a.RunOnce(context.Background())

	assert.Equal(t, 1, res.Expired, "expires against the 10-day override, not the 30-day default")
	assert.Equal(t, config.StateExpired, h.meta.Rows[db.ID].State)
}
