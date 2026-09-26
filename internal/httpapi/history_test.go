package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	runner, err := engine.New(2, 0, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()
	handler := New(testDefinitions(), runner, nil, db, t.TempDir())
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

func TestProgressSaveFailureReasonSurvivesDatabaseReopen(t *testing.T) {
	for _, test := range []struct {
		name         string
		stepStatus   string
		wantExecuted int
		wantError    string
	}{
		{"before step", "running", 0, `Could not save progress before step "Check health". Execution stopped. Check server logs.`},
		{"after step", "succeeded", 1, `Could not save the result of step "Check health". Execution stopped. Check server logs.`},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "history.db")
			db := openTestDB(t, path)
			// Reject a real step progress write. The run's terminal status lets its
			// final save succeed even when it includes that same completed step.
			const privateError = "forced progress save failure: private storage detail"
			_, err := db.ExecContext(context.Background(), fmt.Sprintf(`CREATE TRIGGER reject_progress_save
                BEFORE UPDATE OF status ON run_steps
                WHEN NEW.step_id = 'health' AND NEW.status = '%s'
                    AND (SELECT status FROM runs WHERE id = NEW.run_id) = 'running'
                BEGIN SELECT RAISE(ABORT, 'forced progress save failure: private storage detail'); END`, test.stepStatus))
			if err != nil {
				t.Fatal(err)
			}
			definitions := testDefinitions()
			later := definitions[0].Steps[0]
			later.ID, later.Name = "later", "Check later"
			definitions[0].Steps = append(definitions[0].Steps, later)
			output := workflow.CheckResult{Healthy: true, StatusCode: 200, Reason: "checked once"}
			executed := 0
			var progressError error
			finalSaved := make(chan error, 3)
			runner, err := engine.New(1, 0, func(context.Context, workflow.Step) (workflow.CheckResult, error) {
				executed++
				return output, nil
			}, func(ctx context.Context, run engine.Run, definition workflow.Definition) error {
				return store.CreateRun(ctx, db, run, definition)
			}, func(ctx context.Context, run engine.Run) error {
				saveError := store.UpdateRun(ctx, db, run)
				if run.Status == "running" && saveError != nil {
					progressError = saveError
				}
				if run.FinishedAt != nil {
					finalSaved <- saveError
				}
				return saveError
			})
			if err != nil {
				t.Fatal(err)
			}
			defer runner.Close()
			started, err := runner.Start(definitions[0])
			if err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-finalSaved:
				if err != nil {
					t.Fatalf("final save did not recover from the progress failure: %v", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("run did not reach its final save")
			}
			runner.Close() // Join the callbacks before inspecting their observations.
			if progressError == nil || !strings.Contains(progressError.Error(), privateError) || executed != test.wantExecuted {
				t.Fatalf("wrong failure or extra actions: error=%v executed=%d", progressError, executed)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			db = openTestDB(t, path)
			emptyRunner, err := engine.New(1, 0, nil, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer emptyRunner.Close()
			handler := New(definitions, emptyRunner, nil, db, t.TempDir())
			for _, route := range []string{"/api/runs", "/api/runs/" + started.ID} {
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, route, nil))
				if response.Code != http.StatusOK || strings.Contains(response.Body.String(), privateError) {
					t.Fatalf("%s: history missing or private storage details leaked: %d %s", route, response.Code, response.Body.String())
				}
				var saved engine.Run
				if route == "/api/runs" {
					var listed []engine.Run
					if err := json.Unmarshal(response.Body.Bytes(), &listed); err != nil {
						t.Fatal(err)
					}
					if len(listed) != 1 {
						t.Fatalf("expected one saved run, got %d", len(listed))
					}
					saved = listed[0]
				} else if err := json.Unmarshal(response.Body.Bytes(), &saved); err != nil {
					t.Fatal(err)
				}
				if saved.ID != started.ID || saved.Status != "failed" || saved.Error != test.wantError || saved.FinalSaveFailed || saved.FinishedAt == nil || len(saved.Steps) != 2 {
					t.Fatalf("%s: progress failure reason or final state was lost: %+v", route, saved)
				}
				for i, step := range saved.Steps {
					if i < test.wantExecuted {
						if step.Status != "succeeded" || step.Error != "" || step.StartedAt == nil || step.FinishedAt == nil || !reflect.DeepEqual(step.Output, &output) {
							t.Fatalf("%s: completed step result was lost: %+v", route, step)
						}
					} else if step.Status != "skipped" || step.Error != "" || step.StartedAt != nil || step.FinishedAt != nil || step.Output != nil {
						t.Fatalf("%s: unexecuted step was not skipped cleanly: %+v", route, step)
					}
				}
			}
			if len(emptyRunner.List()) != 0 {
				t.Fatal("history reads repopulated the execution runner")
			}
		})
	}
}

func TestEmptyAndMissingHistory(t *testing.T) {
	db := openTestDB(t, filepath.Join(t.TempDir(), "history.db"))
	runner, err := engine.New(2, 0, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()
	handler := New(testDefinitions(), runner, nil, db, t.TempDir())
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
			runner, err := engine.New(2, 0, nil, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer runner.Close()
			handler := New(testDefinitions(), runner, nil, db, t.TempDir())
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
		Status: "succeeded", CreatedAt: started, StartedAt: started, FinishedAt: &finished,
		Steps: []engine.StepRun{{
			ID: step.ID, Name: step.Name, Status: "succeeded", StartedAt: &started, FinishedAt: &finished,
			Output: &workflow.CheckResult{Healthy: true, StatusCode: 200, DurationMS: 42, Reason: "expected status"},
		}},
	}
}

func TestHistoryRecoversAfterFinalSaveFailure(t *testing.T) {
	db := openTestDB(t, filepath.Join(t.TempDir(), "history.db"))
	// Reject a real terminal UPDATE, then remove the failure for the next attempt.
	_, err := db.ExecContext(context.Background(), `CREATE TRIGGER reject_final_save
        BEFORE UPDATE OF status ON runs WHEN NEW.status = 'succeeded'
        BEGIN SELECT RAISE(ABORT, 'forced final save failure'); END`)
	if err != nil {
		t.Fatal(err)
	}
	definitions := testDefinitions()
	output := workflow.CheckResult{Healthy: true, StatusCode: 200, Reason: "checked once"}
	executed := 0
	var finalAttempts []engine.Run
	var firstError error
	runner, err := engine.New(1, 0, func(context.Context, workflow.Step) (workflow.CheckResult, error) {
		executed++
		return output, nil
	}, func(ctx context.Context, run engine.Run, definition workflow.Definition) error {
		return store.CreateRun(ctx, db, run, definition)
	}, func(ctx context.Context, run engine.Run) error {
		saveError := store.UpdateRun(ctx, db, run)
		if run.Status == "succeeded" {
			finalAttempts = append(finalAttempts, run)
			if len(finalAttempts) == 1 {
				firstError = saveError
				_, cleanupError := db.ExecContext(ctx, "DROP TRIGGER reject_final_save")
				return errors.Join(saveError, cleanupError)
			}
		}
		return saveError
	})
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()
	started, err := runner.Start(definitions[0])
	if err != nil {
		t.Fatal(err)
	}
	handler := New(definitions, runner, nil, db, t.TempDir())
	deadline := time.After(2 * time.Second)
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	var detail engine.Run
	for {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/runs/"+started.ID, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("history read failed: %d %s", response.Code, response.Body.String())
		}
		if err := json.Unmarshal(response.Body.Bytes(), &detail); err != nil {
			t.Fatal(err)
		}
		if detail.Status == "succeeded" {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("API still reports %q after a temporary final-save failure", detail.Status)
		case <-ticker.C:
		}
	}
	runner.Close() // Join the executor before reading the callback's observations.
	if firstError == nil || !strings.Contains(firstError.Error(), "forced final save failure") || len(finalAttempts) != 2 || executed != 1 {
		t.Fatalf("want one failed save, one retry, and one execution: error=%v attempts=%d executed=%d", firstError, len(finalAttempts), executed)
	}
	if !reflect.DeepEqual(finalAttempts[0], finalAttempts[1]) {
		t.Fatal("retry changed the completed run snapshot")
	}
	if detail.ID != started.ID || detail.FinalSaveFailed || detail.FinishedAt == nil || finalAttempts[0].FinishedAt == nil || detail.FinishedAt.UnixMilli() != finalAttempts[0].FinishedAt.UnixMilli() || len(detail.Steps) != 1 {
		t.Fatalf("API lost the original run identity or finish time: %+v", detail)
	}
	step := detail.Steps[0]
	if step.Status != "succeeded" || step.FinishedAt == nil || !reflect.DeepEqual(step.Output, &output) {
		t.Fatalf("API lost the completed step result: %+v", step)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/runs", nil))
	var listed []engine.Run
	if err := json.Unmarshal(response.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || !reflect.DeepEqual(listed, []engine.Run{detail}) {
		t.Fatalf("list and detail history disagree after recovery: %d %s", response.Code, response.Body.String())
	}
}

func TestHistoryShowsUnsavedFinalResultWithoutHidingReadFailures(t *testing.T) {
	db := openTestDB(t, filepath.Join(t.TempDir(), "history.db"))
	older := historyRun("older", time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond))
	if err := store.CreateRun(context.Background(), db, older, testDefinitions()[0]); err != nil {
		t.Fatal(err)
	}
	// Leave reads/progress writes working, but reject every final save.
	const privateError = "forced final save failure: private storage detail"
	_, err := db.ExecContext(context.Background(), `CREATE TRIGGER reject_final_save
        BEFORE UPDATE OF status ON runs WHEN NEW.status = 'succeeded'
        BEGIN SELECT RAISE(ABORT, 'forced final save failure: private storage detail'); END`)
	if err != nil {
		t.Fatal(err)
	}
	output := workflow.CheckResult{Healthy: true, StatusCode: 200, Reason: "checked once"}
	finalStarted := make(chan struct{})
	executed, attempts := 0, 0
	runner, err := engine.New(1, 0, func(context.Context, workflow.Step) (workflow.CheckResult, error) {
		executed++
		return output, nil
	}, func(ctx context.Context, run engine.Run, definition workflow.Definition) error {
		return store.CreateRun(ctx, db, run, definition)
	}, func(ctx context.Context, run engine.Run) error {
		if run.Status == "succeeded" {
			attempts++
			if attempts == 1 {
				close(finalStarted)
			}
		}
		return store.UpdateRun(ctx, db, run)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()
	started, err := runner.Start(testDefinitions()[0])
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-finalStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("execution did not reach finalization")
	}
	// Execution has finished; join finalization before inspecting its observations.
	runner.Close()
	live, ok := runner.Get(started.ID)
	if !ok || !live.FinalSaveFailed || live.Status != "succeeded" || live.FinishedAt == nil || attempts != 3 || executed != 1 {
		t.Fatalf("wrong final result: %+v, attempts=%d executed=%d", live, attempts, executed)
	}
	if !reflect.DeepEqual(live.Steps[0].Output, &output) {
		t.Fatal("completed output was lost")
	}
	saved, err := store.GetRun(context.Background(), db, started.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Status != "running" || saved.FinishedAt != nil || saved.FinalSaveFailed {
		t.Fatalf("fixture must leave only stale progress in SQLite: %+v", saved)
	}
	handler := New(testDefinitions(), runner, nil, db, t.TempDir())
	for _, path := range []string{"/api/runs", "/api/runs/" + started.ID} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK || strings.Contains(response.Body.String(), privateError) {
			t.Fatalf("%s: result missing or storage details leaked: %d %s", path, response.Code, response.Body.String())
		}
		if path == "/api/runs" {
			var listed []engine.Run
			if err := json.Unmarshal(response.Body.Bytes(), &listed); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(listed, []engine.Run{live, older}) {
				t.Fatalf("list changed order, lost saved history, or omitted the live result: %+v", listed)
			}
		} else {
			var detail engine.Run
			if err := json.Unmarshal(response.Body.Bytes(), &detail); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(detail, live) {
				t.Fatalf("detail disagrees with the retained execution result: %+v", detail)
			}
		}
	}
	// Retaining a live result must not turn a broken history read into HTTP 200.
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	for path, message := range map[string]string{
		"/api/runs":               "could not load run history",
		"/api/runs/" + started.ID: "could not load run",
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		var body map[string]string
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if response.Code != http.StatusInternalServerError || !reflect.DeepEqual(body, map[string]string{"error": message}) {
			t.Fatalf("%s: read failure was hidden: %d %s", path, response.Code, response.Body.String())
		}
	}
}
