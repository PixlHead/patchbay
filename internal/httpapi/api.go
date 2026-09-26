// Package httpapi translates HTTP requests into execution and saved-history operations.
package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"patchbay/internal/engine"
	"patchbay/internal/store"
	"patchbay/internal/workflow"
)

// NextRunSource reports when a workflow's schedule will next start a run.
type NextRunSource interface {
	NextRun(workflowID string) (time.Time, bool)
}

type workflowSummary struct {
	workflow.Definition
	NextRunAt *time.Time `json:"nextRunAt,omitempty"`
}

// New reads saved history, overlaying retained results whose final save failed.
// A nil schedules source lists workflows without next-run times.
func New(definitions []workflow.Definition, runner *engine.Runner, schedules NextRunSource, db *sql.DB, webDir string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/workflows", func(w http.ResponseWriter, r *http.Request) {
		summaries := make([]workflowSummary, 0, len(definitions))
		for _, definition := range definitions {
			summary := workflowSummary{Definition: definition}
			if schedules != nil {
				if next, ok := schedules.NextRun(definition.ID); ok {
					summary.NextRunAt = &next
				}
			}
			summaries = append(summaries, summary)
		}
		writeJSON(w, http.StatusOK, summaries)
	})
	mux.HandleFunc("POST /api/workflows/{id}/runs", func(w http.ResponseWriter, r *http.Request) {
		// Start accepts no payload. Requiring JSON prevents cross-site HTML forms
		// from triggering checks on this unauthenticated localhost prototype.
		if r.Header.Get("Content-Type") != "application/json" {
			writeError(w, http.StatusUnsupportedMediaType, "use Content-Type: application/json and an empty request body")
			return
		}
		if r.ContentLength != 0 {
			writeError(w, http.StatusBadRequest, "this endpoint does not accept a request body")
			return
		}
		for _, definition := range definitions {
			if definition.ID != r.PathValue("id") {
				continue
			}
			run, err := runner.Start(definition)
			if err != nil {
				code := http.StatusBadRequest
				message := err.Error()
				if errors.Is(err, engine.ErrWorkflowBusy) {
					code = http.StatusConflict
				} else if errors.Is(err, engine.ErrCapacity) {
					code = http.StatusTooManyRequests
				} else if errors.Is(err, engine.ErrClosed) {
					code = http.StatusServiceUnavailable
				} else if errors.Is(err, engine.ErrRecordRun) {
					code = http.StatusInternalServerError
					slog.Error("could not save new run", "workflow_id", definition.ID, "error", err)
					message = "Could not save run. Check server logs."
				}
				writeError(w, code, message)
				return
			}
			w.Header().Set("Location", "/api/runs/"+run.ID)
			writeJSON(w, http.StatusAccepted, run)
			return
		}
		writeError(w, http.StatusNotFound, "workflow not found")
	})
	mux.HandleFunc("GET /api/runs", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		runs, err := store.ListRuns(ctx, db, 100)
		if err != nil {
			slog.Error("could not read run history", "error", err)
			writeError(w, http.StatusInternalServerError, "could not load run history")
			return
		}
		for i := range runs {
			runs[i] = preferUnsavedRun(runs[i], runner)
		}
		writeJSON(w, http.StatusOK, runs)
	})
	mux.HandleFunc("GET /api/runs/{id}", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		id := r.PathValue("id")
		run, err := store.GetRun(ctx, db, id)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "run not found")
			return
		}
		if err != nil {
			slog.Error("could not read run", "run_id", id, "error", err)
			writeError(w, http.StatusInternalServerError, "could not load run")
			return
		}
		writeJSON(w, http.StatusOK, preferUnsavedRun(run, runner))
	})
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "API endpoint not found")
	})
	files := http.FileServer(http.Dir(webDir))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if _, err := os.Stat(filepath.Join(webDir, "index.html")); err != nil {
			http.Error(w, "Frontend is not built. Run npm --prefix web install and npm --prefix web run build, or use the Vite dev server.", http.StatusServiceUnavailable)
			return
		}
		files.ServeHTTP(w, r)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		mux.ServeHTTP(w, r)
	})
}

// Keep database errors, list membership/order, and normal saved history intact.
// An unsaved result is available only until restart or memory-history eviction.
func preferUnsavedRun(saved engine.Run, runner *engine.Runner) engine.Run {
	if live, ok := runner.Get(saved.ID); ok && live.FinalSaveFailed {
		return live
	}
	return saved
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		// A disconnected client cannot affect an already-admitted background run.
		return
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
