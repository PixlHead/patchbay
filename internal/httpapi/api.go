// Package httpapi translates HTTP requests into calls to the runner.
package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"

	"patchbay/internal/engine"
	"patchbay/internal/workflow"
)

func New(definitions []workflow.Definition, runner *engine.Runner, webDir string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/workflows", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, definitions)
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
				if errors.Is(err, engine.ErrBusy) {
					code = http.StatusConflict
				} else if errors.Is(err, engine.ErrClosed) {
					code = http.StatusServiceUnavailable
				}
				writeError(w, code, err.Error())
				return
			}
			w.Header().Set("Location", "/api/runs/"+run.ID)
			writeJSON(w, http.StatusAccepted, run)
			return
		}
		writeError(w, http.StatusNotFound, "workflow not found")
	})
	mux.HandleFunc("GET /api/runs", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, runner.List())
	})
	mux.HandleFunc("GET /api/runs/{id}", func(w http.ResponseWriter, r *http.Request) {
		run, ok := runner.Get(r.PathValue("id"))
		if !ok {
			writeError(w, http.StatusNotFound, "run not found; runs are cleared when the server restarts")
			return
		}
		writeJSON(w, http.StatusOK, run)
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
