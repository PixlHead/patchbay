package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"patchbay/internal/engine"
	"patchbay/internal/httpapi"
	"patchbay/internal/nodes/httpcheck"
	"patchbay/internal/nodes/tcpcheck"
	"patchbay/internal/store"
	"patchbay/internal/workflow"
)

func TestMixedChecksPersistThroughAPIAndDatabaseReopen(t *testing.T) {
	var requests atomic.Int32
	httpTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer httpTarget.Close()
	tcpTarget, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer tcpTarget.Close()
	port := tcpTarget.Addr().(*net.TCPAddr).Port
	directory := t.TempDir()
	definitionJSON := fmt.Sprintf(`{
        "schemaVersion": 1, "id": "mixed", "name": "Mixed checks", "steps": [
            {"id": "http", "name": "HTTP", "type": "http.check",
                "config": {"url": %q, "expectedStatus": 200, "timeoutMs": 2000}},
            {"id": "tcp", "name": "TCP", "type": "tcp.check",
                "config": {"host": "127.0.0.1", "port": %d, "timeoutMs": 2000}}
        ]
    }`, httpTarget.URL, port)
	if err := os.WriteFile(filepath.Join(directory, "mixed.json"), []byte(definitionJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	definitions, err := workflow.Load(directory)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "history.db")
	db, err := store.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	finalSaved := make(chan error, 3)
	var executed []string
	execute := checkExecutor(httpcheck.New(), tcpcheck.New())
	runner, err := engine.New(1, 0, func(ctx context.Context, step workflow.Step) (workflow.CheckResult, error) {
		executed = append(executed, step.ID)
		return execute(ctx, step)
	}, func(ctx context.Context, run engine.Run, definition workflow.Definition) error {
		return store.CreateRun(ctx, db, run, definition)
	}, func(ctx context.Context, run engine.Run) error {
		err := store.UpdateRun(ctx, db, run)
		if run.FinishedAt != nil {
			finalSaved <- err
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()
	handler := httpapi.New(definitions, runner, nil, db, "")
	request := httptest.NewRequest(http.MethodPost, "/api/workflows/mixed/runs", nil)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var started engine.Run
	if err := json.Unmarshal(response.Body.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusAccepted || started.ID == "" {
		t.Fatalf("mixed workflow was not accepted: %d %s", response.Code, response.Body.String())
	}
	select {
	case err := <-finalSaved:
		if err != nil {
			t.Fatalf("final snapshot was not saved: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("mixed checks did not reach their final save")
	}
	runner.Close() // Join execution before reading observations or closing SQLite.
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := tcpTarget.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	connection, err := tcpTarget.AcceptTCP()
	if err != nil {
		t.Fatalf("TCP check never reached its target: %v", err)
	}
	connection.Close()
	reopened, err := store.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	idleRunner, err := engine.New(1, 0, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer idleRunner.Close()
	// No current definitions or in-memory runs can supply the saved result type.
	history := httpapi.New(nil, idleRunner, nil, reopened, "")
	response = httptest.NewRecorder()
	history.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/runs/"+started.ID, nil))
	var saved engine.Run
	if err := json.Unmarshal(response.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || saved.ID != started.ID || saved.Status != "succeeded" || saved.Error != "" || saved.FinalSaveFailed || saved.FinishedAt == nil || len(saved.Steps) != 2 {
		t.Fatalf("saved mixed run was lost: %d %s", response.Code, response.Body.String())
	}
	for i, id := range []string{"http", "tcp"} {
		step := saved.Steps[i]
		if step.ID != id || step.Status != "succeeded" || step.Error != "" || step.StartedAt == nil || step.FinishedAt == nil || step.Output == nil || !step.Output.Healthy {
			t.Fatalf("saved step order or completed output was lost: %+v", step)
		}
	}
	if saved.Steps[1].StartedAt.Before(*saved.Steps[0].FinishedAt) {
		t.Fatal("mixed steps overlapped")
	}
	httpResult, tcpResult := saved.Steps[0].Output, saved.Steps[1].Output
	if httpResult.Type != "" || httpResult.URL != httpTarget.URL || httpResult.ExpectedStatus != 200 || httpResult.StatusCode != 200 {
		t.Fatalf("legacy-compatible HTTP output changed: %+v", httpResult)
	}
	if tcpResult.Type != "tcp.check" || tcpResult.Host != "127.0.0.1" || tcpResult.Port != port || tcpResult.URL != "" || tcpResult.ExpectedStatus != 0 || tcpResult.StatusCode != 0 {
		t.Fatalf("TCP output was not independently identifiable: %+v", tcpResult)
	}
	response = httptest.NewRecorder()
	history.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/runs", nil))
	var listed []engine.Run
	if err := json.Unmarshal(response.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || !reflect.DeepEqual(listed, []engine.Run{saved}) {
		t.Fatalf("history list and detail disagree: %d %s", response.Code, response.Body.String())
	}
	if !reflect.DeepEqual(executed, []string{"http", "tcp"}) || requests.Load() != 1 || len(idleRunner.List()) != 0 {
		t.Fatalf("actions repeated or history repopulated the runner: executions=%v HTTP requests=%d", executed, requests.Load())
	}
}
