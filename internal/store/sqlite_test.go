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
	if _, err := db.ExecContext(ctx, "CREATE TABLE notes (body TEXT NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO notes (body) VALUES (?)", "saved"); err != nil {
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
	var body string
	if err := reopened.QueryRowContext(ctx, "SELECT body FROM notes").Scan(&body); err != nil {
		t.Fatal(err)
	}
	if body != "saved" {
		t.Fatalf("wanted saved data after reopening, got %q", body)
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
