package main

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunServerInitializesDatabaseBeforeListenError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	dbPath := filepath.Join(t.TempDir(), "data", "patchbay.db")
	// This address fails parsing, so no network listener is opened.
	err := runServer(ctx, "invalid-listen-address", writeStartupWorkflow(t), "", dbPath)
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
		{"empty path", "", "open database"},
		{"directory as database", t.TempDir(), "open database"},
		{"file as parent", filepath.Join(parentFile, "patchbay.db"), "create database directory"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := runServer(ctx, "invalid-listen-address", directory, "", test.path)
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
