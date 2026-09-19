package store

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenRefusesUnrecognizedDatabasesWithoutChangingTheirFiles(t *testing.T) {
	const patchbayMarker = "PRAGMA application_id = 0x50544259;"
	const foreignMarker = "PRAGMA application_id = 0x12345678;"
	const extraTable = "CREATE TABLE notes (body TEXT); INSERT INTO notes VALUES ('keep this history');"
	const extraView = "CREATE VIEW saved_names AS SELECT workflow_name FROM runs;"
	const extraIndex = "CREATE INDEX run_status ON runs(status);"
	const extraTrigger = "CREATE TRIGGER keep_runs AFTER INSERT ON runs BEGIN SELECT 1; END;"
	versionTwoSchema := initialSchema + runErrorMigration
	currentSchema := versionTwoSchema + tcpCheckMigration
	for _, test := range []struct{ name, setup string }{
		{"unrelated populated version zero", extraTable},
		{"sqliteX table is not internal", "CREATE TABLE sqliteXnotes (body TEXT); INSERT INTO sqliteXnotes VALUES ('keep');"},
		{"view-only version zero", "CREATE VIEW sqliteXview AS SELECT 'keep' AS marker;"},
		{"foreign marker on empty database", foreignMarker},
		{"foreign marker on matching schema", currentSchema + foreignMarker},
		{"version one wrong columns", "CREATE TABLE runs (id TEXT); CREATE TABLE run_steps (run_id TEXT); PRAGMA user_version = 1;"},
		{"version two wrong columns", "CREATE TABLE runs (id TEXT); CREATE TABLE run_steps (run_id TEXT); PRAGMA user_version = 2;"},
		{"version one missing primary key", strings.Replace(initialSchema, "id TEXT PRIMARY KEY NOT NULL", "id TEXT NOT NULL", 1)},
		{"version one missing position uniqueness", strings.Replace(initialSchema, ",\n    UNIQUE (run_id, position)", "", 1)},
		{"version two missing foreign key", strings.Replace(versionTwoSchema, " REFERENCES runs(id) ON DELETE CASCADE", "", 1)},
		{"version two non-cascading foreign key", strings.Replace(versionTwoSchema, "ON DELETE CASCADE", "ON DELETE RESTRICT", 1)},
		{"version two wrong column type", strings.Replace(versionTwoSchema, "workflow_name TEXT", "workflow_name BLOB", 1)},
		{"version two nullable column", strings.Replace(versionTwoSchema, "workflow_name TEXT NOT NULL", "workflow_name TEXT", 1)},
		{"version two wrong error default", initialSchema + strings.Replace(runErrorMigration, "DEFAULT ''", "DEFAULT 'keep'", 1)},
		{"version two generated error column", initialSchema + strings.Replace(runErrorMigration, "error TEXT NOT NULL DEFAULT ''", "error TEXT GENERATED ALWAYS AS ('') VIRTUAL", 1)},
		{"unmarked legacy extra table", versionTwoSchema + extraTable},
		{"unmarked legacy extra view", versionTwoSchema + extraView},
		{"unmarked legacy extra index", versionTwoSchema + extraIndex},
		{"unmarked legacy extra trigger", versionTwoSchema + extraTrigger},
		{"marked current wrong columns", "CREATE TABLE runs (id TEXT); PRAGMA user_version = 3;" + patchbayMarker},
		{"marked current missing constraint", strings.Replace(currentSchema, ",\n    UNIQUE (run_id, position)", "", 1) + patchbayMarker},
		{"marked current extra table", currentSchema + patchbayMarker + extraTable},
		{"marked current extra view", currentSchema + patchbayMarker + extraView},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "existing.db")
			fixture, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			defer fixture.Close()
			if _, err := fixture.ExecContext(context.Background(), test.setup); err != nil {
				t.Fatalf("create existing database: %v", err)
			}
			if err := fixture.Close(); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			db, openErr := Open(context.Background(), path)
			if db != nil {
				if err := db.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if openErr == nil || db != nil {
				t.Errorf("unrecognized database was not refused: db=%v error=%v", db, openErr)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			// With all connections closed, this covers schema, row contents, and
			// both header values: user_version and application_id.
			if !bytes.Equal(before, after) {
				t.Fatal("refused database was modified")
			}
		})
	}
}

func TestOpenAdoptsUnmarkedVersionTwoAndPreservesHistory(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer legacy.Close()
	if _, err := legacy.ExecContext(ctx, initialSchema+runErrorMigration); err != nil {
		t.Fatal(err)
	}
	definition, original := runFixture()
	original.Error = `Could not save the result of step "Broken service". Execution stopped. Check server logs.`
	if err := CreateRun(ctx, legacy, original, definition); err != nil {
		t.Fatal(err)
	}
	// SQLite's own statistics and automatic indexes do not make a legacy
	// Patchbay database unrelated. ANALYZE creates the internal stats table.
	if _, err := legacy.ExecContext(ctx, "ANALYZE"); err != nil {
		t.Fatal(err)
	}
	var marker int
	if err := legacy.QueryRowContext(ctx, "PRAGMA application_id").Scan(&marker); err != nil {
		t.Fatal(err)
	}
	if marker != 0 {
		t.Fatalf("legacy fixture was already marked: %d", marker)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	for attempt := range 2 {
		db, err := Open(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		if err := db.QueryRowContext(ctx, "PRAGMA application_id").Scan(&marker); err != nil {
			t.Fatal(err)
		}
		var version int
		if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
			t.Fatal(err)
		}
		if marker != applicationID || version != schemaVersion {
			t.Fatalf("wrong identity after open %d: marker=%x version=%d", attempt+1, marker, version)
		}
		assertStoredRun(t, db, definition, original)
		if attempt == 0 {
			// A marked database may retain indexes and triggers attached to its
			// known tables, including the existing tests' failure-injection triggers.
			if _, err := db.ExecContext(ctx, `CREATE INDEX run_status ON runs(status);
                CREATE TRIGGER keep_runs AFTER INSERT ON runs BEGIN SELECT 1; END;`); err != nil {
				t.Fatal(err)
			}
		} else {
			var attachedObjects int
			if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_schema WHERE name IN ('run_status', 'keep_runs')").Scan(&attachedObjects); err != nil {
				t.Fatal(err)
			}
			if attachedObjects != 2 {
				t.Fatal("reopening removed a known table's index or trigger")
			}
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
