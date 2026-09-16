package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"patchbay/internal/engine"
	"patchbay/internal/httpapi"
	"patchbay/internal/nodes"
	"patchbay/internal/store"
	"patchbay/internal/workflow"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "HTTP listen address")
	directory := flag.String("workflows", "workflows", "directory containing workflow JSON files")
	webDir := flag.String("web", "web/dist", "built frontend directory")
	dbPath := flag.String("db", "data/patchbay.db", "SQLite database file")
	maxActiveRuns := flag.Int("max-active-runs", 2, "maximum concurrent workflow runs (at least 1)")
	maxQueuedRuns := flag.Int("max-queued-runs", 10, "maximum waiting workflow runs (0 disables queuing)")
	allowedHosts := flag.String("allowed-hosts", "", "comma-separated additional allowed hostnames or IPs (no ports)")
	flag.Parse()

	stop, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := runServer(stop, *addr, *directory, *webDir, *dbPath, *maxActiveRuns, *maxQueuedRuns, *allowedHosts); err != nil {
		slog.Error("patchbay failed", "error", err)
		os.Exit(1)
	}
}

// Returning errors lets deferred cleanup finish before main exits the process.
func runServer(ctx context.Context, addr, directory, webDir, dbPath string, maxActiveRuns, maxQueuedRuns int, allowedHosts string) error {
	if maxActiveRuns < 1 {
		return fmt.Errorf("max-active-runs must be at least 1")
	}
	if maxQueuedRuns < 0 {
		return fmt.Errorf("max-queued-runs must be at least 0")
	}
	hostGuard, err := httpapi.NewHostGuard(allowedHosts)
	if err != nil {
		return fmt.Errorf("invalid allowed-hosts configuration: %w", err)
	}
	definitions, err := workflow.Load(directory)
	if err != nil {
		return fmt.Errorf("invalid workflow configuration: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		return fmt.Errorf("create database directory: %w", err)
	}
	// Take ownership before migrations or startup cleanup can change saved runs.
	databaseLock, err := lockDatabase(dbPath)
	if err != nil {
		return fmt.Errorf("lock database %q: %w", dbPath, err)
	}
	defer func() {
		if err := databaseLock.Close(); err != nil {
			slog.Error("database lock close failed", "error", err)
		}
	}()
	db, err := store.Open(ctx, dbPath)
	if err != nil {
		return fmt.Errorf("open database %q: %w", dbPath, err)
	}
	// Defers run in reverse order: stop the runner, close SQLite, release the lock.
	defer func() {
		if err := db.Close(); err != nil {
			slog.Error("database close failed", "error", err)
		}
	}()

	// Reconcile saved history before creating a runner or accepting requests.
	interruptionCtx, cancelInterruption := context.WithTimeout(ctx, 5*time.Second)
	interrupted, err := store.MarkUnfinishedRunsInterrupted(interruptionCtx, db)
	cancelInterruption()
	if err != nil {
		return fmt.Errorf("mark unfinished runs interrupted: %w", err)
	}
	if interrupted > 0 {
		slog.Info("marked unfinished runs interrupted", "runs", interrupted)
	}

	httpNode := nodes.NewHTTP()
	runner, err := engine.New(maxActiveRuns, maxQueuedRuns, httpNode.Execute, func(ctx context.Context, run engine.Run, definition workflow.Definition) error {
		return store.CreateRun(ctx, db, run, definition)
	}, func(ctx context.Context, run engine.Run) error {
		return store.UpdateRun(ctx, db, run)
	})
	if err != nil {
		return fmt.Errorf("create runner: %w", err)
	}
	defer runner.Close()
	server := &http.Server{
		Addr: addr, Handler: hostGuard.Wrap(httpapi.New(definitions, runner, db, webDir)),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
		WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second,
	}
	defer server.Close() // Also closes connections if graceful shutdown times out.
	errorsCh := make(chan error, 1)
	go func() { errorsCh <- server.ListenAndServe() }()
	slog.Info("patchbay M0 starting", "address", addr, "workflows", len(definitions), "storage", "sqlite", "history", "sqlite", "database", dbPath, "max_active_runs", maxActiveRuns, "max_queued_runs", maxQueuedRuns)
	select {
	case err := <-errorsCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("HTTP server failed: %w", err)
		}
	case <-ctx.Done():
		slog.Info("shutting down")
		runner.Close()
		shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelShutdown()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("HTTP shutdown failed: %w", err)
		}
	}
	return nil
}
