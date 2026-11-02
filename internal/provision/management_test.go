package provision

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tecsharp/posterndb/internal/config"
	"github.com/tecsharp/posterndb/internal/model"
	"github.com/tecsharp/posterndb/internal/testsupport"
)

func ptr[T any](v T) *T { return &v }

func TestResetPassword(t *testing.T) {
	h := newHarness(true, testsupport.FakeSecrets{})
	h.seedActive("acme", fixedNow)
	creds, err := h.svc.ResetPassword(context.Background(), "acme", "admin")
	require.NoError(t, err)
	assert.Equal(t, "pdb_acme", creds.Username)
	assert.Equal(t, "SCRAM-SHA-256$fake:"+creds.Password, h.cluster.Roles["pdb_acme"])
	assert.Contains(t, h.meta.Actions(), config.ActionPasswordReset)

	h.cluster.FailPassword = true
	_, err = h.svc.ResetPassword(context.Background(), "acme", "admin")
	assert.Error(t, err)

	_, err = newHarness(true, testsupport.FakeSecrets{FailPassword: true}).svc.ResetPassword(context.Background(), "acme", "a")
	assert.Error(t, err)
}

func TestOperationsRequireActiveDatabase(t *testing.T) {
	h := newHarness(true, testsupport.FakeSecrets{})
	h.meta.Seed(model.ClientDatabase{Slug: "old", State: config.StateExpired})
	ctx := context.Background()
	_, err := h.svc.ResetPassword(ctx, "old", "a")
	assert.ErrorIs(t, err, ErrNotActive)
	_, err = h.svc.Stats(ctx, "old")
	assert.ErrorIs(t, err, ErrNotActive)
	_, err = h.svc.Backup(ctx, "old", "a")
	assert.ErrorIs(t, err, ErrNotActive)
	_, err = h.svc.ResetPassword(ctx, "missing", "a")
	assert.ErrorIs(t, err, model.ErrNotFound)
}

func TestUsersLifecycle(t *testing.T) {
	h := newHarness(true, testsupport.FakeSecrets{})
	h.seedActive("acme", fixedNow)
	ctx := context.Background()

	creds, err := h.svc.CreateUser(ctx, "acme", " BI ", config.AccessRead, "admin")
	require.NoError(t, err)
	assert.Equal(t, "pdb_acme__bi", creds.Username)
	assert.Equal(t, config.AccessRead, h.insp.Grants["pdb_acme__bi"])

	_, err = h.svc.CreateUser(ctx, "acme", "bi", config.AccessWrite, "admin")
	assert.ErrorIs(t, err, model.ErrConflict)

	users, err := h.svc.ListUsers(ctx, "acme")
	require.NoError(t, err)
	require.Len(t, users, 1)

	reset, err := h.svc.ResetUserPassword(ctx, "acme", "bi", "admin")
	require.NoError(t, err)
	assert.Equal(t, "pdb_acme__bi", reset.Username)

	require.NoError(t, h.svc.DeleteUser(ctx, "acme", "bi", "admin"))
	assert.NotContains(t, h.cluster.Roles, "pdb_acme__bi")
	assert.Empty(t, h.meta.Users)
	assert.ErrorIs(t, h.svc.DeleteUser(ctx, "acme", "bi", "admin"), model.ErrNotFound)
}

func TestCreateUserValidationAndRollback(t *testing.T) {
	h := newHarness(true, testsupport.FakeSecrets{})
	h.seedActive("acme", fixedNow)
	ctx := context.Background()
	var invalidErr *InvalidInputError

	_, err := h.svc.CreateUser(ctx, "acme", "x", config.AccessRead, "a")
	assert.ErrorAs(t, err, &invalidErr)
	_, err = h.svc.CreateUser(ctx, "acme", "bi", "superuser", "a")
	assert.ErrorAs(t, err, &invalidErr)

	h.insp.FailGrant = true
	_, err = h.svc.CreateUser(ctx, "acme", "bi", config.AccessRead, "a")
	assert.ErrorContains(t, err, "grant privileges")
	assert.NotContains(t, h.cluster.Roles, "pdb_acme__bi", "role removed after failed grant")
	h.insp.FailGrant = false

	h.cluster.FailRole = true
	_, err = h.svc.CreateUser(ctx, "acme", "bi", config.AccessRead, "a")
	assert.ErrorContains(t, err, "create user role")
	h.cluster.FailRole = false

	_, err = newHarness(true, testsupport.FakeSecrets{FailVerifier: true}).svc.CreateUser(ctx, "acme", "bi", config.AccessRead, "a")
	assert.Error(t, err)
}

func TestUserOperationErrors(t *testing.T) {
	h := newHarness(true, testsupport.FakeSecrets{})
	h.seedActive("acme", fixedNow)
	ctx := context.Background()
	_, err := h.svc.CreateUser(ctx, "acme", "bi", config.AccessRead, "a")
	require.NoError(t, err)

	var invalidErr *InvalidInputError
	_, err = h.svc.ResetUserPassword(ctx, "acme", "BAD!", "a")
	assert.ErrorAs(t, err, &invalidErr)

	h.cluster.FailPassword = true
	_, err = h.svc.ResetUserPassword(ctx, "acme", "bi", "a")
	assert.Error(t, err)

	h.insp.FailDrop = true
	assert.ErrorContains(t, h.svc.DeleteUser(ctx, "acme", "bi", "a"), "revoke")

	_, err = h.svc.ListUsers(ctx, "Bad!")
	assert.ErrorAs(t, err, &invalidErr)
	_, err = h.svc.ResetUserPassword(ctx, "ghost", "bi", "a")
	assert.ErrorIs(t, err, model.ErrNotFound)
}

func TestSetExpiry(t *testing.T) {
	h := newHarness(true, testsupport.FakeSecrets{})
	h.seedActive("acme", fixedNow)
	ctx := context.Background()

	v, err := h.svc.SetExpiry(ctx, "acme", ExpiryRequest{MaxIdleDays: ptr(90)}, "admin")
	require.NoError(t, err)
	assert.Equal(t, fixedNow.Add(90*day), *v.ExpiresAt)

	v, err = h.svc.SetExpiry(ctx, "acme", ExpiryRequest{NeverExpires: true}, "admin")
	require.NoError(t, err)
	assert.Nil(t, v.ExpiresAt)
	assert.Nil(t, v.MaxIdleDays, "nil restores the global default")

	_, err = h.svc.SetExpiry(ctx, "acme", ExpiryRequest{MaxIdleDays: ptr(0)}, "admin")
	var invalidErr *InvalidInputError
	assert.ErrorAs(t, err, &invalidErr)

	h.meta.FailUpdate = true
	_, err = h.svc.SetExpiry(ctx, "acme", ExpiryRequest{}, "admin")
	assert.Error(t, err)
	_, err = h.svc.SetExpiry(ctx, "ghost", ExpiryRequest{}, "admin")
	assert.Error(t, err)
}

func TestExtend(t *testing.T) {
	h := newHarness(true, testsupport.FakeSecrets{})
	h.seedActive("acme", fixedNow.Add(-20*day)) // expires in 10 days
	ctx := context.Background()

	v, err := h.svc.Extend(ctx, "acme", 30, "admin")
	require.NoError(t, err)
	assert.Equal(t, fixedNow.Add(40*day), *v.ExpiresAt, "extends from the current deadline")

	h2 := newHarness(true, testsupport.FakeSecrets{})
	h2.seedActive("late", fixedNow.Add(-45*day)) // already overdue
	v, err = h2.svc.Extend(ctx, "late", 7, "admin")
	require.NoError(t, err)
	assert.Equal(t, fixedNow.Add(7*day), *v.ExpiresAt, "extends from now when already overdue")

	var invalidErr *InvalidInputError
	_, err = h.svc.Extend(ctx, "acme", 0, "admin")
	assert.ErrorAs(t, err, &invalidErr)
	h.meta.FailUpdate = true
	_, err = h.svc.Extend(ctx, "acme", 5, "admin")
	assert.Error(t, err)
	_, err = h.svc.Extend(ctx, "ghost", 5, "admin")
	assert.Error(t, err)
}

func sampleSchema() model.Schema {
	return model.Schema{Schemas: []string{"public"}, Tables: []model.Table{
		{Schema: "public", Name: "orders", Rows: 120, SizeBytes: 8192},
		{Schema: "public", Name: "customers", Rows: 30, SizeBytes: 4096},
	}}
}

func TestStatsAndSchema(t *testing.T) {
	h := newHarness(true, testsupport.FakeSecrets{})
	h.seedActive("acme", fixedNow)
	h.cluster.Stats = model.DatabaseStats{SizeBytes: 9_000_000, OpenConnections: 2}
	h.insp.SchemaValue = sampleSchema()
	ctx := context.Background()

	st, err := h.svc.Stats(ctx, "acme")
	require.NoError(t, err)
	assert.Equal(t, int64(9_000_000), st.SizeBytes)
	assert.Equal(t, 2, st.TableCount)
	assert.Equal(t, int64(150), st.TotalRows)

	sc, err := h.svc.Schema(ctx, "acme")
	require.NoError(t, err)
	assert.Len(t, sc.Tables, 2)

	h.cluster.FailStats = true
	_, err = h.svc.Stats(ctx, "acme")
	assert.Error(t, err)
	_, err = h.svc.Schema(ctx, "ghost")
	assert.Error(t, err)
}

func TestQueryPreviewAndCount(t *testing.T) {
	h := newHarness(true, testsupport.FakeSecrets{})
	h.seedActive("acme", fixedNow)
	h.insp.Result = model.QueryResult{Columns: []string{"n"}, Rows: [][]any{{"1"}}, RowCount: 1}
	h.insp.Rows = 42
	ctx := context.Background()

	res, err := h.svc.Query(ctx, "acme", strings.Repeat("SELECT 1 ", 50), "admin")
	require.NoError(t, err)
	assert.Equal(t, 1, res.RowCount)
	last := h.meta.Audit[len(h.meta.Audit)-1]
	assert.Equal(t, config.ActionQueryExecuted, last.Action)
	assert.LessOrEqual(t, len(last.Details["sql"].(string)), auditSQLMaxLength+len("…"))

	_, err = h.svc.Preview(ctx, "acme", "public", "orders")
	require.NoError(t, err)
	n, err := h.svc.CountRows(ctx, "acme", "public", "orders")
	require.NoError(t, err)
	assert.Equal(t, int64(42), n)

	h.insp.FailRead = true
	var invalidErr *InvalidInputError
	_, err = h.svc.Query(ctx, "acme", "SELECT 1", "admin")
	assert.ErrorAs(t, err, &invalidErr, "SQL errors are shown to the operator")
	assert.Equal(t, false, h.meta.Audit[len(h.meta.Audit)-1].Details["ok"])
	_, err = h.svc.Preview(ctx, "acme", "public", "orders")
	assert.ErrorAs(t, err, &invalidErr)
	_, err = h.svc.CountRows(ctx, "acme", "public", "orders")
	assert.ErrorAs(t, err, &invalidErr)

	for _, call := range []func() error{
		func() error { _, e := h.svc.Query(ctx, "ghost", "x", "a"); return e },
		func() error { _, e := h.svc.Preview(ctx, "ghost", "s", "t"); return e },
		func() error { _, e := h.svc.CountRows(ctx, "ghost", "s", "t"); return e },
	} {
		assert.ErrorIs(t, call(), model.ErrNotFound)
	}
}

func TestBackups(t *testing.T) {
	h := newHarness(true, testsupport.FakeSecrets{})
	h.seedActive("acme", fixedNow)
	ctx := context.Background()

	job, err := h.svc.Backup(ctx, "acme", "admin")
	require.NoError(t, err)
	assert.Equal(t, "pdb_acme_20261102120000.dump", job.File)
	assert.Equal(t, config.ReasonManual, job.Reason)

	list, err := h.svc.ListBackups(ctx, "acme")
	require.NoError(t, err)
	assert.Empty(t, list, "pending jobs are not downloadable yet")

	_, _, err = h.svc.BackupPath(ctx, job.ID)
	var invalidErr *InvalidInputError
	assert.ErrorAs(t, err, &invalidErr)

	h.meta.SetJobStatus(job.ID, config.JobDone)
	path, _, err := h.svc.BackupPath(ctx, job.ID)
	require.NoError(t, err)
	assert.Equal(t, "/backups/pdb_acme_20261102120000.dump", path)

	jobs, err := h.svc.ListJobs(ctx, "acme")
	require.NoError(t, err)
	assert.Len(t, jobs, 1)
	_, err = h.svc.ListJobs(ctx, "Bad!")
	assert.ErrorAs(t, err, &invalidErr)

	require.NoError(t, h.svc.DeleteBackup(ctx, job.ID, "admin"))
	assert.Equal(t, config.JobDeleteFile, h.meta.Jobs[len(h.meta.Jobs)-1].Kind)

	h.meta.FailJob = true
	_, err = h.svc.Backup(ctx, "acme", "admin")
	assert.Error(t, err)
	assert.Error(t, h.svc.DeleteBackup(ctx, job.ID, "admin"))
	assert.Error(t, h.svc.DeleteBackup(ctx, 999, "admin"))
}

func TestBackupPathRejectsTraversal(t *testing.T) {
	h := newHarness(true, testsupport.FakeSecrets{})
	for _, file := range []string{"../etc/passwd", ".hidden", "a/b.dump", ""} {
		j, _ := h.meta.EnqueueJob(context.Background(), model.Job{Kind: config.JobDump, File: file})
		h.meta.SetJobStatus(j.ID, config.JobDone)
		_, _, err := h.svc.BackupPath(context.Background(), j.ID)
		var invalidErr *InvalidInputError
		assert.ErrorAs(t, err, &invalidErr, file)
	}
}

func TestTemplates(t *testing.T) {
	h := newHarness(true, testsupport.FakeSecrets{})
	ctx := context.Background()

	saved, err := h.svc.SaveTemplate(ctx, model.Template{Name: "  Demo ", SQL: "CREATE TABLE t (id int)"}, "admin")
	require.NoError(t, err)
	assert.Equal(t, "Demo", saved.Name)

	saved.Description = "updated"
	_, err = h.svc.SaveTemplate(ctx, saved, "admin")
	require.NoError(t, err)

	list, err := h.svc.ListTemplates(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "updated", list[0].Description)

	var invalidErr *InvalidInputError
	_, err = h.svc.SaveTemplate(ctx, model.Template{Name: "", SQL: "x"}, "admin")
	assert.ErrorAs(t, err, &invalidErr)
	_, err = h.svc.SaveTemplate(ctx, model.Template{Name: "x", SQL: "  "}, "admin")
	assert.ErrorAs(t, err, &invalidErr)
	h.meta.FailInsert = true
	_, err = h.svc.SaveTemplate(ctx, model.Template{Name: "x", SQL: "y"}, "admin")
	assert.Error(t, err)

	require.NoError(t, h.svc.DeleteTemplate(ctx, saved.ID, "admin"))
	assert.ErrorIs(t, h.svc.DeleteTemplate(ctx, saved.ID, "admin"), model.ErrNotFound)
}

func TestConnectionInfo(t *testing.T) {
	h := newHarness(true, testsupport.FakeSecrets{})
	info := h.svc.ConnectionInfo(model.ClientDatabase{Hostname: "h", DBName: "d", RoleName: "r"})
	assert.Equal(t, model.ConnectionInfo{Hostname: "h", Database: "d", Username: "r", Host: config.LocalHostname, Port: 5432}, info)
}

func TestTruncate(t *testing.T) {
	assert.Equal(t, "abc", truncate("abc", 5))
	assert.Equal(t, "ab…", truncate("abcdef", 2))
}
