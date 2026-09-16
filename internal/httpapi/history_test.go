package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"patchbay/internal/engine"
	"patchbay/internal/store"
	"patchbay/internal/workflow"
)

func TestHistorySurvivesDatabaseReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	db := openTestDB(t, path)
	older := historyRun("older", time.UnixMilli(1_750_000_000_000).UTC())
	newer := historyRun("newer", older.StartedAt.Add(time.Hour))
	for _, run := range []engine.Run{older, newer} {
		if err := store.CreateRun(context.Background(), db, run, testDefinitions()[0]); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db = openTestDB(t, path)
	// An empty runner represents the new process; neither run exists in memory.
	// This test only sends GET requests, so it needs no executor.
	runner, err := engine.New(2, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()
	handler := New(testDefinitions(), runner, db, t.TempDir())
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/runs", nil))
	var listed []engine.Run
	if err := json.Unmarshal(response.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || !reflect.DeepEqual(listed, []engine.Run{newer, older}) {
		t.Fatalf("saved history was not returned newest first: %d %s", response.Code, response.Body.String())
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/runs/older", nil))
	var detail engine.Run
	if err := json.Unmarshal(response.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || !reflect.DeepEqual(detail, older) {
		t.Fatalf("saved run details were lost: %d %s", response.Code, response.Body.String())
	}
	if len(runner.List()) != 0 {
		t.Fatal("reading saved history repopulated the execution runner")
	}
}

func TestEmptyAndMissingHistory(t *testing.T) {
	db := openTestDB(t, filepath.Join(t.TempDir(), "history.db"))
	runner, err := engine.New(2, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()
	handler := New(testDefinitions(), runner, db, t.TempDir())
	for _, test := range []struct {
		path   string
		status int
		body   string
	}{
		{"/api/runs", http.StatusOK, `[]`},
		{"/api/runs/missing", http.StatusNotFound, `{"error":"run not found"}`},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))
		if response.Code != test.status || strings.TrimSpace(response.Body.String()) != test.body || response.Header().Get("Content-Type") != "application/json" {
			t.Fatalf("%s: unexpected history response: %d %s", test.path, response.Code, response.Body.String())
		}
	}
}

func TestHistoryReadFailuresReturnServerErrors(t *testing.T) {
	for _, failure := range []string{"closed database", "invalid saved output"} {
		t.Run(failure, func(t *testing.T) {
			db := openTestDB(t, filepath.Join(t.TempDir(), "history.db"))
			run := historyRun("saved", time.UnixMilli(1_750_000_000_000).UTC())
			if err := store.CreateRun(context.Background(), db, run, testDefinitions()[0]); err != nil {
				t.Fatal(err)
			}
			if failure == "closed database" {
				if err := db.Close(); err != nil {
					t.Fatal(err)
				}
			} else if _, err := db.ExecContext(context.Background(), "UPDATE run_steps SET output_json = ?", "{"); err != nil {
				t.Fatal(err)
			}
			runner, err := engine.New(2, nil, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer runner.Close()
			handler := New(testDefinitions(), runner, db, t.TempDir())
			for path, message := range map[string]string{
				"/api/runs":       "could not load run history",
				"/api/runs/saved": "could not load run",
			} {
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
				var body map[string]string
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if response.Code != http.StatusInternalServerError || !reflect.DeepEqual(body, map[string]string{"error": message}) {
					t.Fatalf("%s: storage failure was hidden or leaked details: %d %s", path, response.Code, response.Body.String())
				}
			}
		})
	}
}

func historyRun(id string, started time.Time) engine.Run {
	definition := testDefinitions()[0]
	finished := started.Add(time.Second)
	step := definition.Steps[0]
	return engine.Run{
		ID: id, WorkflowID: definition.ID, WorkflowName: definition.Name,
		Status: "succeeded", StartedAt: started, FinishedAt: &finished,
		Steps: []engine.StepRun{{
			ID: step.ID, Name: step.Name, Status: "succeeded", StartedAt: &started, FinishedAt: &finished,
			Output: &workflow.HTTPResult{Healthy: true, StatusCode: 200, DurationMS: 42, Reason: "expected status"},
		}},
	}
}
