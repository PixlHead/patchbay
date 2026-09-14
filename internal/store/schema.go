package store

import (
	"context"
	"database/sql"
	"fmt"
)

const schemaVersion = 1

// Keep this first migration unchanged when adding later schema versions.
// Timestamps are Unix milliseconds; NULL means a run or step has not started/finished.
const initialSchema = `
CREATE TABLE runs (
    id TEXT PRIMARY KEY NOT NULL,
    workflow_id TEXT NOT NULL,
    workflow_name TEXT NOT NULL,
    definition_json TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    started_at INTEGER,
    finished_at INTEGER
);

CREATE TABLE run_steps (
    run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    step_id TEXT NOT NULL,
    position INTEGER NOT NULL CHECK (position >= 0),
    name TEXT NOT NULL,
    status TEXT NOT NULL,
    started_at INTEGER,
    finished_at INTEGER,
    output_json TEXT,
    error TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (run_id, step_id),
    UNIQUE (run_id, position)
);

PRAGMA user_version = 1;
`

func migrate(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin schema migration: %w", err)
	}
	defer tx.Rollback() // Also releases the transaction on any early return.

	var version int
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("read database schema version: %w", err)
	}
	if version == schemaVersion {
		return nil
	}
	if version != 0 {
		return fmt.Errorf("unsupported database schema version %d (expected %d)", version, schemaVersion)
	}

	// The tables and version number either all commit or all roll back.
	if _, err := tx.ExecContext(ctx, initialSchema); err != nil {
		return fmt.Errorf("create initial database schema: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit schema migration: %w", err)
	}
	return nil
}
