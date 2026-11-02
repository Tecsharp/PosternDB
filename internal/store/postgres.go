// Package store implements persistence: the platform metadata repository and
// the cluster-level operations (CREATE DATABASE / ROLE, activity snapshots)
// executed against the shared PostgreSQL server.
package store

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tecsharp/posterndb/internal/model"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// migrationLockID is an arbitrary constant for pg_advisory_lock so that two
// app replicas starting at once do not apply migrations concurrently.
const migrationLockID = 7_406_119_022

// uniqueViolation is the SQLSTATE for unique_violation.
const uniqueViolation = "23505"

// Open connects to PostgreSQL, retrying while the server finishes booting.
func Open(ctx context.Context, dsn string, timeout time.Duration) (*pgxpool.Pool, error) {
	deadline := time.Now().Add(timeout)
	for {
		pool, err := connect(ctx, dsn)
		if err == nil {
			return pool, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("connect to postgres: %w", err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func connect(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

// Migrate applies embedded SQL migrations in lexical order, once each.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrationLockID); err != nil {
		return err
	}
	defer conn.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", migrationLockID) //nolint:errcheck
	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	names, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	for _, name := range names {
		if err := applyMigration(ctx, conn.Conn(), name); err != nil {
			return fmt.Errorf("migration %s: %w", name, err)
		}
	}
	return nil
}

func applyMigration(ctx context.Context, conn *pgx.Conn, name string) error {
	var applied bool
	if err := conn.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)", name).Scan(&applied); err != nil {
		return err
	}
	if applied {
		return nil
	}
	body, err := migrationFiles.ReadFile(name)
	if err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, string(body)); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", name)
		return err
	})
}

// translate maps driver errors to domain errors.
func translate(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return model.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return model.ErrConflict
	}
	return err
}
