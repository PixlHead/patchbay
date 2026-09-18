package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestOpenPersistsAcrossReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "history ?#.db")
	db, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var marker int
	if err := db.QueryRowContext(ctx, "PRAGMA application_id").Scan(&marker); err != nil {
		t.Fatal(err)
	}
	if marker != applicationID {
		t.Fatalf("new database was not marked as Patchbay: %d", marker)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO runs
        (id, workflow_id, workflow_name, definition_json, status, created_at)
        VALUES ('run-1', 'workflow-1', 'Example', '{}', 'running', 0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO run_steps
        (run_id, step_id, position, name, status)
        VALUES ('run-1', 'check', 0, 'Check health', 'pending')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var status string
	if err := reopened.QueryRowContext(ctx, "SELECT status FROM run_steps WHERE run_id = ?", "run-1").Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "pending" {
		t.Fatalf("wanted pending step after reopening, got %q", status)
	}
}

func TestOpenRejectsUnavailablePaths(t *testing.T) {
	for _, test := range []struct{ name, path string }{
		{"empty path", ""},
		{"missing parent", filepath.Join(t.TempDir(), "missing", "history.db")},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, err := Open(context.Background(), test.path)
			if db != nil {
				defer db.Close()
			}
			if err == nil {
				t.Fatal("expected an error for an unavailable database path")
			}
			if db != nil {
				t.Fatal("failed open returned a database handle")
			}
		})
	}
}
