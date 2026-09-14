package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestMigrateLeavesUnsupportedOrConflictingSchemasUnchanged(t *testing.T) {
	for _, test := range []struct {
		name, setup string
		version     int
	}{
		{"newer version", "PRAGMA user_version = 2", 2},
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
