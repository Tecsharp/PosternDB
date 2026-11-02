package store

import (
	"context"
	"sort"

	"github.com/jackc/pgx/v5"

	"github.com/tecsharp/posterndb/internal/model"
)

// userRelations lists tables, partitioned tables, views and materialized
// views outside the system schemas. Row counts come from statistics
// (n_live_tup, falling back to the planner estimate) so listing a large
// database never scans tables; the panel offers an exact count on demand.
const userRelations = `
SELECT c.oid, n.nspname, c.relname,
       CASE c.relkind WHEN 'r' THEN 'table' WHEN 'p' THEN 'table'
                      WHEN 'v' THEN 'view' WHEN 'm' THEN 'materialized_view' END,
       GREATEST(COALESCE(s.n_live_tup, 0), c.reltuples::bigint, 0),
       CASE WHEN c.relkind IN ('r', 'p', 'm') THEN pg_total_relation_size(c.oid) ELSE 0 END,
       COALESCE(obj_description(c.oid, 'pg_class'), '')
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
LEFT JOIN pg_stat_user_tables s ON s.relid = c.oid
WHERE c.relkind IN ('r', 'p', 'v', 'm')
  AND NOT c.relispartition
  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
  AND n.nspname NOT LIKE 'pg_toast%'
  AND n.nspname NOT LIKE 'pg_temp%'
ORDER BY n.nspname, c.relname`

const columnsSQL = `
SELECT a.attrelid, a.attname, format_type(a.atttypid, a.atttypmod), NOT a.attnotnull,
       COALESCE(pg_get_expr(d.adbin, d.adrelid), ''),
       EXISTS (SELECT 1 FROM pg_index i WHERE i.indrelid = a.attrelid AND i.indisprimary AND a.attnum = ANY (i.indkey))
FROM pg_attribute a
LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
WHERE a.attrelid = ANY ($1) AND a.attnum > 0 AND NOT a.attisdropped
ORDER BY a.attrelid, a.attnum`

const foreignKeysSQL = `
SELECT con.conrelid, con.conname, rn.nspname, rc.relname,
       ARRAY(SELECT a.attname FROM unnest(con.conkey) WITH ORDINALITY k(n, o)
             JOIN pg_attribute a ON a.attrelid = con.conrelid AND a.attnum = k.n ORDER BY k.o),
       ARRAY(SELECT a.attname FROM unnest(con.confkey) WITH ORDINALITY k(n, o)
             JOIN pg_attribute a ON a.attrelid = con.confrelid AND a.attnum = k.n ORDER BY k.o)
FROM pg_constraint con
JOIN pg_class rc ON rc.oid = con.confrelid
JOIN pg_namespace rn ON rn.oid = rc.relnamespace
WHERE con.contype = 'f' AND con.conrelid = ANY ($1)
ORDER BY con.conrelid, con.conname`

const indexesSQL = `
SELECT i.indrelid, ic.relname, pg_get_indexdef(i.indexrelid), i.indisunique
FROM pg_index i JOIN pg_class ic ON ic.oid = i.indexrelid
WHERE i.indrelid = ANY ($1)
ORDER BY i.indrelid, ic.relname`

// Schema reads the structure of a client database.
func (i *Inspector) Schema(ctx context.Context, database string) (model.Schema, error) {
	var out model.Schema
	err := i.with(ctx, database, false, func(conn *pgx.Conn) error {
		tables, oids, err := readRelations(ctx, conn)
		if err != nil {
			return err
		}
		if err := readColumns(ctx, conn, oids, tables); err != nil {
			return err
		}
		if err := readForeignKeys(ctx, conn, oids, tables); err != nil {
			return err
		}
		if err := readIndexes(ctx, conn, oids, tables); err != nil {
			return err
		}
		out = assemble(oids, tables)
		return nil
	})
	return out, err
}

func readRelations(ctx context.Context, conn *pgx.Conn) (map[uint32]*model.Table, []uint32, error) {
	rows, err := conn.Query(ctx, userRelations)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	tables := map[uint32]*model.Table{}
	var oids []uint32
	for rows.Next() {
		var oid uint32
		t := &model.Table{Columns: []model.Column{}, ForeignKeys: []model.ForeignKey{}, Indexes: []model.Index{}}
		if err := rows.Scan(&oid, &t.Schema, &t.Name, &t.Kind, &t.Rows, &t.SizeBytes, &t.Comment); err != nil {
			return nil, nil, err
		}
		tables[oid] = t
		oids = append(oids, oid)
	}
	return tables, oids, rows.Err()
}

func readColumns(ctx context.Context, conn *pgx.Conn, oids []uint32, tables map[uint32]*model.Table) error {
	rows, err := conn.Query(ctx, columnsSQL, oids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var oid uint32
		var c model.Column
		if err := rows.Scan(&oid, &c.Name, &c.Type, &c.Nullable, &c.Default, &c.PrimaryKey); err != nil {
			return err
		}
		tables[oid].Columns = append(tables[oid].Columns, c)
	}
	return rows.Err()
}

func readForeignKeys(ctx context.Context, conn *pgx.Conn, oids []uint32, tables map[uint32]*model.Table) error {
	rows, err := conn.Query(ctx, foreignKeysSQL, oids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var oid uint32
		var fk model.ForeignKey
		if err := rows.Scan(&oid, &fk.Name, &fk.RefSchema, &fk.RefTable, &fk.Columns, &fk.RefColumns); err != nil {
			return err
		}
		tables[oid].ForeignKeys = append(tables[oid].ForeignKeys, fk)
	}
	return rows.Err()
}

func readIndexes(ctx context.Context, conn *pgx.Conn, oids []uint32, tables map[uint32]*model.Table) error {
	rows, err := conn.Query(ctx, indexesSQL, oids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var oid uint32
		var ix model.Index
		if err := rows.Scan(&oid, &ix.Name, &ix.Definition, &ix.Unique); err != nil {
			return err
		}
		tables[oid].Indexes = append(tables[oid].Indexes, ix)
	}
	return rows.Err()
}

func assemble(oids []uint32, tables map[uint32]*model.Table) model.Schema {
	out := model.Schema{Schemas: []string{}, Tables: make([]model.Table, 0, len(oids))}
	seen := map[string]bool{}
	for _, oid := range oids {
		t := tables[oid]
		out.Tables = append(out.Tables, *t)
		if !seen[t.Schema] {
			seen[t.Schema] = true
			out.Schemas = append(out.Schemas, t.Schema)
		}
	}
	sort.Strings(out.Schemas)
	return out
}

// TableSummary returns the table count, total estimated rows and the ten
// largest tables, for the database overview.
func (i *Inspector) TableSummary(ctx context.Context, database string) (int, int64, []model.TableBrief, error) {
	var count int
	var total int64
	top := []model.TableBrief{}
	err := i.with(ctx, database, false, func(conn *pgx.Conn) error {
		rows, err := conn.Query(ctx, userRelations)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var oid uint32
			var t model.Table
			if err := rows.Scan(&oid, &t.Schema, &t.Name, &t.Kind, &t.Rows, &t.SizeBytes, &t.Comment); err != nil {
				return err
			}
			if t.Kind == "view" {
				continue
			}
			count++
			total += t.Rows
			top = append(top, model.TableBrief{Schema: t.Schema, Name: t.Name, Rows: t.Rows, SizeBytes: t.SizeBytes})
		}
		return rows.Err()
	})
	sort.Slice(top, func(a, b int) bool { return top[a].SizeBytes > top[b].SizeBytes })
	if len(top) > 10 {
		top = top[:10]
	}
	return count, total, top, err
}
