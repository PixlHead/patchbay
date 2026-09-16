package main

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"patchbay/internal/engine"
	"patchbay/internal/store"
	"patchbay/internal/workflow"
)

func TestRunServerInitializesDatabaseBeforeListenError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	dbPath := filepath.Join(t.TempDir(), "data", "patchbay.db")
	// This address fails parsing, so no network listener is opened.
	err := runServer(ctx, "invalid-listen-address", writeStartupWorkflow(t), "", dbPath, 2, 10)
	var addressError *net.AddrError
	if !errors.As(err, &addressError) {
		t.Fatalf("expected a returned listen error after initialization, got %v", err)
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("database was not created: %v", err)
	}
	// Read directly: store.Open here would hide a missing startup migration.
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var runs, steps int
	if err := db.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM runs),
        (SELECT COUNT(*) FROM run_steps)`).Scan(&runs, &steps); err != nil {
		t.Fatalf("run-history schema was not initialized: %v", err)
	}
	if runs != 0 || steps != 0 {
		t.Fatalf("startup unexpectedly created history: %d runs, %d steps", runs, steps)
	}
}

func TestRunServerRejectsUnusableDatabasePaths(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	directory := writeStartupWorkflow(t)
	parentFile := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(parentFile, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ name, path, message string }{
		{"empty path", "", "lock database"},
		{"directory as database", t.TempDir(), "lock database"},
		{"file as parent", filepath.Join(parentFile, "patchbay.db"), "create database directory"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := runServer(ctx, "invalid-listen-address", directory, "", test.path, 2, 10)
			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("expected %q before attempting to listen, got %v", test.message, err)
			}
		})
	}
}

func writeStartupWorkflow(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	data := `{
        "schemaVersion": 1, "id": "startup", "name": "Startup test",
        "steps": [{"id": "check", "name": "Check", "type": "http.check",
            "config": {"url": "http://127.0.0.1:8080/api/health", "expectedStatus": 200, "timeoutMs": 1000}}]
    }`
	if err := os.WriteFile(filepath.Join(directory, "startup.json"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	return directory
}

func TestRunServerReconcilesHistoryBeforeListening(t *testing.T) {
	for _, test := range []struct {
		name          string
		rejectUpdate  bool
		databaseInUse bool
	}{
		{name: "success"},
		{name: "failed cleanup prevents startup", rejectUpdate: true},
		{name: "another server owns the database", databaseInUse: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			directory := writeStartupWorkflow(t)
			definitions, err := workflow.Load(directory)
			if err != nil {
				t.Fatal(err)
			}
			definition := definitions[0]
			dbPath := filepath.Join(t.TempDir(), "history.db")
			db, err := store.Open(ctx, dbPath)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			started := time.UnixMilli(1_750_000_000_000).UTC()
			expected := engine.Run{
				ID: "unfinished", WorkflowID: definition.ID, WorkflowName: definition.Name,
				Status: "running", CreatedAt: started, StartedAt: started,
				Steps: []engine.StepRun{{
					ID: definition.Steps[0].ID, Name: definition.Steps[0].Name,
					Status: "running", StartedAt: &started,
				}},
			}
			if err := store.CreateRun(ctx, db, expected, definition); err != nil {
				t.Fatal(err)
			}
			if test.rejectUpdate {
				_, err := db.ExecContext(ctx, `CREATE TRIGGER reject_interruption
                    BEFORE UPDATE OF status ON runs
                    WHEN NEW.status = 'interrupted'
                    BEGIN SELECT RAISE(ABORT, 'forced interruption failure'); END`)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			if test.databaseInUse {
				lock, err := lockDatabase(dbPath)
				if err != nil {
					t.Fatal(err)
				}
				defer lock.Close()
			}

			// An invalid address proves ordering without opening a network listener.
			err = runServer(ctx, "invalid-listen-address", directory, "", dbPath, 2, 10)
			var addressError *net.AddrError
			switch {
			case test.databaseInUse:
				if !errors.Is(err, errDatabaseInUse) {
					t.Fatalf("expected database ownership to prevent startup, got %v", err)
				}
			case test.rejectUpdate:
				if err == nil || !strings.Contains(err.Error(), "mark unfinished runs interrupted") || errors.As(err, &addressError) {
					t.Fatalf("expected cleanup to fail before HTTP startup, got %v", err)
				}
			default:
				if !errors.As(err, &addressError) {
					t.Fatalf("expected HTTP startup after cleanup, got %v", err)
				}
				expected.Status, expected.Steps[0].Status = "interrupted", "interrupted"
			}
			if !test.databaseInUse {
				// Both reconciliation and listen failures must release server ownership.
				lock, err := lockDatabase(dbPath)
				if err != nil {
					t.Fatalf("startup failure left the database locked: %v", err)
				}
				defer lock.Close()
			}
			db, err = store.Open(ctx, dbPath)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			saved, err := store.ListRuns(ctx, db, 100)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(saved, []engine.Run{expected}) {
				t.Fatalf("unexpected startup history changes or new executions: %+v", saved)
			}
		})
	}
}

func TestRunServerReleasesDatabaseLockAfterOpenFailure(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "invalid.db")
	if err := os.WriteFile(dbPath, []byte("not a SQLite database"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := runServer(context.Background(), "invalid-listen-address", writeStartupWorkflow(t), "", dbPath, 2, 10)
	if err == nil || !strings.Contains(err.Error(), "open database") {
		t.Fatalf("expected SQLite to reject the file, got %v", err)
	}
	lock, err := lockDatabase(dbPath)
	if err != nil {
		t.Fatalf("failed database open left it locked: %v", err)
	}
	defer lock.Close()
}

func TestRunServerRejectsInvalidActiveRunLimitBeforeTouchingStorage(t *testing.T) {
	for _, limit := range []int{0, -1} {
		dbPath := filepath.Join(t.TempDir(), "data", "history.db")
		err := runServer(context.Background(), "invalid-listen-address", "missing-workflows", "", dbPath, limit, 10)
		if err == nil || !strings.Contains(err.Error(), "max-active-runs must be at least 1") {
			t.Fatalf("limit %d was not rejected before loading workflows: %v", limit, err)
		}
		if _, err := os.Stat(filepath.Dir(dbPath)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("invalid configuration touched storage: %v", err)
		}
	}
}

func TestRunServerRejectsNegativeQueueLimitBeforeTouchingStorage(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "data", "history.db")
	err := runServer(context.Background(), "invalid-listen-address", "missing-workflows", "", dbPath, 2, -1)
	if err == nil || !strings.Contains(err.Error(), "max-queued-runs must be at least 0") {
		t.Fatalf("unexpected validation: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(dbPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid queue limit touched storage: %v", err)
	}
}
