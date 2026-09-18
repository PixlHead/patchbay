//go:build darwin || linux

package main

import (
	"context"
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

func TestRunServerRejectsUnwritableJournalDirectoryBeforeCleanup(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can bypass directory permissions")
	}
	for _, unfinished := range []bool{false, true} {
		name := "empty history"
		if unfinished {
			name = "unfinished history"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			workflowDirectory := writeStartupWorkflow(t)
			definitions, err := workflow.Load(workflowDirectory)
			if err != nil {
				t.Fatal(err)
			}
			directory := t.TempDir()
			dbPath := filepath.Join(directory, "history.db")
			db, err := store.Open(ctx, dbPath)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			// A committed write must create a rollback journal beside the database.
			// Set this while the directory is still writable to isolate that failure.
			var journalMode string
			if err := db.QueryRowContext(ctx, "PRAGMA journal_mode = DELETE").Scan(&journalMode); err != nil {
				t.Fatal(err)
			}
			if journalMode != "delete" {
				t.Fatalf("fixture needs DELETE journal mode, got %q", journalMode)
			}
			expected := []engine.Run{}
			if unfinished {
				definition := definitions[0]
				started := time.UnixMilli(1_750_000_000_000).UTC()
				run := engine.Run{
					ID: "unfinished", WorkflowID: definition.ID, WorkflowName: definition.Name,
					Status: "running", CreatedAt: started, StartedAt: started,
					Steps: []engine.StepRun{{
						ID: definition.Steps[0].ID, Name: definition.Steps[0].Name,
						Status: "running", StartedAt: &started,
					}},
				}
				if err := store.CreateRun(ctx, db, run, definition); err != nil {
					t.Fatal(err)
				}
				expected = append(expected, run)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			// Precreate the reusable lock file so ownership can still be acquired
			// after directory writes are denied. The database itself stays writable.
			if err := os.WriteFile(dbPath+".lock", nil, 0o600); err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{dbPath, dbPath + ".lock"} {
				if err := os.Chmod(path, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			t.Cleanup(func() {
				if err := os.Chmod(directory, 0o700); err != nil {
					t.Errorf("restore temporary directory permissions: %v", err)
				}
			})
			if err := os.Chmod(directory, 0o555); err != nil {
				t.Fatal(err)
			}

			readHistory := func() []engine.Run {
				t.Helper()
				readable, err := store.Open(ctx, dbPath)
				if err != nil {
					t.Fatalf("fixture must allow opening the recognized database: %v", err)
				}
				defer readable.Close()
				history, err := store.ListRuns(ctx, readable, 100)
				if err != nil {
					t.Fatalf("fixture must allow reading saved history: %v", err)
				}
				return history
			}
			if got := readHistory(); !reflect.DeepEqual(got, expected) {
				t.Fatalf("unexpected history before startup: %+v", got)
			}
			// Invalid address keeps this test from opening a listener if startup
			// incorrectly advances beyond the writeability check.
			err = runServer(ctx, "invalid-listen-address", workflowDirectory, "", dbPath, 2, 10, "")
			var addressError *net.AddrError
			if err == nil || !strings.Contains(err.Error(), "check database writeability") || errors.As(err, &addressError) {
				t.Fatalf("expected writeability failure before cleanup and listening, got %v", err)
			}
			lock, err := lockDatabase(dbPath)
			if err != nil {
				t.Fatalf("failed startup left the database locked: %v", err)
			}
			defer lock.Close()
			if got := readHistory(); !reflect.DeepEqual(got, expected) {
				t.Fatalf("failed writeability check changed history: %+v", got)
			}
		})
	}
}
