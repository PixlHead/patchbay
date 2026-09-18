package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestCheckWritableCommitsWithoutChangingSchemaOrHistory(t *testing.T) {
	for _, populated := range []bool{false, true} {
		name := "empty history"
		if populated {
			name = "saved history"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "history.db")
			db, err := Open(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			definition, original := runFixture()
			original.Error = "Keep this run-level error."
			if populated {
				if err := CreateRun(ctx, db, original, definition); err != nil {
					t.Fatal(err)
				}
			}
			const schemaQuery = `SELECT group_concat(sql, ';') FROM
				(SELECT sql FROM sqlite_schema ORDER BY name)`
			var beforeSchema string
			if err := db.QueryRowContext(ctx, schemaQuery).Scan(&beforeSchema); err != nil {
				t.Fatal(err)
			}

			observer, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			defer observer.Close()
			conn, err := observer.Conn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			// data_version changes on this connection only when another connection
			// commits a write. This catches probes that merely read or roll back.
			var before, after int
			if err := conn.QueryRowContext(ctx, "PRAGMA data_version").Scan(&before); err != nil {
				t.Fatal(err)
			}
			if err := CheckWritable(ctx, db); err != nil {
				t.Fatal(err)
			}
			if err := conn.QueryRowContext(ctx, "PRAGMA data_version").Scan(&after); err != nil {
				t.Fatal(err)
			}
			if after == before {
				t.Fatal("write check did not commit a write visible to another connection")
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			// Reopen to verify the persistent state, including identity validation.
			reopened, err := Open(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			var version, marker int
			if err := reopened.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
				t.Fatal(err)
			}
			if err := reopened.QueryRowContext(ctx, "PRAGMA application_id").Scan(&marker); err != nil {
				t.Fatal(err)
			}
			if version != schemaVersion || marker != applicationID {
				t.Fatalf("write check changed identity: version=%d marker=%x", version, marker)
			}
			var afterSchema string
			if err := reopened.QueryRowContext(ctx, schemaQuery).Scan(&afterSchema); err != nil {
				t.Fatal(err)
			}
			if afterSchema != beforeSchema {
				t.Fatal("write check changed the stored schema")
			}
			var runs, steps int
			if err := reopened.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM runs),
				(SELECT COUNT(*) FROM run_steps)`).Scan(&runs, &steps); err != nil {
				t.Fatal(err)
			}
			if populated {
				if runs != 1 || steps != len(original.Steps) {
					t.Fatalf("write check changed history size: %d runs, %d steps", runs, steps)
				}
				assertStoredRun(t, reopened, definition, original)
			} else if runs != 0 || steps != 0 {
				t.Fatalf("write check inserted history: %d runs, %d steps", runs, steps)
			}
		})
	}
}

func TestCheckWritableRejectsQueryOnlyConnection(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	definition, original := runFixture()
	if err := CreateRun(ctx, db, original, definition); err != nil {
		t.Fatal(err)
	}
	// Open keeps one connection, so this setting also applies to the probe.
	if _, err := db.ExecContext(ctx, "PRAGMA query_only = ON"); err != nil {
		t.Fatal(err)
	}
	assertStoredRun(t, db, definition, original)
	if err := CheckWritable(ctx, db); err == nil {
		t.Fatal("write check accepted a connection that permits only reads")
	}
	assertStoredRun(t, db, definition, original)
}

func TestCheckWritableHonorsContext(t *testing.T) {
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := CheckWritable(ctx, db); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled context, got %v", err)
	}
	// Hold the sole connection so the check must wait using its caller's deadline.
	// This avoids relying on SQLite's busy-handler sleep timing for cancellation.
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	deadlineCtx, cancelDeadline := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelDeadline()
	if err := CheckWritable(deadlineCtx, db); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline while waiting for a connection, got %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if err := CheckWritable(context.Background(), db); err != nil {
		t.Fatalf("write check failed after connection became available: %v", err)
	}
}
