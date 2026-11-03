package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tecsharp/posterndb/internal/config"
	"github.com/tecsharp/posterndb/internal/model"
)

// Cluster performs server-wide operations on the shared PostgreSQL instance.
//
// Security model: every client gets one LOGIN role that owns exactly one
// database. CONNECT is revoked from PUBLIC on every database (client and
// system ones), so a client role can only open sessions on its own database
// even though all clients share one server and one tunnel endpoint.
type Cluster struct {
	pool *pgxpool.Pool
}

// NewCluster returns cluster operations executed through the given pool. The
// pool user must be able to CREATE DATABASE / ROLE (the POSTGRES_USER
// superuser created by the official image, reachable only inside Docker).
func NewCluster(pool *pgxpool.Pool) *Cluster {
	return &Cluster{pool: pool}
}

// Harden revokes the default PUBLIC CONNECT privilege on system and platform
// databases. Without this, any client role could connect to "postgres" or
// to the metadata database and read pg_catalog. Idempotent; run at startup.
func (c *Cluster) Harden(ctx context.Context, platformDB string) error {
	for _, db := range []string{platformDB, "postgres", "template1"} {
		if _, err := c.pool.Exec(ctx, "REVOKE CONNECT, TEMPORARY ON DATABASE "+ident(db)+" FROM PUBLIC"); err != nil {
			return fmt.Errorf("harden %s: %w", db, err)
		}
	}
	return nil
}

// CreateRole creates a least-privilege login role. scramVerifier is a
// pre-computed "SCRAM-SHA-256$..." string, never a plaintext password.
func (c *Cluster) CreateRole(ctx context.Context, role, scramVerifier string) error {
	sql := fmt.Sprintf(`CREATE ROLE %s WITH LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE
		NOREPLICATION NOBYPASSRLS NOINHERIT CONNECTION LIMIT %d PASSWORD %s`,
		ident(role), config.ClientConnLimit, literal(scramVerifier))
	_, err := c.pool.Exec(ctx, sql)
	return translate(err)
}

// CreateDatabase creates a database owned by role and closes it to PUBLIC.
// The owner keeps CONNECT implicitly, so only that role (and the platform
// superuser) can connect. CREATE DATABASE cannot run inside a transaction,
// which is why provisioning uses compensating actions instead.
func (c *Cluster) CreateDatabase(ctx context.Context, db, owner string) error {
	if _, err := c.pool.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s OWNER %s", ident(db), ident(owner))); err != nil {
		return translate(err)
	}
	_, err := c.pool.Exec(ctx, "REVOKE ALL ON DATABASE "+ident(db)+" FROM PUBLIC")
	return err
}

// DropClient permanently removes a client database and its role.
func (c *Cluster) DropClient(ctx context.Context, db, role string) error {
	if err := c.disableLogin(ctx, role); err != nil {
		return err
	}
	// WITH (FORCE) terminates remaining sessions (PostgreSQL 13+).
	if _, err := c.pool.Exec(ctx, "DROP DATABASE IF EXISTS "+ident(db)+" WITH (FORCE)"); err != nil {
		return err
	}
	_, err := c.pool.Exec(ctx, "DROP ROLE IF EXISTS "+ident(role))
	return err
}

// DropRole removes a role that owns nothing (used to undo a failed create).
func (c *Cluster) DropRole(ctx context.Context, role string) error {
	_, err := c.pool.Exec(ctx, "DROP ROLE IF EXISTS "+ident(role))
	return err
}

// ArchiveClient takes a database out of service without destroying data:
// login is disabled, sessions are terminated and both the database and the
// role are renamed to archiveName. The backup sidecar later pg_dumps every
// "pdbarch_*" database and drops it together with its same-named role.
// Renaming the role (instead of dropping it) avoids having to reassign
// ownership of every object inside the database.
func (c *Cluster) ArchiveClient(ctx context.Context, db, role, archiveName string) error {
	if err := c.disableLogin(ctx, role); err != nil {
		return err
	}
	if _, err := c.pool.Exec(ctx, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity
		WHERE datname = $1 AND pid <> pg_backend_pid()`, db); err != nil {
		return err
	}
	if _, err := c.pool.Exec(ctx, "ALTER DATABASE "+ident(db)+" RENAME TO "+ident(archiveName)); err != nil {
		return err
	}
	_, err := c.pool.Exec(ctx, "ALTER ROLE "+ident(role)+" RENAME TO "+ident(archiveName))
	return err
}

func (c *Cluster) disableLogin(ctx context.Context, role string) error {
	var exists bool
	if err := c.pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)", role).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return nil
	}
	_, err := c.pool.Exec(ctx, "ALTER ROLE "+ident(role)+" NOLOGIN")
	return err
}

// activitySQL combines two signals per client database:
//
//   - pg_stat_activity: sessions open right now. Catches long-lived pools
//     (e.g. a Spring Boot HikariCP pool) that never open new connections.
//   - pg_stat_database.sessions (PostgreSQL 14+): cumulative count of
//     sessions ever established. Comparing it with the previous run catches
//     short-lived clients that connected and left between two checks, which
//     a point-in-time pg_stat_activity snapshot every few hours would miss.
//
// Only "client backend" rows count, so autovacuum and other background
// workers never keep an abandoned database alive. The platform connects to
// its own metadata database, so these checks do not create false activity.
const activitySQL = `
SELECT d.datname,
       COALESCE(a.open_sessions, 0),
       COALESCE(s.sessions, 0)
FROM pg_database d
LEFT JOIN (
    SELECT datname, count(*) AS open_sessions
    FROM pg_stat_activity
    WHERE backend_type = 'client backend' AND pid <> pg_backend_pid()
      AND application_name NOT IN ($2, $3)
    GROUP BY datname
) a ON a.datname = d.datname
LEFT JOIN pg_stat_database s ON s.datname = d.datname
WHERE starts_with(d.datname, $1)`

// ActivitySnapshot returns current activity for every client database.
func (c *Cluster) ActivitySnapshot(ctx context.Context) (map[string]model.Activity, error) {
	rows, err := c.pool.Query(ctx, activitySQL, config.ClientDBPrefix, config.AppNameInspector, config.AppNameConsole)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]model.Activity{}
	for rows.Next() {
		var name string
		var a model.Activity
		if err := rows.Scan(&name, &a.Connections, &a.Sessions); err != nil {
			return nil, err
		}
		out[name] = a
	}
	return out, rows.Err()
}

// EnsureConsoleRole creates or updates the console role. Its password is
// regenerated at every start and only kept in memory, so it never needs to
// be configured or stored anywhere.
func (c *Cluster) EnsureConsoleRole(ctx context.Context, scramVerifier string) error {
	role := ident(config.ConsoleRole)
	attrs := "LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS NOINHERIT CONNECTION LIMIT 20 PASSWORD " + literal(scramVerifier)
	var exists bool
	if err := c.pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)", config.ConsoleRole).Scan(&exists); err != nil {
		return err
	}
	verb := "CREATE"
	if exists {
		verb = "ALTER"
	}
	_, err := c.pool.Exec(ctx, fmt.Sprintf("%s ROLE %s WITH %s", verb, role, attrs))
	return err
}

// GrantConsoleAccess lets the console role connect to database and switch
// to its owner role (SET ROLE). NOINHERIT means it never holds the owner's
// privileges implicitly. Idempotent.
func (c *Cluster) GrantConsoleAccess(ctx context.Context, database, owner string) error {
	stmt := fmt.Sprintf("GRANT CONNECT ON DATABASE %s TO %s; GRANT %s TO %s",
		ident(database), ident(config.ConsoleRole), ident(owner), ident(config.ConsoleRole))
	_, err := c.pool.Exec(ctx, stmt)
	return err
}

// SetPassword replaces the password of a login role.
func (c *Cluster) SetPassword(ctx context.Context, role, scramVerifier string) error {
	_, err := c.pool.Exec(ctx, "ALTER ROLE "+ident(role)+" WITH LOGIN PASSWORD "+literal(scramVerifier))
	return err
}

// CreateUserRole creates an additional login role allowed to connect to
// database. Table privileges are granted separately, inside the database.
func (c *Cluster) CreateUserRole(ctx context.Context, role, database, scramVerifier string) error {
	if err := c.CreateRole(ctx, role, scramVerifier); err != nil {
		return err
	}
	_, err := c.pool.Exec(ctx, fmt.Sprintf("GRANT CONNECT ON DATABASE %s TO %s", ident(database), ident(role)))
	return err
}

// DatabaseStats reads server-wide statistics for one database.
func (c *Cluster) DatabaseStats(ctx context.Context, database string) (model.DatabaseStats, error) {
	var s model.DatabaseStats
	err := c.pool.QueryRow(ctx, `
		SELECT pg_database_size(d.datname),
		       (SELECT count(*) FROM pg_stat_activity a
		         WHERE a.datname = d.datname AND a.backend_type = 'client backend'
		           AND a.application_name NOT IN ($2, $3)),
		       COALESCE(s.sessions, 0), COALESCE(s.xact_commit, 0), COALESCE(s.xact_rollback, 0)
		FROM pg_database d LEFT JOIN pg_stat_database s ON s.datname = d.datname
		WHERE d.datname = $1`, database, config.AppNameInspector, config.AppNameConsole).
		Scan(&s.SizeBytes, &s.OpenConnections, &s.TotalSessions, &s.Commits, &s.Rollbacks)
	return s, translate(err)
}

// DatabaseConnections lists the client sessions open on database right now.
// Internal sessions (the panel's own inspector and console) are hidden, and
// the statement text is only returned while something is actually running.
func (c *Cluster) DatabaseConnections(ctx context.Context, database string) ([]model.Connection, error) {
	rows, err := c.pool.Query(ctx, `
		SELECT pid, COALESCE(usename, ''), COALESCE(application_name, ''), COALESCE(state, ''),
		       backend_start, state_change,
		       CASE WHEN state IN ('active', 'idle in transaction') THEN left(COALESCE(query, ''), $4) ELSE '' END
		FROM pg_stat_activity
		WHERE datname = $1 AND backend_type = 'client backend'
		  AND application_name NOT IN ($2, $3) AND pid <> pg_backend_pid()
		ORDER BY backend_start`, database, config.AppNameInspector, config.AppNameConsole, config.MaxConnectionQueryChars)
	if err != nil {
		return nil, translate(err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.Connection, error) {
		var c model.Connection
		err := r.Scan(&c.PID, &c.User, &c.Application, &c.State, &c.ConnectedAt, &c.StateSince, &c.Query)
		return c, err
	})
}

// ident quotes an identifier. Names are already whitelisted by the naming
// package; quoting is defence in depth.
func ident(name string) string {
	return pgx.Identifier{name}.Sanitize()
}

// literal quotes a string constant for statements that cannot use bind
// parameters (CREATE ROLE ... PASSWORD).
func literal(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
