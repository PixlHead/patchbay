package store

import (
	"context"
	"database/sql"
	"fmt"
)

// Legacy files have no application ID. Recognize the known table layouts
// before adopting them. Also check marked files before allowing startup writes.
// This guards against selecting the wrong file; it is not a tamper/integrity check.
func recognizeSchema(ctx context.Context, tx *sql.Tx, version int, legacy bool) error {
	rows, err := tx.QueryContext(ctx, `SELECT type, name, tbl_name FROM main.sqlite_schema
        WHERE name NOT GLOB 'sqlite_*'`)
	if err != nil {
		return fmt.Errorf("read schema objects: %w", err)
	}
	defer rows.Close()
	tables := make(map[string]bool)
	for rows.Next() {
		var kind, name, table string
		if err := rows.Scan(&kind, &name, &table); err != nil {
			return fmt.Errorf("read schema object: %w", err)
		}
		knownTable := table == "runs" || table == "run_steps"
		if kind == "table" && knownTable && name == table {
			tables[name] = true
		} else if !legacy && knownTable && (kind == "index" || kind == "trigger") {
			// Marked files may have diagnostic indexes or test-injected triggers.
			continue
		} else {
			return fmt.Errorf("unexpected %s %q", kind, name)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read schema objects: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close schema objects: %w", err)
	}
	if !tables["runs"] || !tables["run_steps"] {
		return fmt.Errorf("expected runs and run_steps tables")
	}

	// Match names, declared types, nullability, defaults, primary-key order,
	// and hidden/generated columns. SQL text need not have identical formatting.
	runColumns := []schemaColumn{
		{"id", "TEXT", 1, "", 1, 0},
		{"workflow_id", "TEXT", 1, "", 0, 0},
		{"workflow_name", "TEXT", 1, "", 0, 0},
		{"definition_json", "TEXT", 1, "", 0, 0},
		{"status", "TEXT", 1, "", 0, 0},
		{"created_at", "INTEGER", 1, "", 0, 0},
		{"started_at", "INTEGER", 0, "", 0, 0},
		{"finished_at", "INTEGER", 0, "", 0, 0},
	}
	if version >= 2 {
		runColumns = append(runColumns, schemaColumn{"error", "TEXT", 1, "''", 0, 0})
	}
	if err := recognizeColumns(ctx, tx, "runs", runColumns); err != nil {
		return err
	}
	if err := recognizeColumns(ctx, tx, "run_steps", []schemaColumn{
		{"run_id", "TEXT", 1, "", 1, 0},
		{"step_id", "TEXT", 1, "", 2, 0},
		{"position", "INTEGER", 1, "", 0, 0},
		{"name", "TEXT", 1, "", 0, 0},
		{"status", "TEXT", 1, "", 0, 0},
		{"started_at", "INTEGER", 0, "", 0, 0},
		{"finished_at", "INTEGER", 0, "", 0, 0},
		{"output_json", "TEXT", 0, "", 0, 0},
		{"error", "TEXT", 1, "''", 0, 0},
	}); err != nil {
		return err
	}

	var uniquePosition bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (
        SELECT 1 FROM pragma_index_list('run_steps', 'main') AS idx
        WHERE idx."unique" = 1 AND idx.partial = 0
          AND (SELECT COUNT(*) FROM pragma_index_info(idx.name, 'main')) = 2
          AND (SELECT name FROM pragma_index_info(idx.name, 'main') WHERE seqno = 0) = 'run_id'
          AND (SELECT name FROM pragma_index_info(idx.name, 'main') WHERE seqno = 1) = 'position'
    )`).Scan(&uniquePosition); err != nil {
		return fmt.Errorf("read step-position uniqueness: %w", err)
	}
	if !uniquePosition {
		return fmt.Errorf("expected unique run_steps (run_id, position)")
	}
	return recognizeForeignKeys(ctx, tx)
}

type schemaColumn struct {
	name, dataType string
	notNull        int
	defaultSQL     string
	primaryKey     int
	hidden         int
}

func recognizeColumns(ctx context.Context, tx *sql.Tx, table string, expected []schemaColumn) error {
	var kind string
	var withoutRowID, strict int
	if err := tx.QueryRowContext(ctx, `SELECT type, wr, strict FROM pragma_table_list
        WHERE schema = 'main' AND name = ?`, table).Scan(&kind, &withoutRowID, &strict); err != nil {
		return fmt.Errorf("read table %q: %w", table, err)
	}
	if kind != "table" || withoutRowID != 0 || strict != 0 {
		return fmt.Errorf("unexpected table kind for %q", table)
	}
	rows, err := tx.QueryContext(ctx, `SELECT name, type, "notnull", COALESCE(dflt_value, ''), pk, hidden
        FROM pragma_table_xinfo(?, 'main') ORDER BY cid`, table)
	if err != nil {
		return fmt.Errorf("read columns of %q: %w", table, err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var column schemaColumn
		if err := rows.Scan(&column.name, &column.dataType, &column.notNull, &column.defaultSQL, &column.primaryKey, &column.hidden); err != nil {
			return fmt.Errorf("read column of %q: %w", table, err)
		}
		if count >= len(expected) || column != expected[count] {
			return fmt.Errorf("unexpected definition for %s column %q", table, column.name)
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read columns of %q: %w", table, err)
	}
	if count != len(expected) {
		return fmt.Errorf("unexpected column count for %q", table)
	}
	return nil
}

func recognizeForeignKeys(ctx context.Context, tx *sql.Tx) error {
	var runKeys int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM pragma_foreign_key_list('runs', 'main')").Scan(&runKeys); err != nil {
		return fmt.Errorf("read run foreign keys: %w", err)
	}
	if runKeys != 0 {
		return fmt.Errorf("unexpected foreign keys on runs")
	}
	rows, err := tx.QueryContext(ctx, `SELECT "table", "from", "to", on_update, on_delete, "match"
        FROM pragma_foreign_key_list('run_steps', 'main')`)
	if err != nil {
		return fmt.Errorf("read step foreign keys: %w", err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var table, from, to, onUpdate, onDelete, match string
		if err := rows.Scan(&table, &from, &to, &onUpdate, &onDelete, &match); err != nil {
			return fmt.Errorf("read step foreign key: %w", err)
		}
		if table != "runs" || from != "run_id" || to != "id" || onUpdate != "NO ACTION" || onDelete != "CASCADE" || match != "NONE" {
			return fmt.Errorf("unexpected foreign key on run_steps")
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read step foreign keys: %w", err)
	}
	if count != 1 {
		return fmt.Errorf("expected one cascading foreign key from run_steps to runs")
	}
	return nil
}
