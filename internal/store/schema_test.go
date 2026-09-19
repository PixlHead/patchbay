package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestOpenUpgradesVersionOneWithoutChangingHistory(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "history.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer legacy.Close()
	if _, err := legacy.ExecContext(ctx, initialSchema); err != nil {
		t.Fatal(err)
	}
	definition, original := runFixture()
	definitionJSON, err := json.Marshal(definition)
	if err != nil {
		t.Fatal(err)
	}
	// Seed the old columns directly: CreateRun requires the error column from v2.
	if _, err := legacy.ExecContext(ctx, `INSERT INTO runs
        (id, workflow_id, workflow_name, definition_json, status, created_at, started_at, finished_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, original.ID, original.WorkflowID,
		original.WorkflowName, string(definitionJSON), original.Status,
		original.CreatedAt.UnixMilli(), unixMilliOrNull(&original.StartedAt), unixMilliOrNull(original.FinishedAt)); err != nil {
		t.Fatal(err)
	}
	for position, step := range original.Steps {
		var outputJSON any
		if step.Output != nil {
			encoded, err := json.Marshal(step.Output)
			if err != nil {
				t.Fatal(err)
			}
			outputJSON = string(encoded)
		}
		if _, err := legacy.ExecContext(ctx, `INSERT INTO run_steps
            (run_id, step_id, position, name, status, started_at, finished_at, output_json, error)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, original.ID, step.ID, position,
			step.Name, step.Status, unixMilliOrNull(step.StartedAt), unixMilliOrNull(step.FinishedAt),
			outputJSON, step.Error); err != nil {
			t.Fatal(err)
		}
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	// The first open upgrades v1; the second must leave the upgraded database alone.
	for range 2 {
		db, err := Open(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		var version, owner int
		if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
			t.Fatal(err)
		}
		if version != schemaVersion {
			t.Fatalf("expected schema version %d, got %d", schemaVersion, version)
		}
		if err := db.QueryRowContext(ctx, "PRAGMA application_id").Scan(&owner); err != nil {
			t.Fatal(err)
		}
		if owner != applicationID {
			t.Fatalf("legacy database was not marked as Patchbay: %d", owner)
		}
		// Includes the new empty error, workflow snapshot, timestamps and step results.
		assertStoredRun(t, db, definition, original)
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMigrateVersionOneConflictPreservesVersionAndData(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// An unexpected preexisting column must not be overwritten or marked migrated.
	if _, err := db.ExecContext(ctx, initialSchema+`
        ALTER TABLE runs ADD COLUMN error TEXT NOT NULL DEFAULT 'keep';
        INSERT INTO runs (id, workflow_id, workflow_name, definition_json, status, created_at)
        VALUES ('old-run', 'checks', 'Checks', '{}', 'failed', 0);
    `); err != nil {
		t.Fatal(err)
	}
	if err := migrate(ctx, db); err == nil {
		t.Fatal("expected the conflicting column to reject migration")
	}
	var version, owner int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, "PRAGMA application_id").Scan(&owner); err != nil {
		t.Fatal(err)
	}
	var message string
	if err := db.QueryRowContext(ctx, "SELECT error FROM runs WHERE id = 'old-run'").Scan(&message); err != nil {
		t.Fatal(err)
	}
	if version != 1 || owner != 0 || message != "keep" {
		t.Fatalf("failed migration changed existing state: version %d, application ID %d, error %q", version, owner, message)
	}
}

func TestMigrateLeavesUnsupportedOrConflictingSchemasUnchanged(t *testing.T) {
	for _, test := range []struct {
		name, setup string
		version     int
	}{
		{"newer version", "PRAGMA user_version = 4", 4},
		{"table conflict", "CREATE TABLE run_steps (marker TEXT)", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "history.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if _, err := db.ExecContext(ctx, test.setup); err != nil {
				t.Fatal(err)
			}
			if err := migrate(ctx, db); err == nil {
				t.Fatal("expected migration to be refused")
			}
			var version, runTables int
			if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
				t.Fatal(err)
			}
			if version != test.version {
				t.Fatalf("schema version changed to %d after failure", version)
			}
			if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_schema WHERE type = 'table' AND name = 'runs'").Scan(&runTables); err != nil {
				t.Fatal(err)
			}
			if runTables != 0 {
				t.Fatal("failed migration left a partially created runs table")
			}
		})
	}
}
