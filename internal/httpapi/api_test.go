package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"patchbay/internal/engine"
	"patchbay/internal/store"
	"patchbay/internal/workflow"
)

// These API tests use a fake executor, so the configured URL is never contacted.
func testDefinitions() []workflow.Definition {
	return []workflow.Definition{{
		SchemaVersion: 1, ID: "test-workflow", Name: "Test workflow",
		Steps: []workflow.Step{{
			ID: "health", Name: "Check health", Type: "http.check",
			Config: workflow.HTTPConfig{URL: "http://service.invalid/health", ExpectedStatus: 200, TimeoutMS: 1000},
		}},
	}}
}

func TestRunLifecycleSurvivesRequestEnd(t *testing.T) {
	db := openTestDB(t, filepath.Join(t.TempDir(), "history.db"))
	definitions := testDefinitions()
	release := make(chan struct{})
	runner, err := engine.New(2, 0, func(ctx context.Context, step workflow.Step) (workflow.HTTPResult, error) {
		select {
		case <-release:
			return workflow.HTTPResult{Healthy: true, StatusCode: 200}, nil
		case <-ctx.Done():
			return workflow.HTTPResult{}, ctx.Err()
		}
	}, func(ctx context.Context, run engine.Run, definition workflow.Definition) error {
		return store.CreateRun(ctx, db, run, definition)
	}, func(ctx context.Context, run engine.Run) error {
		return store.UpdateRun(ctx, db, run)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()
	server := httptest.NewServer(New(definitions, runner, db, t.TempDir()))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "POST", server.URL+"/api/workflows/test-workflow/runs", nil)
	req.Header.Set("Content-Type", "application/json")
	response, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var started engine.Run
	if err := json.NewDecoder(response.Body).Decode(&started); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	cancel() // The originating request is finished; the run must continue.
	if response.StatusCode != 202 || response.Header.Get("Location") != "/api/runs/"+started.ID {
		t.Fatal("missing accepted run response")
	}
	stillRunning, _ := runner.Get(started.ID)
	if stillRunning.Status != "running" {
		t.Fatal("request cancellation stopped the run")
	}
	conflict, err := server.Client().Post(server.URL+"/api/workflows/test-workflow/runs", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	conflict.Body.Close()
	if conflict.StatusCode != 409 {
		t.Fatalf("expected 409, got %d", conflict.StatusCode)
	}
	close(release)
	deadline := time.After(2 * time.Second)
	for {
		response, err := server.Client().Get(server.URL + "/api/runs/" + started.ID)
		if err != nil {
			t.Fatal(err)
		}
		var finished engine.Run
		err = json.NewDecoder(response.Body).Decode(&finished)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if finished.Status == "succeeded" {
			break
		}
		select {
		case <-deadline:
			t.Fatal("run did not complete")
		case <-time.After(time.Millisecond):
		}
	}
}

func TestRunSaveFailureReturnsServerError(t *testing.T) {
	// The app's default slog handler writes through the standard logger.
	// Keep this test serial and restore its output so other tests are unaffected.
	var logs bytes.Buffer
	previousOutput := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(previousOutput)
	storageError := errors.New("write /private/patchbay-test.db: disk full")
	db := openTestDB(t, filepath.Join(t.TempDir(), "history.db"))
	executed := false
	runner, err := engine.New(2, 0, func(context.Context, workflow.Step) (workflow.HTTPResult, error) {
		executed = true
		return workflow.HTTPResult{}, nil
	}, func(context.Context, engine.Run, workflow.Definition) error {
		return storageError
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()
	handler := New(testDefinitions(), runner, db, t.TempDir())
	request := httptest.NewRequest(http.MethodPost, "/api/workflows/test-workflow/runs", nil)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	runner.Close()
	if response.Code != http.StatusInternalServerError || response.Header().Get("Location") != "" {
		t.Fatalf("expected a rejected run with HTTP 500, got %d: %s", response.Code, response.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if response.Header().Get("Content-Type") != "application/json" || len(body) != 1 || body["error"] != "Could not save run. Check server logs." {
		t.Fatalf("expected a fixed JSON error without storage details, got %s", response.Body.String())
	}
	for _, detail := range []string{"ERROR", "could not save new run", "workflow_id=test-workflow", storageError.Error()} {
		if !strings.Contains(logs.String(), detail) {
			t.Fatalf("server log is missing %q: %s", detail, logs.String())
		}
	}
	if executed || len(runner.List()) != 0 {
		t.Fatal("a run was executed or accepted despite its save failing")
	}
}

func TestAPIErrorResponses(t *testing.T) {
	db := openTestDB(t, filepath.Join(t.TempDir(), "history.db"))
	definitions := testDefinitions()
	runner, err := engine.New(2, 0, func(context.Context, workflow.Step) (workflow.HTTPResult, error) { return workflow.HTTPResult{}, nil }, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()
	handler := New(definitions, runner, db, t.TempDir())
	tests := []struct {
		method, path, contentType, body string
		want                            int
	}{
		{"GET", "/api/workflows", "", "", 200},
		{"GET", "/api/runs", "", "", 200},
		{"GET", "/api/runs/missing", "", "", 404},
		{"POST", "/api/workflows/missing/runs", "application/json", "", 404},
		{"POST", "/api/workflows/test-workflow/runs", "text/plain", "", 415},
		{"POST", "/api/workflows/test-workflow/runs", "application/json", `{}`, 400},
		{"GET", "/api/missing", "", "", 404},
	}
	for _, test := range tests {
		req := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
		req.Header.Set("Content-Type", test.contentType)
		result := httptest.NewRecorder()
		handler.ServeHTTP(result, req)
		if result.Code != test.want {
			t.Fatalf("%s %s: want %d, got %d", test.method, test.path, test.want, result.Code)
		}
		if result.Header().Get("Content-Type") != "application/json" {
			t.Fatal("API response was not JSON")
		}
		if _, err := io.ReadAll(result.Result().Body); err != nil {
			t.Fatal(err)
		}
	}
}

func openTestDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := store.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestConcurrentAdmissionErrorsAndShutdownPersistence(t *testing.T) {
	db := openTestDB(t, filepath.Join(t.TempDir(), "history.db"))
	started := make(chan struct{}, 6)
	runner, err := engine.New(2, 1, func(ctx context.Context, _ workflow.Step) (workflow.HTTPResult, error) {
		started <- struct{}{}
		<-ctx.Done()
		return workflow.HTTPResult{}, ctx.Err()
	}, func(ctx context.Context, run engine.Run, definition workflow.Definition) error {
		return store.CreateRun(ctx, db, run, definition)
	}, func(ctx context.Context, run engine.Run) error {
		return store.UpdateRun(ctx, db, run)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()
	var definitions []workflow.Definition
	for _, id := range []string{"a", "b", "c", "d"} {
		definition := testDefinitions()[0]
		definition.ID = id
		definitions = append(definitions, definition)
	}
	handler := New(definitions, runner, db, t.TempDir())
	for _, test := range []struct {
		id      string
		status  int
		message string
	}{
		{"a", http.StatusAccepted, ""},
		{"b", http.StatusAccepted, ""},
		{"a", http.StatusConflict, "this workflow is already queued or running"},
		{"c", http.StatusAccepted, ""},
		{"c", http.StatusConflict, "already queued or running"},
		{"d", http.StatusTooManyRequests, "queue limit 1"},
	} {
		request := httptest.NewRequest(http.MethodPost, "/api/workflows/"+test.id+"/runs", nil)
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != test.status || !strings.Contains(response.Body.String(), test.message) {
			t.Fatalf("start %s: got %d %s", test.id, response.Code, response.Body.String())
		}
		if (response.Header().Get("Location") != "") != (test.status == http.StatusAccepted) {
			t.Fatal("only accepted runs should have a Location header")
		}
	}
	queued, err := store.ListRuns(context.Background(), db, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, run := range queued {
		if run.WorkflowID == "c" && (run.Status != "queued" || !run.StartedAt.IsZero() || run.CreatedAt.IsZero()) {
			t.Fatalf("queued admission was not persisted: %+v", run)
		}
	}
	for range 2 {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("both workflows must execute concurrently")
		}
	}
	runner.Close()
	saved, err := store.ListRuns(context.Background(), db, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved) != 3 {
		t.Fatalf("rejected starts were saved or accepted runs disappeared: %+v", saved)
	}
	for _, run := range saved {
		if run.WorkflowID == "c" {
			if run.Status != "canceled" || run.FinishedAt == nil || !run.StartedAt.IsZero() || run.Steps[0].Status != "skipped" || run.Steps[0].StartedAt != nil {
				t.Fatalf("shutdown executed waiting work or lost its cancellation: %+v", run)
			}
			continue
		}
		if run.Status != "canceled" || run.FinishedAt == nil || len(run.Steps) != 1 || run.Steps[0].Status != "canceled" || run.Steps[0].FinishedAt == nil {
			t.Fatalf("Close returned without persisting cancellation: %+v", run)
		}
	}
}
