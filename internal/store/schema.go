package store

import (
	"context"
	"database/sql"
	"fmt"
)

const schemaVersion = 3

// "PTBY" identifies Patchbay files independently of their migration version.
const applicationID = 0x50544259

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

// Existing runs have no recorded run-level reason; leave that value empty.
const runErrorMigration = `
ALTER TABLE runs ADD COLUMN error TEXT NOT NULL DEFAULT '';
PRAGMA user_version = 2;
`

// The existing JSON columns can now contain TCP check configuration/results.
// Record that capability so older HTTP-only builds refuse this database.
// Existing tables, workflow snapshots, and result JSON remain unchanged.
const tcpCheckMigration = `PRAGMA user_version = 3;`

func migrate(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin schema migration: %w", err)
	}
	defer tx.Rollback() // Also releases the transaction on any early return.

	var owner, version int
	if err := tx.QueryRowContext(ctx, "PRAGMA application_id").Scan(&owner); err != nil {
		return fmt.Errorf("read database application ID: %w", err)
	}
	if owner != 0 && owner != applicationID {
		return fmt.Errorf("refuse database belonging to another application (application ID %d)", owner)
	}
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("read database schema version: %w", err)
	}
	if version < 0 || version > schemaVersion {
		return fmt.Errorf("unsupported database schema version %d (expected %d)", version, schemaVersion)
	}
	if version == 0 {
		var hasObjects bool
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM main.sqlite_schema WHERE name NOT GLOB 'sqlite_*')").Scan(&hasObjects); err != nil {
			return fmt.Errorf("inspect uninitialized database: %w", err)
		}
		if owner != 0 || hasObjects {
			return fmt.Errorf("refuse to initialize a database that is not empty and unmarked")
		}
	} else if err := recognizeSchema(ctx, tx, version, owner == 0); err != nil {
		return fmt.Errorf("database is not a recognized Patchbay schema version %d: %w", version, err)
	}
	if version == schemaVersion && owner == applicationID {
		return nil // A recognized current database needs no writes.
	}

	// Apply all needed changes in this transaction, including on a fresh database.
	if version == 0 {
		if _, err := tx.ExecContext(ctx, initialSchema); err != nil {
			return fmt.Errorf("create initial database schema: %w", err)
		}
	}
	if version < 2 {
		if _, err := tx.ExecContext(ctx, runErrorMigration); err != nil {
			return fmt.Errorf("migrate database schema to version 2: %w", err)
		}
	}
	if version < 3 {
		if _, err := tx.ExecContext(ctx, tcpCheckMigration); err != nil {
			return fmt.Errorf("migrate database schema to version 3: %w", err)
		}
	}
	// Adopt a recognized legacy database only after its migration succeeds.
	// The marker and any schema changes commit or roll back together.
	if owner == 0 {
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA application_id = %d", applicationID)); err != nil {
			return fmt.Errorf("mark database as Patchbay: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit schema migration: %w", err)
	}
	return nil
}
