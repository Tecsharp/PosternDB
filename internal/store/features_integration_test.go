//go:build integration

package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tecsharp/posterndb/internal/config"
	"github.com/tecsharp/posterndb/internal/credentials"
	"github.com/tecsharp/posterndb/internal/model"
)

// newInspector sets up the console role exactly like main.go does.
func newInspector(t *testing.T) *Inspector {
	t.Helper()
	gen := credentials.New()
	pass, err := gen.Password(32)
	require.NoError(t, err)
	verifier, err := gen.ScramVerifier(pass)
	require.NoError(t, err)
	require.NoError(t, NewCluster(pool).EnsureConsoleRole(context.Background(), verifier))
	insp, err := NewInspector(adminDSN, pass)
	require.NoError(t, err)
	return insp
}

// clientWithConsole provisions a database the console can reach.
func clientWithConsole(t *testing.T, name string) (string, *Inspector) {
	t.Helper()
	pass := provisionClient(t, name)
	insp := newInspector(t)
	require.NoError(t, NewCluster(pool).GrantConsoleAccess(context.Background(), name, name))
	return pass, insp
}

const shopSchema = `
CREATE TABLE customers (id BIGSERIAL PRIMARY KEY, name TEXT NOT NULL);
CREATE TABLE orders (
    id BIGSERIAL PRIMARY KEY,
    customer_id BIGINT NOT NULL REFERENCES customers (id),
    total NUMERIC(10,2) DEFAULT 0
);
CREATE INDEX orders_customer ON orders (customer_id);
CREATE VIEW big_orders AS SELECT * FROM orders WHERE total > 100;
COMMENT ON TABLE orders IS 'pedidos';
INSERT INTO customers (name) VALUES ('Ana'), ('Bruno');
INSERT INTO orders (customer_id, total) VALUES (1, 50), (1, 150), (2, 20);
ANALYZE;`

func TestApplySQLAndSchema(t *testing.T) {
	ctx := context.Background()
	_, insp := clientWithConsole(t, "pdb_it_shop")
	require.NoError(t, insp.ApplySQL(ctx, "pdb_it_shop", "pdb_it_shop", shopSchema))

	owner, err := insp.Query(ctx, "pdb_it_shop", "pdb_it_shop", "SELECT tableowner FROM pg_tables WHERE tablename = 'orders'")
	require.NoError(t, err)
	assert.Equal(t, "pdb_it_shop", owner.Rows[0][0], "template objects belong to the client role")

	sc, err := insp.Schema(ctx, "pdb_it_shop")
	require.NoError(t, err)
	assert.Equal(t, []string{"public"}, sc.Schemas)
	byName := map[string]model.Table{}
	for _, tb := range sc.Tables {
		byName[tb.Name] = tb
	}
	require.Contains(t, byName, "orders")
	orders := byName["orders"]
	assert.Equal(t, "table", orders.Kind)
	assert.Equal(t, "pedidos", orders.Comment)
	assert.Equal(t, int64(3), orders.Rows)
	require.Len(t, orders.ForeignKeys, 1)
	assert.Equal(t, "customers", orders.ForeignKeys[0].RefTable)
	assert.Equal(t, []string{"customer_id"}, orders.ForeignKeys[0].Columns)
	assert.Equal(t, []string{"id"}, orders.ForeignKeys[0].RefColumns)
	assert.True(t, orders.Columns[0].PrimaryKey)
	assert.Equal(t, "numeric(10,2)", orders.Columns[2].Type)
	assert.Len(t, orders.Indexes, 2)
	assert.Equal(t, "view", byName["big_orders"].Kind)

	count, rows, top, err := insp.TableSummary(ctx, "pdb_it_shop")
	require.NoError(t, err)
	assert.Equal(t, 2, count, "views are not counted")
	assert.Equal(t, int64(5), rows)
	assert.Len(t, top, 2)
}

func TestConsoleQueries(t *testing.T) {
	ctx := context.Background()
	_, insp := clientWithConsole(t, "pdb_it_console")
	require.NoError(t, insp.ApplySQL(ctx, "pdb_it_console", "pdb_it_console", shopSchema))

	res, err := insp.Query(ctx, "pdb_it_console", "pdb_it_console", "SELECT id, name, NULL AS nothing FROM customers ORDER BY id")
	require.NoError(t, err)
	assert.Equal(t, []string{"id", "name", "nothing"}, res.Columns)
	assert.Equal(t, [][]any{{"1", "Ana", nil}, {"2", "Bruno", nil}}, res.Rows)

	res, err = insp.Query(ctx, "pdb_it_console", "pdb_it_console", "SELECT generate_series(1, 600)")
	require.NoError(t, err)
	assert.True(t, res.Truncated)
	assert.Equal(t, config.ConsoleRowLimit, res.RowCount)

	prev, err := insp.Preview(ctx, "pdb_it_console", "pdb_it_console", "public", "orders")
	require.NoError(t, err)
	assert.Equal(t, 3, prev.RowCount)
	n, err := insp.CountRows(ctx, "pdb_it_console", "pdb_it_console", "public", "orders")
	require.NoError(t, err)
	assert.Equal(t, int64(3), n)

	_, err = insp.Query(ctx, "pdb_it_console", "pdb_it_console", "   ")
	assert.ErrorIs(t, err, ErrInvalidQuery)
}

// Security: everything an operator can type into the console must stay
// read-only and confined to the client's own privileges.
func TestConsoleIsReadOnlyAndUnprivileged(t *testing.T) {
	ctx := context.Background()
	_, insp := clientWithConsole(t, "pdb_it_guard")
	require.NoError(t, insp.ApplySQL(ctx, "pdb_it_guard", "pdb_it_guard", shopSchema))
	q := func(sql string) error {
		_, err := insp.Query(ctx, "pdb_it_guard", "pdb_it_guard", sql)
		return err
	}
	assert.Error(t, q("DELETE FROM orders"), "writes are rejected")
	assert.Error(t, q("CREATE TABLE x (id int)"), "DDL is rejected")
	assert.Error(t, q("SELECT 1; DELETE FROM orders"), "only one statement per request")
	assert.Error(t, q("COPY (SELECT 1) TO PROGRAM 'id'"), "no shell access")
	assert.Error(t, q("SELECT pg_read_file('/etc/passwd')"), "no server file access")
	assert.Error(t, q("SELECT set_config('role', 'posterndb', false)"), "cannot become the platform superuser")
	assert.Error(t, q("SELECT pg_sleep(30)"), "statement timeout applies")

	n, err := insp.CountRows(ctx, "pdb_it_guard", "pdb_it_guard", "public", "orders")
	require.NoError(t, err)
	assert.Equal(t, int64(3), n, "nothing was deleted")
}

func TestConsoleCannotReachOtherDatabases(t *testing.T) {
	ctx := context.Background()
	_, insp := clientWithConsole(t, "pdb_it_mine")
	provisionClient(t, "pdb_it_theirs") // no console grant
	_, err := insp.Query(ctx, "pdb_it_theirs", "pdb_it_theirs", "SELECT 1")
	assert.Error(t, err)
	_, err = insp.Query(ctx, platformDB, "pdb_it_mine", "SELECT 1")
	assert.Error(t, err)
}

func TestExtraUsers(t *testing.T) {
	ctx := context.Background()
	_, insp := clientWithConsole(t, "pdb_it_team")
	require.NoError(t, insp.ApplySQL(ctx, "pdb_it_team", "pdb_it_team", shopSchema))
	c := NewCluster(pool)
	gen := credentials.New()

	mk := func(role, access string) string {
		pass, _ := gen.Password(32)
		verifier, _ := gen.ScramVerifier(pass)
		require.NoError(t, c.CreateUserRole(ctx, role, "pdb_it_team", verifier))
		require.NoError(t, insp.GrantUser(ctx, "pdb_it_team", "pdb_it_team", role, access))
		return pass
	}
	readPass := mk("pdb_it_team__bi", config.AccessRead)
	writePass := mk("pdb_it_team__app", config.AccessWrite)

	ro, err := connectAs(ctx, "pdb_it_team__bi", readPass, "pdb_it_team")
	require.NoError(t, err)
	defer ro.Close(ctx)
	var total int
	require.NoError(t, ro.QueryRow(ctx, "SELECT count(*) FROM orders").Scan(&total))
	assert.Equal(t, 3, total)
	_, err = ro.Exec(ctx, "DELETE FROM orders")
	assert.Error(t, err, "read users cannot write")

	rw, err := connectAs(ctx, "pdb_it_team__app", writePass, "pdb_it_team")
	require.NoError(t, err)
	_, err = rw.Exec(ctx, "INSERT INTO customers (name) VALUES ('Carla')")
	assert.NoError(t, err, "write users can insert (sequence usage granted)")
	_, err = rw.Exec(ctx, "DROP TABLE orders")
	assert.Error(t, err, "write users cannot drop the owner's tables")
	rw.Close(ctx)

	// Tables created later by the owner are covered by default privileges.
	require.NoError(t, insp.ApplySQL(ctx, "pdb_it_team", "pdb_it_team", "CREATE TABLE later (id int); INSERT INTO later VALUES (1)"))
	require.NoError(t, ro.QueryRow(ctx, "SELECT count(*) FROM later").Scan(&total))

	// Removal: revoke inside the database, then drop the role.
	require.NoError(t, insp.DropUserObjects(ctx, "pdb_it_team", "pdb_it_team", "pdb_it_team__app"))
	require.NoError(t, c.DropRole(ctx, "pdb_it_team__app"))
	_, err = connectAs(ctx, "pdb_it_team__app", writePass, "pdb_it_team")
	assert.Error(t, err)

	newPass, _ := gen.Password(32)
	verifier, _ := gen.ScramVerifier(newPass)
	require.NoError(t, c.SetPassword(ctx, "pdb_it_team__bi", verifier))
	_, err = connectAs(ctx, "pdb_it_team__bi", readPass, "pdb_it_team")
	assert.Error(t, err, "old password rejected after reset")
	conn, err := connectAs(ctx, "pdb_it_team__bi", newPass, "pdb_it_team")
	require.NoError(t, err)
	conn.Close(ctx)
	require.NoError(t, insp.DropUserObjects(ctx, "pdb_it_team", "pdb_it_team", "pdb_it_team__bi"))
	require.NoError(t, c.DropRole(ctx, "pdb_it_team__bi"))
}

func TestStatsAndInternalSessions(t *testing.T) {
	ctx := context.Background()
	_, insp := clientWithConsole(t, "pdb_it_stats")
	m := NewMetadata(pool)
	row, err := m.InsertDatabase(ctx, model.ClientDatabase{Slug: "it-stats", DBName: "pdb_it_stats", RoleName: "pdb_it_stats",
		Hostname: "it-stats.example.com", LastSeenAt: time.Now()})
	require.NoError(t, err)
	insp.OnConnect = func(ctx context.Context, db string) { _ = m.IncrementInternalSessions(ctx, db) }

	_, err = insp.Schema(ctx, "pdb_it_stats")
	require.NoError(t, err)
	got, _ := m.GetDatabase(ctx, "it-stats")
	assert.Equal(t, int64(1), got.InternalSessions)

	st, err := NewCluster(pool).DatabaseStats(ctx, "pdb_it_stats")
	require.NoError(t, err)
	assert.Greater(t, st.SizeBytes, int64(0))

	snapshot, err := NewCluster(pool).ActivitySnapshot(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), snapshot["pdb_it_stats"].Connections)

	_, err = NewCluster(pool).DatabaseStats(ctx, "pdb_missing")
	assert.ErrorIs(t, err, model.ErrNotFound)
	require.NoError(t, m.MarkDeleted(ctx, row.ID, "", time.Now()))
}

func TestExpiryColumns(t *testing.T) {
	ctx := context.Background()
	m := NewMetadata(pool)
	row, err := m.InsertDatabase(ctx, model.ClientDatabase{Slug: "it-expiry", DBName: "pdb_it_expiry", RoleName: "r",
		Hostname: "", LastSeenAt: time.Now()})
	require.NoError(t, err)
	_, err = m.InsertDatabase(ctx, model.ClientDatabase{Slug: "it-expiry2", DBName: "pdb_it_expiry2", RoleName: "r2",
		Hostname: "", LastSeenAt: time.Now()})
	require.NoError(t, err, "empty hostnames (local mode) do not collide")

	days := 90
	grace := time.Now().Add(48 * time.Hour).UTC().Truncate(time.Second)
	require.NoError(t, m.SetExpiry(ctx, row.ID, true, &days, &grace))
	require.NoError(t, m.MarkWarned(ctx, row.ID, time.Now()))
	got, _ := m.GetDatabase(ctx, "it-expiry")
	assert.True(t, got.NeverExpires)
	assert.Equal(t, 90, *got.MaxIdleDays)
	assert.True(t, got.GraceUntil.Equal(grace))
	assert.NotNil(t, got.ExpiryWarnedAt)
	require.NoError(t, m.RecordActivity(ctx, row.ID, time.Now(), 1, 0))
	got, _ = m.GetDatabase(ctx, "it-expiry")
	assert.Nil(t, got.ExpiryWarnedAt, "activity re-arms the warning")
}

func TestAdminsUsersJobsTemplatesRepositories(t *testing.T) {
	ctx := context.Background()
	m := NewMetadata(pool)

	a, err := m.CreateAdmin(ctx, model.Admin{Username: "it-ana", PasswordHash: "h", Role: config.RoleAdmin, CreatedBy: "root"})
	require.NoError(t, err)
	_, err = m.CreateAdmin(ctx, model.Admin{Username: "it-ana", PasswordHash: "h", Role: config.RoleAdmin})
	assert.ErrorIs(t, err, model.ErrConflict)
	require.NoError(t, m.UpdateAdminRole(ctx, "it-ana", config.RoleViewer))
	require.NoError(t, m.UpdateAdminPassword(ctx, "it-ana", "h2"))
	require.NoError(t, m.TouchAdminLogin(ctx, "it-ana", time.Now()))
	got, err := m.GetAdmin(ctx, "it-ana")
	require.NoError(t, err)
	assert.Equal(t, config.RoleViewer, got.Role)
	assert.Equal(t, "h2", got.PasswordHash)
	assert.NotNil(t, got.LastLoginAt)
	list, err := m.ListAdmins(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, list)
	require.NoError(t, m.DeleteAdmin(ctx, a.Username))
	assert.ErrorIs(t, m.DeleteAdmin(ctx, a.Username), model.ErrNotFound)

	db, err := m.InsertDatabase(ctx, model.ClientDatabase{Slug: "it-repo", DBName: "pdb_it_repo", RoleName: "pdb_it_repo",
		Hostname: "it-repo.example.com", LastSeenAt: time.Now()})
	require.NoError(t, err)
	u, err := m.InsertUser(ctx, model.DatabaseUser{DatabaseID: db.ID, Name: "bi", RoleName: "pdb_it_repo__bi", Access: config.AccessRead})
	require.NoError(t, err)
	_, err = m.InsertUser(ctx, model.DatabaseUser{DatabaseID: db.ID, Name: "bi", RoleName: "other", Access: config.AccessRead})
	assert.ErrorIs(t, err, model.ErrConflict)
	users, err := m.ListUsers(ctx, db.ID)
	require.NoError(t, err)
	assert.Len(t, users, 1)
	_, err = m.GetUser(ctx, db.ID, "bi")
	require.NoError(t, err)
	require.NoError(t, m.DeleteUser(ctx, u.ID))
	_, err = m.GetUser(ctx, db.ID, "bi")
	assert.ErrorIs(t, err, model.ErrNotFound)

	dump, err := m.EnqueueJob(ctx, model.Job{Kind: config.JobDump, Slug: "it-repo", SourceDB: "pdb_it_repo", File: "it.dump", RequestedBy: "root"})
	require.NoError(t, err)
	assert.Equal(t, config.JobPending, dump.Status)
	backups, _ := m.ListBackups(ctx, "it-repo")
	assert.Empty(t, backups, "pending dumps are not listed")
	_, err = pool.Exec(ctx, "UPDATE jobs SET status = 'done' WHERE id = $1", dump.ID)
	require.NoError(t, err)
	backups, _ = m.ListBackups(ctx, "it-repo")
	assert.Len(t, backups, 1)
	all, _ := m.ListBackups(ctx, "")
	assert.NotEmpty(t, all)
	_, err = m.EnqueueJob(ctx, model.Job{Kind: config.JobDeleteFile, Slug: "it-repo", File: "it.dump"})
	require.NoError(t, err)
	backups, _ = m.ListBackups(ctx, "it-repo")
	assert.Empty(t, backups, "files scheduled for deletion disappear")
	jobs, err := m.ListJobs(ctx, "it-repo")
	require.NoError(t, err)
	assert.Len(t, jobs, 2)
	_, err = m.GetJob(ctx, dump.ID)
	require.NoError(t, err)
	_, err = m.EnqueueJob(ctx, model.Job{Kind: "bogus"})
	assert.Error(t, err)

	tpl, err := m.SaveTemplate(ctx, model.Template{Name: "it-tpl", SQL: "SELECT 1"})
	require.NoError(t, err)
	tpl.Description = "x"
	_, err = m.SaveTemplate(ctx, tpl)
	require.NoError(t, err)
	_, err = m.SaveTemplate(ctx, model.Template{ID: 999999, Name: "ghost", SQL: "x"})
	assert.ErrorIs(t, err, model.ErrNotFound)
	tpls, err := m.ListTemplates(ctx)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(tpls), 2)
	got2, err := m.GetTemplate(ctx, tpl.ID)
	require.NoError(t, err)
	assert.Equal(t, "x", got2.Description)
	require.NoError(t, m.DeleteTemplate(ctx, tpl.ID))
	assert.ErrorIs(t, m.DeleteTemplate(ctx, tpl.ID), model.ErrNotFound)
}
