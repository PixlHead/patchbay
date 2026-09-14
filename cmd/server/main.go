package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"patchbay/internal/engine"
	"patchbay/internal/httpapi"
	"patchbay/internal/nodes"
	"patchbay/internal/workflow"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "HTTP listen address")
	directory := flag.String("workflows", "workflows", "directory containing workflow JSON files")
	webDir := flag.String("web", "web/dist", "built frontend directory")
	flag.Parse()

	definitions, err := workflow.Load(*directory)
	if err != nil {
		slog.Error("invalid workflow configuration", "error", err)
		os.Exit(1)
	}
	httpNode := nodes.NewHTTP()
	runner := engine.New(httpNode.Execute)
	defer runner.Close()
	server := &http.Server{
		Addr: *addr, Handler: httpapi.New(definitions, runner, *webDir),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
		WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second,
	}
	stop, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	errorsCh := make(chan error, 1)
	go func() { errorsCh <- server.ListenAndServe() }()
	slog.Info("patchbay M0 starting", "address", *addr, "workflows", len(definitions), "storage", "memory")
	select {
	case err := <-errorsCh:
		if !errors.Is(err, http.ErrServerClosed) {
			slog.Error("HTTP server failed", "error", err)
			os.Exit(1)
		}
	case <-stop.Done():
		slog.Info("shutting down")
		runner.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			slog.Error("HTTP shutdown failed", "error", err)
		}
	}
}
