//go:build integration

package store

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/tecsharp/posterndb/internal/config"
	"github.com/tecsharp/posterndb/internal/credentials"
	"github.com/tecsharp/posterndb/internal/model"
)

const platformDB = "posterndb"

var (
	pool       *pgxpool.Pool
	adminDSN   string
	containerH string
	containerP string
)

// TestMain starts one PostgreSQL container for the whole package, applies
// the migrations once and tears everything down at the end.
func TestMain(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	ctx := context.Background()
	ctr, err := tcpostgres.Run(ctx, "postgres:17-alpine",
		tcpostgres.WithDatabase(platformDB),
		tcpostgres.WithUsername("posterndb"),
		tcpostgres.WithPassword("integration-secret"),
		testcontainers.WithEnv(map[string]string{
			"POSTGRES_INITDB_ARGS":      "--auth=scram-sha-256",
			"POSTGRES_HOST_AUTH_METHOD": "scram-sha-256",
		}),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		log.Printf("start postgres container: %v", err)
		return 1
	}
	defer func() { _ = testcontainers.TerminateContainer(ctr) }()

	adminDSN, err = ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		log.Printf("connection string: %v", err)
		return 1
	}
	u, _ := url.Parse(adminDSN)
	containerH, containerP = u.Hostname(), u.Port()

	pool, err = Open(ctx, adminDSN, 30*time.Second)
	if err != nil {
		log.Printf("open: %v", err)
		return 1
	}
	defer pool.Close()
	if err := Migrate(ctx, pool); err != nil {
		log.Printf("migrate: %v", err)
		return 1
	}
	if err := NewCluster(pool).Harden(ctx, platformDB); err != nil {
		log.Printf("harden: %v", err)
		return 1
	}
	return m.Run()
}

// connectAs opens a session as a client role, the way a customer would
// after `cloudflared access tcp`.
func connectAs(ctx context.Context, user, password, db string) (*pgx.Conn, error) {
	dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", user, password, containerH, containerP, db)
	return pgx.Connect(ctx, dsn)
}

// provisionClient creates a role (from a SCRAM verifier) and its database.
func provisionClient(t *testing.T, name string) (password string) {
	t.Helper()
	ctx := context.Background()
	gen := credentials.New()
	password, err := gen.Password(config.GeneratedPasswordLn)
	require.NoError(t, err)
	verifier, err := gen.ScramVerifier(password)
	require.NoError(t, err)
	c := NewCluster(pool)
	require.NoError(t, c.CreateRole(ctx, name, verifier))
	require.NoError(t, c.CreateDatabase(ctx, name, name))
	t.Cleanup(func() { _ = c.DropClient(context.Background(), name, name) })
	return password
}

func TestMigrateIsIdempotent(t *testing.T) {
	require.NoError(t, Migrate(context.Background(), pool))
	var n int
	require.NoError(t, pool.QueryRow(context.Background(), "SELECT count(*) FROM schema_migrations").Scan(&n))
	assert.Equal(t, 2, n)
	var templates int
	require.NoError(t, pool.QueryRow(context.Background(), "SELECT count(*) FROM templates").Scan(&templates))
	assert.Equal(t, 1, templates, "demo template seeded once")
}

func TestOpenTimesOut(t *testing.T) {
	_, err := Open(context.Background(), "postgres://x:y@127.0.0.1:1/z?sslmode=disable&connect_timeout=1", time.Second)
	assert.Error(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = Open(ctx, "postgres://x:y@127.0.0.1:1/z?sslmode=disable", time.Minute)
	assert.Error(t, err)
}

func TestClientRoleCanUseOwnDatabase(t *testing.T) {
	ctx := context.Background()
	pass := provisionClient(t, "pdb_it_owner")

	// Logging in proves PostgreSQL accepted our client-side SCRAM verifier.
	conn, err := connectAs(ctx, "pdb_it_owner", pass, "pdb_it_owner")
	require.NoError(t, err)
	defer conn.Close(ctx)

	_, err = conn.Exec(ctx, "CREATE TABLE notes (id serial PRIMARY KEY, body text)")
	require.NoError(t, err)
	_, err = conn.Exec(ctx, "INSERT INTO notes (body) VALUES ('hello')")
	require.NoError(t, err)
	var body string
	require.NoError(t, conn.QueryRow(ctx, "SELECT body FROM notes").Scan(&body))
	assert.Equal(t, "hello", body)

	var super, createdb, createrole bool
	require.NoError(t, conn.QueryRow(ctx,
		"SELECT rolsuper, rolcreatedb, rolcreaterole FROM pg_roles WHERE rolname = current_user").Scan(&super, &createdb, &createrole))
	assert.False(t, super || createdb || createrole, "client role must be least privilege")
}

func TestClientRoleIsIsolated(t *testing.T) {
	ctx := context.Background()
	passA := provisionClient(t, "pdb_it_a")
	provisionClient(t, "pdb_it_b")

	for _, db := range []string{"pdb_it_b", platformDB, "postgres", "template1"} {
		conn, err := connectAs(ctx, "pdb_it_a", passA, db)
		if conn != nil {
			conn.Close(ctx)
		}
		assert.Error(t, err, "client A must not connect to %s", db)
	}

	_, err := connectAs(ctx, "pdb_it_a", "wrong-password", "pdb_it_a")
	assert.Error(t, err)
}

func TestCreateRoleConflict(t *testing.T) {
	provisionClient(t, "pdb_it_dup")
	err := NewCluster(pool).CreateRole(context.Background(), "pdb_it_dup", "SCRAM-SHA-256$4096:x$y:z")
	assert.Error(t, err)
}

func TestActivitySnapshot(t *testing.T) {
	ctx := context.Background()
	c := NewCluster(pool)
	pass := provisionClient(t, "pdb_it_activity")

	before, err := c.ActivitySnapshot(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), before["pdb_it_activity"].Connections)

	conn, err := connectAs(ctx, "pdb_it_activity", pass, "pdb_it_activity")
	require.NoError(t, err)

	during, err := c.ActivitySnapshot(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), during["pdb_it_activity"].Connections, "open session visible in pg_stat_activity")
	conn.Close(ctx)

	// pg_stat_database counters are flushed asynchronously.
	require.Eventually(t, func() bool {
		after, err := c.ActivitySnapshot(ctx)
		return err == nil && after["pdb_it_activity"].Connections == 0 &&
			after["pdb_it_activity"].Sessions > before["pdb_it_activity"].Sessions
	}, 10*time.Second, 200*time.Millisecond, "closed session still counted in pg_stat_database.sessions")

	_, ok := during[platformDB]
	assert.False(t, ok, "only client databases are reported")
}

func TestArchiveClient(t *testing.T) {
	ctx := context.Background()
	c := NewCluster(pool)
	pass := provisionClient(t, "pdb_it_arch")
	conn, err := connectAs(ctx, "pdb_it_arch", pass, "pdb_it_arch")
	require.NoError(t, err)
	_, err = conn.Exec(ctx, "CREATE TABLE keep (v int); INSERT INTO keep VALUES (42)")
	require.NoError(t, err)

	archive := "pdbarch_it_arch_20261102120000"
	require.NoError(t, c.ArchiveClient(ctx, "pdb_it_arch", "pdb_it_arch", archive))
	t.Cleanup(func() { _ = c.DropClient(context.Background(), archive, archive) })

	assert.Error(t, conn.Ping(ctx), "open session was terminated")
	var exists, canLogin bool
	require.NoError(t, pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", archive).Scan(&exists))
	assert.True(t, exists, "data kept for the backup sidecar")
	require.NoError(t, pool.QueryRow(ctx, "SELECT rolcanlogin FROM pg_roles WHERE rolname = $1", archive).Scan(&canLogin))
	assert.False(t, canLogin)

	_, err = connectAs(ctx, "pdb_it_arch", pass, archive)
	assert.Error(t, err, "old credentials no longer work")
}

func TestDropClient(t *testing.T) {
	ctx := context.Background()
	c := NewCluster(pool)
	pass := provisionClient(t, "pdb_it_drop")
	conn, err := connectAs(ctx, "pdb_it_drop", pass, "pdb_it_drop")
	require.NoError(t, err)
	defer conn.Close(ctx)

	require.NoError(t, c.DropClient(ctx, "pdb_it_drop", "pdb_it_drop"))
	var dbs, roles int
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM pg_database WHERE datname = 'pdb_it_drop'").Scan(&dbs))
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM pg_roles WHERE rolname = 'pdb_it_drop'").Scan(&roles))
	assert.Zero(t, dbs+roles)

	assert.NoError(t, c.DropClient(ctx, "pdb_it_drop", "pdb_it_drop"), "idempotent")
	assert.NoError(t, c.DropRole(ctx, "pdb_it_missing"))
}

func TestMetadataLifecycle(t *testing.T) {
	ctx := context.Background()
	m := NewMetadata(pool)
	seen := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

	row, err := m.InsertDatabase(ctx, model.ClientDatabase{
		Slug: "it-meta", DBName: "pdb_it_meta", RoleName: "pdb_it_meta", Hostname: "postern-it-meta.example.com", LastSeenAt: seen,
	})
	require.NoError(t, err)
	assert.Equal(t, config.StateActive, row.State)
	assert.True(t, row.LastSeenAt.Equal(seen))

	_, err = m.InsertDatabase(ctx, model.ClientDatabase{Slug: "it-meta", DBName: "x", RoleName: "x", Hostname: "y", LastSeenAt: seen})
	assert.ErrorIs(t, err, model.ErrConflict, "slug unique while not deleted")

	got, err := m.GetDatabase(ctx, "it-meta")
	require.NoError(t, err)
	assert.Equal(t, row.ID, got.ID)

	later := seen.Add(48 * time.Hour)
	require.NoError(t, m.RecordActivity(ctx, row.ID, later, 9, 0))
	require.NoError(t, m.UpdateBaseline(ctx, row.ID, 11, 2))
	require.NoError(t, m.UpdateHostname(ctx, row.ID, "it-meta.example.com"))
	got, _ = m.GetDatabase(ctx, "it-meta")
	assert.True(t, got.LastSeenAt.Equal(later))
	assert.Equal(t, int64(11), got.SessionsBaseline)
	assert.Equal(t, "it-meta.example.com", got.Hostname)

	active, err := m.ListActive(ctx)
	require.NoError(t, err)
	assert.True(t, containsSlug(active, "it-meta"))

	require.NoError(t, m.MarkExpired(ctx, row.ID, "pdbarch_it_meta_1", later))
	got, _ = m.GetDatabase(ctx, "it-meta")
	assert.Equal(t, config.StateExpired, got.State)
	assert.Equal(t, "pdbarch_it_meta_1", got.ArchivedAs)
	active, _ = m.ListActive(ctx)
	assert.False(t, containsSlug(active, "it-meta"))

	all, err := m.ListDatabases(ctx)
	require.NoError(t, err)
	assert.True(t, containsSlug(all, "it-meta"))

	require.NoError(t, m.MarkDeleted(ctx, row.ID, "", later))
	_, err = m.GetDatabase(ctx, "it-meta")
	assert.ErrorIs(t, err, model.ErrNotFound)
	all, _ = m.ListDatabases(ctx)
	assert.False(t, containsSlug(all, "it-meta"))

	_, err = m.InsertDatabase(ctx, model.ClientDatabase{Slug: "it-meta", DBName: "pdb_it_meta", RoleName: "pdb_it_meta", Hostname: "z", LastSeenAt: seen})
	assert.NoError(t, err, "slug reusable after deletion")

	assert.ErrorIs(t, m.RecordActivity(ctx, 999999, later, 0, 0), model.ErrNotFound)
}

func TestAuditLog(t *testing.T) {
	ctx := context.Background()
	m := NewMetadata(pool)
	require.NoError(t, m.RecordAudit(ctx, model.AuditEntry{Actor: "it", Action: config.ActionDBCreated, Target: "it-audit",
		Details: map[string]any{"hostname": "h.example.com"}}))
	require.NoError(t, m.RecordAudit(ctx, model.AuditEntry{Actor: "it", Action: config.ActionAuditRun}))

	entries, err := m.ListAudit(ctx, 2)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	assert.Equal(t, config.ActionAuditRun, entries[0].Action, "newest first")
	assert.Equal(t, "h.example.com", entries[1].Details["hostname"])
	assert.Empty(t, entries[0].Details)

	require.Error(t, m.RecordAudit(ctx, model.AuditEntry{Details: map[string]any{"bad": make(chan int)}}))
}

func TestErrorsOnClosedPool(t *testing.T) {
	ctx := context.Background()
	closed, err := pgxpool.New(ctx, adminDSN)
	require.NoError(t, err)
	closed.Close()

	m := NewMetadata(closed)
	_, err = m.ListDatabases(ctx)
	assert.Error(t, err)
	_, err = m.ListAudit(ctx, 1)
	assert.Error(t, err)
	c := NewCluster(closed)
	_, err = c.ActivitySnapshot(ctx)
	assert.Error(t, err)
	assert.Error(t, c.Harden(ctx, platformDB))
	assert.Error(t, c.CreateDatabase(ctx, "pdb_x", "pdb_x"))
	assert.Error(t, c.DropClient(ctx, "pdb_x", "pdb_x"))
	assert.Error(t, c.ArchiveClient(ctx, "pdb_x", "pdb_x", "pdbarch_x"))
	assert.Error(t, Migrate(ctx, closed))
}

func containsSlug(dbs []model.ClientDatabase, slug string) bool {
	for _, d := range dbs {
		if d.Slug == slug {
			return true
		}
	}
	return false
}
