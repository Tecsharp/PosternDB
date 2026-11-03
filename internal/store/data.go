package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/tecsharp/posterndb/internal/config"
	"github.com/tecsharp/posterndb/internal/model"
)

// Data editor.
//
// Security model: every statement runs as posterndb_console after
// SET ROLE <owner>, so it can only touch what the client role owns.
// Identifiers (columns, types) are never taken from the request: the
// request only *names* columns, which are looked up in pg_catalog first;
// unknown names are rejected and the quoted catalog names are used. Values
// are always bind parameters, cast to the column type with
// CAST($n AS <type from pg_catalog>).

// Data editor errors shown to the operator.
var (
	ErrTableNotFound  = errors.New("la tabla no existe")
	ErrReadOnlyTable  = errors.New("esta tabla no se puede editar: es una vista o no tiene clave primaria")
	ErrUnknownColumn  = errors.New("columna desconocida")
	ErrMissingKey     = errors.New("falta la clave primaria de la fila")
	ErrRowNotFound    = errors.New("la fila ya no existe o cambió su clave; recarga los datos")
	ErrNothingToWrite = errors.New("no hay cambios que guardar")
	ErrGeneratedWrite = errors.New("las columnas generadas no se pueden escribir")
)

const tableColumnsSQL = `
SELECT a.attname, format_type(a.atttypid, a.atttypmod), NOT a.attnotnull,
       COALESCE(a.attnum = ANY (i.indkey), false),
       a.atthasdef OR a.attidentity <> '',
       a.attgenerated <> '' OR a.attidentity = 'a',
       c.relkind
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped
LEFT JOIN pg_index i ON i.indrelid = c.oid AND i.indisprimary
WHERE n.nspname = $1 AND c.relname = $2 AND c.relkind IN ('r', 'p', 'v', 'm')
ORDER BY a.attnum`

type tableMeta struct {
	columns  []model.DataColumn
	byName   map[string]model.DataColumn
	pk       []model.DataColumn
	editable bool
}

func loadTableMeta(ctx context.Context, tx pgx.Tx, schema, table string) (tableMeta, error) {
	rows, err := tx.Query(ctx, tableColumnsSQL, schema, table)
	if err != nil {
		return tableMeta{}, err
	}
	defer rows.Close()
	m := tableMeta{byName: map[string]model.DataColumn{}}
	writable := false
	for rows.Next() {
		var c model.DataColumn
		var kind string
		if err := rows.Scan(&c.Name, &c.Type, &c.Nullable, &c.PrimaryKey, &c.HasDefault, &c.Generated, &kind); err != nil {
			return m, err
		}
		writable = kind == "r" || kind == "p"
		m.columns = append(m.columns, c)
		m.byName[c.Name] = c
		if c.PrimaryKey {
			m.pk = append(m.pk, c)
		}
	}
	if err := rows.Err(); err != nil {
		return m, err
	}
	if len(m.columns) == 0 {
		return m, ErrTableNotFound
	}
	m.editable = writable && len(m.pk) > 0
	return m, nil
}

// Browse returns one page of rows ordered by sortColumn (or the primary key).
func (i *Inspector) Browse(ctx context.Context, database, owner, schema, table string, page, size int, sortColumn string, desc bool) (model.TableData, error) {
	size = clamp(size, 1, config.MaxDataPageSize)
	page = max(page, 1)
	var out model.TableData
	err := i.asOwner(ctx, database, owner, true, func(tx pgx.Tx) error {
		meta, err := loadTableMeta(ctx, tx, schema, table)
		if err != nil {
			return err
		}
		order, err := orderBy(meta, sortColumn, desc)
		if err != nil {
			return err
		}
		names := make([]string, len(meta.columns))
		for k, c := range meta.columns {
			names[k] = ident(c.Name)
		}
		sql := fmt.Sprintf("SELECT %s FROM %s %s LIMIT %d OFFSET %d",
			strings.Join(names, ", "), qualified(schema, table), order, size, (page-1)*size)
		rows, err := tx.Query(ctx, sql, pgx.QueryResultFormats{pgx.TextFormatCode})
		if err != nil {
			return err
		}
		out = model.TableData{Columns: meta.columns, Rows: [][]*string{}, Page: page, PageSize: size, Editable: meta.editable}
		for rows.Next() {
			raw := rows.RawValues()
			row := make([]*string, len(raw))
			for k, v := range raw {
				if v != nil {
					s := string(v)
					row[k] = &s
				}
			}
			out.Rows = append(out.Rows, row)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		return tx.QueryRow(ctx, "SELECT count(*) FROM "+qualified(schema, table)).Scan(&out.Total)
	})
	return out, err
}

func orderBy(meta tableMeta, sortColumn string, desc bool) (string, error) {
	dir := "ASC"
	if desc {
		dir = "DESC"
	}
	if sortColumn != "" {
		if _, ok := meta.byName[sortColumn]; !ok {
			return "", ErrUnknownColumn
		}
		return fmt.Sprintf("ORDER BY %s %s NULLS LAST", ident(sortColumn), dir), nil
	}
	if len(meta.pk) == 0 {
		return "", nil
	}
	parts := make([]string, len(meta.pk))
	for k, c := range meta.pk {
		parts[k] = ident(c.Name) + " " + dir
	}
	return "ORDER BY " + strings.Join(parts, ", "), nil
}

// InsertRow inserts one row; omitted columns take their defaults.
func (i *Inspector) InsertRow(ctx context.Context, database, owner, schema, table string, values model.RowValues) error {
	return i.write(ctx, database, owner, schema, table, func(tx pgx.Tx, meta tableMeta) error {
		cols, args, err := assignments(meta, values)
		if err != nil {
			return err
		}
		sql := "INSERT INTO " + qualified(schema, table) + " DEFAULT VALUES"
		if len(cols) > 0 {
			names, params := make([]string, len(cols)), make([]string, len(cols))
			for k, c := range cols {
				names[k] = ident(c.Name)
				params[k] = cast(k+1, c)
			}
			sql = fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", qualified(schema, table), strings.Join(names, ", "), strings.Join(params, ", "))
		}
		_, err = tx.Exec(ctx, sql, args...)
		return err
	})
}

// UpdateRow changes the given columns of the row identified by key.
func (i *Inspector) UpdateRow(ctx context.Context, database, owner, schema, table string, key, values model.RowValues) error {
	return i.write(ctx, database, owner, schema, table, func(tx pgx.Tx, meta tableMeta) error {
		cols, args, err := assignments(meta, values)
		if err != nil {
			return err
		}
		if len(cols) == 0 {
			return ErrNothingToWrite
		}
		sets := make([]string, len(cols))
		for k, c := range cols {
			sets[k] = ident(c.Name) + " = " + cast(k+1, c)
		}
		where, keyArgs, err := keyCondition(meta, key, len(args))
		if err != nil {
			return err
		}
		sql := fmt.Sprintf("UPDATE %s SET %s WHERE %s", qualified(schema, table), strings.Join(sets, ", "), where)
		return exactlyOne(tx.Exec(ctx, sql, append(args, keyArgs...)...))
	})
}

// DeleteRow removes the row identified by key.
func (i *Inspector) DeleteRow(ctx context.Context, database, owner, schema, table string, key model.RowValues) error {
	return i.write(ctx, database, owner, schema, table, func(tx pgx.Tx, meta tableMeta) error {
		where, args, err := keyCondition(meta, key, 0)
		if err != nil {
			return err
		}
		return exactlyOne(tx.Exec(ctx, fmt.Sprintf("DELETE FROM %s WHERE %s", qualified(schema, table), where), args...))
	})
}

func (i *Inspector) write(ctx context.Context, database, owner, schema, table string, fn func(pgx.Tx, tableMeta) error) error {
	return i.asOwner(ctx, database, owner, false, func(tx pgx.Tx) error {
		meta, err := loadTableMeta(ctx, tx, schema, table)
		if err != nil {
			return err
		}
		if !meta.editable {
			return ErrReadOnlyTable
		}
		return fn(tx, meta)
	})
}

// assignments validates column names against the catalog, in table order.
func assignments(meta tableMeta, values model.RowValues) ([]model.DataColumn, []any, error) {
	var cols []model.DataColumn
	var args []any
	for name := range values {
		c, ok := meta.byName[name]
		if !ok {
			return nil, nil, fmt.Errorf("%w: %s", ErrUnknownColumn, name)
		}
		if c.Generated {
			return nil, nil, fmt.Errorf("%w: %s", ErrGeneratedWrite, name)
		}
	}
	for _, c := range meta.columns {
		if v, ok := values[c.Name]; ok {
			cols = append(cols, c)
			args = append(args, textArg(v))
		}
	}
	return cols, args, nil
}

// keyCondition builds "pk1 = CAST($n AS type) AND ..." from the full key.
func keyCondition(meta tableMeta, key model.RowValues, offset int) (string, []any, error) {
	parts := make([]string, len(meta.pk))
	args := make([]any, len(meta.pk))
	for k, c := range meta.pk {
		v, ok := key[c.Name]
		if !ok || v == nil {
			return "", nil, ErrMissingKey
		}
		parts[k] = ident(c.Name) + " = " + cast(offset+k+1, c)
		args[k] = *v
	}
	return strings.Join(parts, " AND "), args, nil
}

func cast(n int, c model.DataColumn) string {
	return fmt.Sprintf("CAST($%d::text AS %s)", n, c.Type)
}

func textArg(v *string) any {
	if v == nil {
		return nil
	}
	return *v
}

// exactlyOne makes UPDATE/DELETE affect a single row; anything else rolls
// the transaction back (returned error).
func exactlyOne(tag interface{ RowsAffected() int64 }, err error) error {
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrRowNotFound
	}
	return nil
}

func clamp(v, lo, hi int) int {
	return min(max(v, lo), hi)
}
