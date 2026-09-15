package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"patchbay/internal/engine"
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
	definitions := testDefinitions()
	release := make(chan struct{})
	runner := engine.New(func(ctx context.Context, step workflow.Step) (workflow.HTTPResult, error) {
		select {
		case <-release:
			return workflow.HTTPResult{Healthy: true, StatusCode: 200}, nil
		case <-ctx.Done():
			return workflow.HTTPResult{}, ctx.Err()
		}
	}, nil)
	defer runner.Close()
	server := httptest.NewServer(New(definitions, runner, t.TempDir()))
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
	executed := false
	runner := engine.New(func(context.Context, workflow.Step) (workflow.HTTPResult, error) {
		executed = true
		return workflow.HTTPResult{}, nil
	}, func(context.Context, engine.Run, workflow.Definition) error {
		return errors.New("storage unavailable")
	})
	defer runner.Close()
	handler := New(testDefinitions(), runner, t.TempDir())
	request := httptest.NewRequest(http.MethodPost, "/api/workflows/test-workflow/runs", nil)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	runner.Close()
	if response.Code != http.StatusInternalServerError || response.Header().Get("Location") != "" {
		t.Fatalf("expected a rejected run with HTTP 500, got %d: %s", response.Code, response.Body.String())
	}
	if executed || len(runner.List()) != 0 {
		t.Fatal("a run was executed or accepted despite its save failing")
	}
}

func TestAPIErrorResponses(t *testing.T) {
	definitions := testDefinitions()
	runner := engine.New(func(context.Context, workflow.Step) (workflow.HTTPResult, error) { return workflow.HTTPResult{}, nil }, nil)
	defer runner.Close()
	handler := New(definitions, runner, t.TempDir())
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
