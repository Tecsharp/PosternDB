package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/tecsharp/posterndb/internal/config"
	"github.com/tecsharp/posterndb/internal/model"
)

// Inspector opens short-lived connections *into* client databases to read
// their structure, run console queries and manage grants.
//
// Two identities are used:
//
//   - the platform superuser, only for statements we write ourselves
//     (catalog queries, DROP OWNED);
//   - posterndb_console, a NOSUPERUSER NOINHERIT role that is a member of
//     every client owner role. Anything containing operator-supplied SQL
//     (console, templates) runs as this role after SET ROLE <owner>, so it
//     can never reach superuser-only features such as COPY ... TO PROGRAM,
//     pg_read_file or other databases. Console queries additionally run in
//     a READ ONLY transaction with a statement timeout.
//
// Every connection is announced through OnConnect first so the inactivity
// auditor can subtract PosternDB's own sessions from client activity.
type Inspector struct {
	base            *pgx.ConnConfig
	consolePassword string
	OnConnect       func(ctx context.Context, database string)
}

// ErrInvalidQuery is returned for empty or oversized console input.
var ErrInvalidQuery = errors.New("consulta vacía o demasiado larga")

// NewInspector derives client connections from the platform DSN.
func NewInspector(platformDSN, consolePassword string) (*Inspector, error) {
	base, err := pgx.ParseConfig(platformDSN)
	if err != nil {
		return nil, err
	}
	return &Inspector{base: base, consolePassword: consolePassword}, nil
}

func (i *Inspector) connect(ctx context.Context, database string, console bool) (*pgx.Conn, error) {
	if i.OnConnect != nil {
		i.OnConnect(ctx, database)
	}
	cfg := i.base.Copy()
	cfg.Database = database
	cfg.RuntimeParams = map[string]string{"application_name": config.AppNameInspector}
	if console {
		cfg.User = config.ConsoleRole
		cfg.Password = i.consolePassword
		cfg.RuntimeParams["application_name"] = config.AppNameConsole
	}
	return pgx.ConnectConfig(ctx, cfg)
}

func (i *Inspector) with(ctx context.Context, database string, console bool, fn func(*pgx.Conn) error) error {
	conn, err := i.connect(ctx, database, console)
	if err != nil {
		return err
	}
	defer conn.Close(context.WithoutCancel(ctx))
	return fn(conn)
}

// asOwner runs fn in a transaction as the console role switched to owner.
func (i *Inspector) asOwner(ctx context.Context, database, owner string, readOnly bool, fn func(pgx.Tx) error) error {
	return i.with(ctx, database, true, func(conn *pgx.Conn) error {
		mode := pgx.ReadWrite
		if readOnly {
			mode = pgx.ReadOnly
		}
		return pgx.BeginTxFunc(ctx, conn, pgx.TxOptions{AccessMode: mode}, func(tx pgx.Tx) error {
			setup := fmt.Sprintf("SET LOCAL ROLE %s; SET LOCAL statement_timeout = %d",
				ident(owner), config.ConsoleTimeout.Milliseconds())
			if _, err := tx.Exec(ctx, setup); err != nil {
				return err
			}
			return fn(tx)
		})
	})
}

// Query runs one read-only statement and returns at most ConsoleRowLimit
// rows. The extended protocol is forced, so PostgreSQL rejects input that
// contains more than one statement. Values come back in PostgreSQL's text
// representation, which is exactly what a console should display.
func (i *Inspector) Query(ctx context.Context, database, owner, sql string) (model.QueryResult, error) {
	sql = strings.TrimSpace(sql)
	if sql == "" || len(sql) > config.MaxQueryLength {
		return model.QueryResult{}, ErrInvalidQuery
	}
	var res model.QueryResult
	err := i.asOwner(ctx, database, owner, true, func(tx pgx.Tx) error {
		start := time.Now()
		rows, err := tx.Query(ctx, sql, pgx.QueryExecModeExec, pgx.QueryResultFormats{pgx.TextFormatCode})
		if err != nil {
			return err
		}
		defer rows.Close()
		res = collectText(rows, config.ConsoleRowLimit)
		if err := rows.Err(); err != nil {
			return err
		}
		res.DurationMs = time.Since(start).Milliseconds()
		return nil
	})
	return res, err
}

func collectText(rows pgx.Rows, limit int) model.QueryResult {
	res := model.QueryResult{Columns: []string{}, Rows: [][]any{}}
	for _, fd := range rows.FieldDescriptions() {
		res.Columns = append(res.Columns, fd.Name)
	}
	for rows.Next() {
		if len(res.Rows) >= limit {
			res.Truncated = true
			break
		}
		raw := rows.RawValues()
		row := make([]any, len(raw))
		for c, v := range raw {
			if v != nil {
				row[c] = string(v)
			}
		}
		res.Rows = append(res.Rows, row)
	}
	res.RowCount = len(res.Rows)
	return res
}

// Preview returns the first rows of a table.
func (i *Inspector) Preview(ctx context.Context, database, owner, schema, table string) (model.QueryResult, error) {
	sql := fmt.Sprintf("SELECT * FROM %s LIMIT %d", qualified(schema, table), config.PreviewRowLimit)
	return i.Query(ctx, database, owner, sql)
}

// CountRows returns the exact number of rows (subject to the timeout).
func (i *Inspector) CountRows(ctx context.Context, database, owner, schema, table string) (int64, error) {
	var n int64
	err := i.asOwner(ctx, database, owner, true, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(*) FROM "+qualified(schema, table)).Scan(&n)
	})
	return n, err
}

// ApplySQL runs a template (possibly many statements) as the owner, so all
// created objects belong to the client role.
func (i *Inspector) ApplySQL(ctx context.Context, database, owner, sql string) error {
	return i.asOwner(ctx, database, owner, false, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, sql)
		return err
	})
}

// GrantUser gives an additional role read or write access to every table
// and sequence of the database, now and in the future (default privileges).
func (i *Inspector) GrantUser(ctx context.Context, database, owner, user, access string) error {
	tablePriv, seqPriv := "SELECT", "SELECT"
	if access == config.AccessWrite {
		tablePriv, seqPriv = "SELECT, INSERT, UPDATE, DELETE", "USAGE, SELECT, UPDATE"
	}
	u := ident(user)
	return i.asOwner(ctx, database, owner, false, func(tx pgx.Tx) error {
		schemas, err := ownedSchemas(ctx, tx, owner)
		if err != nil {
			return err
		}
		var stmts []string
		for _, s := range schemas {
			q := ident(s)
			stmts = append(stmts,
				fmt.Sprintf("GRANT USAGE ON SCHEMA %s TO %s", q, u),
				fmt.Sprintf("GRANT %s ON ALL TABLES IN SCHEMA %s TO %s", tablePriv, q, u),
				fmt.Sprintf("GRANT %s ON ALL SEQUENCES IN SCHEMA %s TO %s", seqPriv, q, u))
		}
		stmts = append(stmts,
			fmt.Sprintf("ALTER DEFAULT PRIVILEGES FOR ROLE %s GRANT %s ON TABLES TO %s", ident(owner), tablePriv, u),
			fmt.Sprintf("ALTER DEFAULT PRIVILEGES FOR ROLE %s GRANT %s ON SEQUENCES TO %s", ident(owner), seqPriv, u),
			fmt.Sprintf("ALTER DEFAULT PRIVILEGES FOR ROLE %s GRANT USAGE ON SCHEMAS TO %s", ident(owner), u))
		_, err = tx.Exec(ctx, strings.Join(stmts, ";\n"))
		return err
	})
}

func ownedSchemas(ctx context.Context, tx pgx.Tx, owner string) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT nspname FROM pg_namespace n JOIN pg_roles r ON r.oid = n.nspowner
		WHERE r.rolname = $1 OR n.nspname = 'public' ORDER BY 1`, owner)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// DropUserObjects removes everything an additional role owns or was granted
// inside database, so the role can then be dropped cluster-wide.
func (i *Inspector) DropUserObjects(ctx context.Context, database, owner, user string) error {
	return i.with(ctx, database, false, func(conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, fmt.Sprintf("REASSIGN OWNED BY %s TO %s; DROP OWNED BY %s",
			ident(user), ident(owner), ident(user)))
		return err
	})
}

func qualified(schema, table string) string {
	return pgx.Identifier{schema, table}.Sanitize()
}
