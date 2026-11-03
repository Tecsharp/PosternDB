package provision

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tecsharp/posterndb/internal/config"
	"github.com/tecsharp/posterndb/internal/expiry"
	"github.com/tecsharp/posterndb/internal/model"
	"github.com/tecsharp/posterndb/internal/naming"
	"github.com/tecsharp/posterndb/internal/testsupport"
)

const day = 24 * time.Hour

var (
	fixedNow = time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	shared   = naming.Resolver{BaseDomain: "pachislab.com", HostPrefix: "postern-", PanelHostname: "postern.pachislab.com"}
)

type harness struct {
	svc     *Service
	meta    *testsupport.FakeMetadata
	cluster *testsupport.FakeCluster
	cf      *testsupport.FakeCloudflareClient
	insp    *testsupport.FakeInspector
}

func newHarness(backup bool, secrets testsupport.FakeSecrets) harness {
	return newHarnessWith(shared, backup, secrets)
}

func newHarnessWith(resolver naming.Resolver, backup bool, secrets testsupport.FakeSecrets) harness {
	h := harness{
		meta:    testsupport.NewFakeMetadata(),
		cluster: testsupport.NewFakeCluster(),
		cf:      testsupport.NewFakeCloudflareClient(),
		insp:    testsupport.NewFakeInspector(),
	}
	h.svc = New(h.meta, h.cluster, h.cf, h.insp, secrets, Options{
		Resolver:           resolver,
		Policy:             expiry.Policy{MaxIdle: 30 * day, WarnBefore: 7 * day},
		BackupBeforeDelete: backup,
		TunnelDBService:    "tcp://db:5432",
		ClientLocalPort:    5432,
		LocalDBPort:        25432,
		BackupDir:          "/backups",
		Now:                func() time.Time { return fixedNow },
	})
	return h
}

func (h harness) create(t *testing.T, req CreateRequest) model.Credentials {
	t.Helper()
	creds, err := h.svc.Create(context.Background(), req, "admin")
	require.NoError(t, err)
	return creds
}

func TestCreateProvisionsEverything(t *testing.T) {
	h := newHarness(true, testsupport.FakeSecrets{})
	creds := h.create(t, CreateRequest{Name: "  Taller-Motos "})

	assert.Equal(t, model.ConnectionInfo{
		Hostname: "postern-taller-motos.pachislab.com", Database: "pdb_taller_motos", Username: "pdb_taller_motos",
		Host: config.LocalHostname, Port: 5432,
	}, creds.ConnectionInfo)
	assert.Len(t, creds.Password, config.GeneratedPasswordLn)
	assert.Equal(t, "SCRAM-SHA-256$fake:"+creds.Password, h.cluster.Roles["pdb_taller_motos"], "only the verifier reaches the cluster")
	assert.Equal(t, "pdb_taller_motos", h.cluster.Databases["pdb_taller_motos"])
	assert.True(t, h.cluster.ConsoleGrants["pdb_taller_motos"])
	assert.Equal(t, "tcp://db:5432", h.cf.Routes["postern-taller-motos.pachislab.com"])

	row, err := h.meta.GetDatabase(context.Background(), "taller-motos")
	require.NoError(t, err)
	assert.Equal(t, fixedNow, row.LastSeenAt)
	assert.Equal(t, []string{config.ActionDBCreated}, h.meta.Actions())
	assert.Equal(t, "empty", h.meta.Audit[0].Details["source"])
}

func TestCreateLocalModeSkipsCloudflare(t *testing.T) {
	h := newHarnessWith(naming.Resolver{Local: true}, true, testsupport.FakeSecrets{})
	creds := h.create(t, CreateRequest{Name: "acme"})
	assert.Empty(t, creds.Hostname)
	assert.True(t, creds.LocalMode)
	assert.Equal(t, 25432, creds.Port)
	assert.Empty(t, h.cf.Routes)
}

func TestCreateFromTemplate(t *testing.T) {
	h := newHarness(true, testsupport.FakeSecrets{})
	tpl, _ := h.meta.SaveTemplate(context.Background(), model.Template{Name: "demo", SQL: "CREATE TABLE x (id int)"})
	h.create(t, CreateRequest{Name: "acme", TemplateID: tpl.ID})
	assert.Equal(t, []string{"CREATE TABLE x (id int)"}, h.insp.Applied)
	assert.Equal(t, "template:1", h.meta.Audit[0].Details["source"])
}

func TestCreateTemplateFailureRollsBack(t *testing.T) {
	h := newHarness(true, testsupport.FakeSecrets{})
	tpl, _ := h.meta.SaveTemplate(context.Background(), model.Template{Name: "broken", SQL: "nope"})
	h.insp.FailApply = true
	_, err := h.svc.Create(context.Background(), CreateRequest{Name: "acme", TemplateID: tpl.ID}, "admin")
	var invalidErr *InvalidInputError
	require.ErrorAs(t, err, &invalidErr)
	assert.Contains(t, err.Error(), "broken")
	assert.Empty(t, h.cluster.Databases)
	assert.Empty(t, h.cluster.Roles)
}

func TestCreateCloneQueuesJob(t *testing.T) {
	h := newHarness(true, testsupport.FakeSecrets{})
	h.create(t, CreateRequest{Name: "origen"})
	h.create(t, CreateRequest{Name: "copia", CloneFrom: "origen"})
	require.Len(t, h.meta.Jobs, 1)
	j := h.meta.Jobs[0]
	assert.Equal(t, config.JobClone, j.Kind)
	assert.Equal(t, "pdb_origen", j.SourceDB)
	assert.Equal(t, "pdb_copia", j.TargetDB)
	assert.Equal(t, "pdb_copia", j.TargetRole)
	assert.Equal(t, "copia", j.Slug)
}

func TestCreateRestoreQueuesJob(t *testing.T) {
	h := newHarness(true, testsupport.FakeSecrets{})
	b, _ := h.meta.EnqueueJob(context.Background(), model.Job{Kind: config.JobDump, Slug: "old", File: "pdb_old_1.dump"})
	h.meta.SetJobStatus(b.ID, config.JobDone)
	h.create(t, CreateRequest{Name: "nueva", RestoreBackupID: b.ID})
	j := h.meta.Jobs[len(h.meta.Jobs)-1]
	assert.Equal(t, config.JobRestore, j.Kind)
	assert.Equal(t, "pdb_old_1.dump", j.File)
	assert.Equal(t, "pdb_nueva", j.TargetDB)
}

func TestCreateSourceValidation(t *testing.T) {
	h := newHarness(true, testsupport.FakeSecrets{})
	cases := map[string]CreateRequest{
		"two sources":      {Name: "acme", TemplateID: 1, CloneFrom: "x"},
		"missing template": {Name: "acme", TemplateID: 99},
		"missing clone":    {Name: "acme", CloneFrom: "ghost"},
		"missing backup":   {Name: "acme", RestoreBackupID: 99},
		"invalid name":     {Name: "bad name!"},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := h.svc.Create(context.Background(), req, "admin")
			var invalidErr *InvalidInputError
			assert.ErrorAs(t, err, &invalidErr)
		})
	}
}

func TestCreateRestoreRejectsUnfinishedBackup(t *testing.T) {
	h := newHarness(true, testsupport.FakeSecrets{})
	b, _ := h.meta.EnqueueJob(context.Background(), model.Job{Kind: config.JobDump, File: "f.dump"})
	_, err := h.svc.Create(context.Background(), CreateRequest{Name: "acme", RestoreBackupID: b.ID}, "admin")
	assert.ErrorContains(t, err, "todavía no termina")
}

func TestCreateRejectsDuplicate(t *testing.T) {
	h := newHarness(true, testsupport.FakeSecrets{})
	h.meta.Seed(model.ClientDatabase{Slug: "acme", LastSeenAt: fixedNow})
	_, err := h.svc.Create(context.Background(), CreateRequest{Name: "acme"}, "admin")
	assert.ErrorIs(t, err, model.ErrConflict)
}

func TestCreatePropagatesLookupError(t *testing.T) {
	h := newHarness(true, testsupport.FakeSecrets{})
	h.meta.FailList = true
	_, err := h.svc.Create(context.Background(), CreateRequest{Name: "acme"}, "admin")
	assert.ErrorIs(t, err, testsupport.ErrInjected)
}

func TestCreateSecretFailures(t *testing.T) {
	h := newHarness(true, testsupport.FakeSecrets{FailPassword: true})
	_, err := h.svc.Create(context.Background(), CreateRequest{Name: "acme"}, "admin")
	assert.ErrorContains(t, err, "generate password")

	h = newHarness(true, testsupport.FakeSecrets{FailVerifier: true})
	_, err = h.svc.Create(context.Background(), CreateRequest{Name: "acme"}, "admin")
	assert.ErrorContains(t, err, "hash password")
}

// Each failing step must leave no trace of the previous ones (saga rollback).
func TestCreateRollsBackOnFailure(t *testing.T) {
	cases := map[string]func(h harness){
		"role":       func(h harness) { h.cluster.FailRole = true },
		"database":   func(h harness) { h.cluster.FailDB = true },
		"console":    func(h harness) { h.cluster.FailGrant = true },
		"cloudflare": func(h harness) { h.cf.FailEnsure = true },
		"metadata":   func(h harness) { h.meta.FailInsert = true },
	}
	for name, breakIt := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(true, testsupport.FakeSecrets{})
			breakIt(h)
			_, err := h.svc.Create(context.Background(), CreateRequest{Name: "acme"}, "admin")
			require.Error(t, err)
			assert.Empty(t, h.cluster.Roles, "role left behind")
			assert.Empty(t, h.cluster.Databases, "database left behind")
			assert.Empty(t, h.cf.Routes, "route left behind")
			assert.Contains(t, h.meta.Actions(), config.ActionProvisionFailed)
		})
	}
}

func TestCreateJobFailureRollsBack(t *testing.T) {
	h := newHarness(true, testsupport.FakeSecrets{})
	h.create(t, CreateRequest{Name: "origen"})
	h.meta.FailJob = true
	_, err := h.svc.Create(context.Background(), CreateRequest{Name: "copia", CloneFrom: "origen"}, "admin")
	require.Error(t, err)
	assert.NotContains(t, h.cluster.Databases, "pdb_copia")
}

func TestRollbackFailureIsRecorded(t *testing.T) {
	h := newHarness(true, testsupport.FakeSecrets{})
	h.meta.FailInsert = true
	h.cf.FailRemove = true
	_, err := h.svc.Create(context.Background(), CreateRequest{Name: "acme"}, "admin")
	require.Error(t, err)
	last := h.meta.Audit[len(h.meta.Audit)-1]
	assert.Equal(t, []string{"remove route"}, last.Details["failedRollbackSteps"])
}

func TestListAndGet(t *testing.T) {
	h := newHarness(true, testsupport.FakeSecrets{})
	h.meta.Seed(model.ClientDatabase{Slug: "fresh", LastSeenAt: fixedNow.Add(-day)})
	h.meta.Seed(model.ClientDatabase{Slug: "old", LastSeenAt: fixedNow.Add(-25 * day)})
	h.meta.Seed(model.ClientDatabase{Slug: "gone", State: config.StateExpired, LastSeenAt: fixedNow.Add(-40 * day)})
	h.meta.Seed(model.ClientDatabase{Slug: "keep", NeverExpires: true, LastSeenAt: fixedNow.Add(-400 * day)})
	h.meta.Seed(model.ClientDatabase{Slug: "deleted", State: config.StateDeleted, LastSeenAt: fixedNow})

	views, err := h.svc.List(context.Background())
	require.NoError(t, err)
	got := map[string]string{}
	for _, v := range views {
		got[v.Slug] = v.Status
	}
	assert.Equal(t, map[string]string{
		"fresh": config.StatusActive, "old": config.StatusExpiring, "gone": config.StatusExpired, "keep": config.StatusActive,
	}, got)

	v, err := h.svc.Get(context.Background(), "keep")
	require.NoError(t, err)
	assert.Nil(t, v.ExpiresAt, "never-expiring databases have no deadline")

	_, err = h.svc.Get(context.Background(), "Bad!")
	var invalidErr *InvalidInputError
	assert.ErrorAs(t, err, &invalidErr)

	h.meta.FailList = true
	_, err = h.svc.List(context.Background())
	assert.Error(t, err)
}

func (h harness) seedActive(slug string, lastSeen time.Time) model.ClientDatabase {
	n, _ := shared.Resolve(slug)
	h.cluster.Roles[n.Role] = "v"
	h.cluster.Databases[n.Database] = n.Role
	h.cf.Routes[n.Hostname] = "tcp://db:5432"
	return h.meta.Seed(model.ClientDatabase{Slug: slug, DBName: n.Database, RoleName: n.Role, Hostname: n.Hostname, LastSeenAt: lastSeen})
}

func TestDeleteWithBackupArchivesAndQueuesDump(t *testing.T) {
	h := newHarness(true, testsupport.FakeSecrets{})
	db := h.seedActive("acme", fixedNow)
	_, err := h.svc.CreateUser(context.Background(), "acme", "bi", config.AccessRead, "admin")
	require.NoError(t, err)

	require.NoError(t, h.svc.Delete(context.Background(), "acme", "admin"))

	assert.False(t, h.cf.HasRoute(db.Hostname))
	assert.Equal(t, "pdb_acme", h.cluster.Archived["pdbarch_acme_20261102120000"])
	assert.Equal(t, []string{"pdb_acme__bi"}, h.insp.Dropped, "extra users are revoked before archiving")
	assert.NotContains(t, h.cluster.Roles, "pdb_acme__bi")
	assert.Empty(t, h.meta.Users)
	assert.Equal(t, config.StateDeleted, h.meta.Rows[db.ID].State)
	job := h.meta.Jobs[len(h.meta.Jobs)-1]
	assert.Equal(t, config.JobArchive, job.Kind)
	assert.Equal(t, "pdbarch_acme_20261102120000.dump", job.File)
	assert.Equal(t, config.ReasonDeletion, job.Reason)
}

func TestArchiveJobFailureIsNotFatal(t *testing.T) {
	h := newHarness(true, testsupport.FakeSecrets{})
	h.seedActive("acme", fixedNow)
	h.meta.FailJob = true
	assert.NoError(t, h.svc.Delete(context.Background(), "acme", "admin"), "the sidecar adopts orphaned archives")
}

func TestDeleteWithoutBackupDrops(t *testing.T) {
	h := newHarness(false, testsupport.FakeSecrets{})
	db := h.seedActive("acme", fixedNow)
	_, err := h.svc.CreateUser(context.Background(), "acme", "app", config.AccessWrite, "admin")
	require.NoError(t, err)
	require.NoError(t, h.svc.Delete(context.Background(), "acme", "admin"))
	assert.Empty(t, h.cluster.Databases)
	assert.Empty(t, h.cluster.Roles)
	assert.Empty(t, h.cluster.Archived)
	assert.Equal(t, config.StateDeleted, h.meta.Rows[db.ID].State)
	assert.Contains(t, h.meta.Actions(), config.ActionDBDropped)
}

func TestDeleteExpiredOnlyHidesRow(t *testing.T) {
	h := newHarness(true, testsupport.FakeSecrets{})
	db := h.meta.Seed(model.ClientDatabase{Slug: "old", State: config.StateExpired, LastSeenAt: fixedNow})
	require.NoError(t, h.svc.Delete(context.Background(), "old", "admin"))
	assert.Equal(t, config.StateDeleted, h.meta.Rows[db.ID].State)
	assert.Empty(t, h.cf.Removed)
}

func TestDeleteErrors(t *testing.T) {
	h := newHarness(true, testsupport.FakeSecrets{})
	var invalidErr *InvalidInputError
	assert.ErrorAs(t, h.svc.Delete(context.Background(), "X", "admin"), &invalidErr)
	assert.ErrorIs(t, h.svc.Delete(context.Background(), "missing", "admin"), model.ErrNotFound)

	h.seedActive("acme", fixedNow)
	h.cf.FailRemove = true
	assert.ErrorContains(t, h.svc.Delete(context.Background(), "acme", "admin"), "unpublish")
	h.cf.FailRemove = false

	h.cluster.FailArchive = true
	assert.ErrorContains(t, h.svc.Delete(context.Background(), "acme", "admin"), "archive")
	h.cluster.FailArchive = false

	h.meta.FailUpdate = true
	assert.Error(t, h.svc.Delete(context.Background(), "acme", "admin"))
}

func TestRetireUserFailures(t *testing.T) {
	h := newHarness(true, testsupport.FakeSecrets{})
	h.seedActive("acme", fixedNow)
	_, err := h.svc.CreateUser(context.Background(), "acme", "bi", config.AccessRead, "admin")
	require.NoError(t, err)
	h.insp.FailDrop = true
	assert.ErrorContains(t, h.svc.Delete(context.Background(), "acme", "admin"), "revoke user")

	h = newHarness(true, testsupport.FakeSecrets{})
	h.seedActive("acme", fixedNow)
	h.meta.FailList = true
	assert.Error(t, h.svc.Delete(context.Background(), "acme", "admin"))
}

func TestDeleteDropFailure(t *testing.T) {
	h := newHarness(false, testsupport.FakeSecrets{})
	h.seedActive("acme", fixedNow)
	h.cluster.FailDrop = true
	assert.ErrorContains(t, h.svc.Delete(context.Background(), "acme", "admin"), "drop database")
}

func TestExpire(t *testing.T) {
	h := newHarness(true, testsupport.FakeSecrets{})
	db := h.seedActive("acme", fixedNow.Add(-40*day))
	require.NoError(t, h.svc.Expire(context.Background(), db, config.ActorAuditor))

	row := h.meta.Rows[db.ID]
	assert.Equal(t, config.StateExpired, row.State)
	assert.Equal(t, fixedNow, *row.ExpiredAt)
	assert.False(t, h.cf.HasRoute(db.Hostname))
	assert.Equal(t, config.ActionDBExpired, h.meta.Audit[len(h.meta.Audit)-1].Action)
	assert.Equal(t, config.ReasonExpiry, h.meta.Jobs[0].Reason)
}

func TestExpireErrors(t *testing.T) {
	h := newHarness(true, testsupport.FakeSecrets{})
	db := h.seedActive("acme", fixedNow)
	h.cf.FailRemove = true
	assert.Error(t, h.svc.Expire(context.Background(), db, "x"))
	h.cf.FailRemove = false
	h.meta.FailUpdate = true
	assert.Error(t, h.svc.Expire(context.Background(), db, "x"))
}

func TestMigrateHostnames(t *testing.T) {
	h := newHarness(true, testsupport.FakeSecrets{})
	h.seedActive("acme", fixedNow)
	h.seedActive("beta", fixedNow)
	h.meta.Seed(model.ClientDatabase{Slug: "old", State: config.StateExpired, Hostname: "postern-old.pachislab.com"})
	h.svc.opts.Resolver = naming.Resolver{BaseDomain: "posterndb.com", PanelHostname: "posterndb.com"}

	changes, err := h.svc.MigrateHostnames(context.Background(), true, config.ActorSystem)
	require.NoError(t, err)
	assert.Len(t, changes, 2)
	assert.True(t, h.cf.HasRoute("postern-acme.pachislab.com"), "dry run changes nothing")

	changes, err = h.svc.MigrateHostnames(context.Background(), false, config.ActorSystem)
	require.NoError(t, err)
	assert.ElementsMatch(t, []HostnameChange{
		{Slug: "acme", From: "postern-acme.pachislab.com", To: "acme.posterndb.com"},
		{Slug: "beta", From: "postern-beta.pachislab.com", To: "beta.posterndb.com"},
	}, changes)
	assert.True(t, h.cf.HasRoute("acme.posterndb.com"))
	assert.False(t, h.cf.HasRoute("postern-acme.pachislab.com"))

	changes, err = h.svc.MigrateHostnames(context.Background(), false, config.ActorSystem)
	require.NoError(t, err)
	assert.Empty(t, changes, "second run is a no-op")
}

func TestMigrateFromLocalMode(t *testing.T) {
	h := newHarness(true, testsupport.FakeSecrets{})
	h.meta.Seed(model.ClientDatabase{Slug: "acme", DBName: "pdb_acme", Hostname: ""})
	changes, err := h.svc.MigrateHostnames(context.Background(), false, "x")
	require.NoError(t, err)
	assert.Equal(t, "", changes[0].From)
	assert.Empty(t, h.cf.Removed, "nothing to unpublish")

	local := newHarnessWith(naming.Resolver{Local: true}, true, testsupport.FakeSecrets{})
	_, err = local.svc.MigrateHostnames(context.Background(), false, "x")
	var invalidErr *InvalidInputError
	assert.ErrorAs(t, err, &invalidErr)
}

func TestMigrateHostnamesErrors(t *testing.T) {
	h := newHarness(true, testsupport.FakeSecrets{})
	h.seedActive("acme", fixedNow)
	h.svc.opts.Resolver = naming.Resolver{BaseDomain: "posterndb.com"}

	h.cf.FailEnsure = true
	_, err := h.svc.MigrateHostnames(context.Background(), false, "x")
	assert.ErrorContains(t, err, "migrate acme")
	h.cf.FailEnsure = false

	h.meta.FailUpdate = true
	_, err = h.svc.MigrateHostnames(context.Background(), false, "x")
	assert.Error(t, err)
	h.meta.FailUpdate = false

	h.cf.FailRemove = true
	_, err = h.svc.MigrateHostnames(context.Background(), false, "x")
	assert.Error(t, err)

	h.meta.FailList = true
	_, err = h.svc.MigrateHostnames(context.Background(), false, "x")
	assert.Error(t, err)
}

func TestMigrateHostnamesRejectsCollision(t *testing.T) {
	h := newHarness(true, testsupport.FakeSecrets{})
	h.seedActive("postern", fixedNow)
	h.svc.opts.Resolver = naming.Resolver{BaseDomain: "pachislab.com", PanelHostname: "postern.pachislab.com"}
	_, err := h.svc.MigrateHostnames(context.Background(), false, "x")
	var invalidErr *InvalidInputError
	assert.ErrorAs(t, err, &invalidErr)
}

func TestAuditFailureDoesNotFailOperation(t *testing.T) {
	h := newHarness(true, testsupport.FakeSecrets{})
	h.meta.FailAudit = true
	_, err := h.svc.Create(context.Background(), CreateRequest{Name: "acme"}, "admin")
	assert.NoError(t, err)
	h.svc.RecordAudit(context.Background(), "a", "b", "c", nil)
}

func TestNewDefaults(t *testing.T) {
	s := New(nil, nil, nil, nil, nil, Options{})
	assert.NotNil(t, s.opts.Now)
	assert.NotNil(t, s.opts.Logger)
	e := &InvalidInputError{Err: assert.AnError}
	assert.Equal(t, assert.AnError.Error(), e.Error())
	assert.ErrorIs(t, e, assert.AnError)
}

func TestEffectivePolicyAndGlobalSettings(t *testing.T) {
	h := newHarness(true, testsupport.FakeSecrets{})
	ctx := context.Background()

	// No override yet: effective policy is the .env default.
	assert.Equal(t, h.svc.opts.Policy, h.svc.EffectivePolicy(ctx))
	raw, err := h.svc.GlobalSettings(ctx)
	require.NoError(t, err)
	assert.Nil(t, raw.ExpirationDays)

	days, warn := 90, 14
	saved, err := h.svc.SetGlobalSettings(ctx, &days, &warn, "root")
	require.NoError(t, err)
	assert.Equal(t, 90, *saved.ExpirationDays)
	assert.Equal(t, "root", saved.UpdatedBy)
	assert.Contains(t, h.meta.Actions(), config.ActionGlobalExpiryChanged)

	assert.Equal(t, expiry.Policy{MaxIdle: 90 * day, WarnBefore: 14 * day}, h.svc.EffectivePolicy(ctx))

	// List/Get now resolve expiry against the override, not the .env default.
	h.seedActive("acme", fixedNow.Add(-91*day))
	views, err := h.svc.List(ctx)
	require.NoError(t, err)
	assert.Equal(t, config.StatusExpired, views[0].Status, "91 days idle against a 90-day override")

	// Validation.
	_, err = h.svc.SetGlobalSettings(ctx, ptr(0), nil, "root")
	var invalidErr *InvalidInputError
	assert.ErrorAs(t, err, &invalidErr)
	_, err = h.svc.SetGlobalSettings(ctx, ptr(5), ptr(10), "root")
	assert.ErrorAs(t, err, &invalidErr, "warn days must stay below expiration days")

	// A read failure falls back to the .env default instead of failing List/Get.
	h.meta.FailList = true
	assert.Equal(t, h.svc.opts.Policy, h.svc.EffectivePolicy(ctx))
	_, err = h.svc.GlobalSettings(ctx)
	assert.Error(t, err)
}

func TestCreateViaAdminPanelHasNoClientUser(t *testing.T) {
	h := newHarness(true, testsupport.FakeSecrets{})
	h.create(t, CreateRequest{Name: "acme"})
	row, err := h.meta.GetDatabase(context.Background(), "acme")
	require.NoError(t, err)
	assert.Nil(t, row.ClientUserID, "a database created through the admin panel belongs to no client")
}

func TestCreateForClientRecordsClientUserID(t *testing.T) {
	h := newHarness(true, testsupport.FakeSecrets{})
	ana, err := h.meta.CreateClientUser(context.Background(), model.NewClientUser{Username: "ana", PasswordHash: "hash"})
	require.NoError(t, err)
	_, err = h.svc.Create(context.Background(), CreateRequest{Name: "ana-app", ClientUserID: &ana.ID}, "ana")
	require.NoError(t, err)
	row, err := h.meta.GetDatabase(context.Background(), "ana-app")
	require.NoError(t, err)
	require.NotNil(t, row.ClientUserID)
	assert.Equal(t, ana.ID, *row.ClientUserID)
}

func TestClientExpiryPrecedence(t *testing.T) {
	h := newHarness(true, testsupport.FakeSecrets{})
	ctx := context.Background()

	// ana never expires; her databases should all be unlimited unless one
	// has its own exception.
	ana, err := h.meta.CreateClientUser(ctx, model.NewClientUser{Username: "ana", PasswordHash: "hash"})
	require.NoError(t, err)
	_, err = h.svc.SetClientExpiry(ctx, ana.ID, true, nil, "root")
	require.NoError(t, err)

	anaDB := h.seedActive("ana-app", fixedNow.Add(-400*day))
	h.meta.Rows[anaDB.ID].ClientUserID = &ana.ID

	ownDays := 5
	anaOverride := h.seedActive("ana-strict", fixedNow.Add(-10*day))
	h.meta.Rows[anaOverride.ID].ClientUserID = &ana.ID
	h.meta.Rows[anaOverride.ID].MaxIdleDays = &ownDays

	noOwner := h.seedActive("nobody", fixedNow.Add(-400*day))
	_ = noOwner

	views, err := h.svc.List(ctx)
	require.NoError(t, err)
	status := map[string]string{}
	for _, v := range views {
		status[v.Slug] = v.Status
	}
	assert.Equal(t, config.StatusActive, status["ana-app"], "inherits ana's never-expires exception")
	assert.Equal(t, config.StatusExpired, status["ana-strict"], "its own 5-day exception wins over ana's never-expires (10 days idle)")
	assert.Equal(t, config.StatusExpired, status["nobody"], "no client exception: falls back to the 30-day global default")

	// A database's own exception beats the client's.
	short := 3
	_, err = h.svc.SetExpiry(ctx, "ana-strict", ExpiryRequest{MaxIdleDays: &short}, "admin")
	require.NoError(t, err)
	views, err = h.svc.List(ctx)
	require.NoError(t, err)
	for _, v := range views {
		if v.Slug == "ana-strict" {
			assert.Equal(t, config.StatusExpired, v.Status, "own 3-day exception wins over ana's never-expires")
		}
	}
}

func TestSetClientExpiryValidationAndAudit(t *testing.T) {
	h := newHarness(true, testsupport.FakeSecrets{})
	ctx := context.Background()
	ana, err := h.meta.CreateClientUser(ctx, model.NewClientUser{Username: "ana", PasswordHash: "hash"})
	require.NoError(t, err)

	var invalidErr *InvalidInputError
	_, err = h.svc.SetClientExpiry(ctx, ana.ID, false, ptr(0), "root")
	assert.ErrorAs(t, err, &invalidErr)

	saved, err := h.svc.SetClientExpiry(ctx, ana.ID, false, ptr(45), "root")
	require.NoError(t, err)
	assert.Equal(t, 45, *saved.MaxIdleDays)
	assert.Contains(t, h.meta.Actions(), config.ActionClientExpiryChanged)

	list, err := h.svc.ListClients(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "ana", list[0].Username)

	_, err = h.svc.SetClientExpiry(ctx, 9999, false, nil, "root")
	assert.ErrorIs(t, err, model.ErrNotFound)

	h.meta.FailList = true
	assert.Empty(t, h.svc.clientOverrides(ctx), "a read failure yields an empty map instead of failing the request")
}
