package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"patchbay/internal/engine"
	"patchbay/internal/workflow"
)

func TestCreateRunStoresSnapshotAndStepResults(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	definition, run := runFixture()
	if err := CreateRun(ctx, db, run, definition); err != nil {
		t.Fatal(err)
	}

	var workflowID, name, status, snapshot string
	var created, started, finished int64
	if err := db.QueryRowContext(ctx, `SELECT workflow_id, workflow_name, status,
        definition_json, created_at, started_at, finished_at FROM runs WHERE id = ?`, run.ID).
		Scan(&workflowID, &name, &status, &snapshot, &created, &started, &finished); err != nil {
		t.Fatal(err)
	}
	if workflowID != run.WorkflowID || name != run.WorkflowName || status != run.Status ||
		created != run.StartedAt.UnixMilli() || started != created || finished != run.FinishedAt.UnixMilli() {
		t.Fatalf("unexpected run metadata: %s %s %s %d %d %d", workflowID, name, status, created, started, finished)
	}
	var savedDefinition workflow.Definition
	if err := json.Unmarshal([]byte(snapshot), &savedDefinition); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(savedDefinition, definition) {
		t.Fatalf("workflow snapshot changed: %#v", savedDefinition)
	}

	rows, err := db.QueryContext(ctx, `SELECT step_id, name, status, started_at,
        finished_at, output_json, error FROM run_steps WHERE run_id = ? ORDER BY position`, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var savedSteps []engine.StepRun
	for rows.Next() {
		var step engine.StepRun
		var start, finish sql.NullInt64
		var output sql.NullString
		if err := rows.Scan(&step.ID, &step.Name, &step.Status, &start, &finish, &output, &step.Error); err != nil {
			t.Fatal(err)
		}
		if start.Valid {
			value := time.UnixMilli(start.Int64).UTC()
			step.StartedAt = &value
		}
		if finish.Valid {
			value := time.UnixMilli(finish.Int64).UTC()
			step.FinishedAt = &value
		}
		if output.Valid {
			step.Output = new(workflow.HTTPResult)
			if err := json.Unmarshal([]byte(output.String), step.Output); err != nil {
				t.Fatal(err)
			}
		}
		savedSteps = append(savedSteps, step)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(savedSteps, run.Steps) {
		t.Fatalf("step order or results changed: %#v", savedSteps)
	}
}

func TestCreateRunRollsBackWithoutReplacingHistory(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	definition, run := runFixture()
	if err := CreateRun(ctx, db, run, definition); err != nil {
		t.Fatal(err)
	}
	definition.Name = "Edited workflow"
	run.WorkflowName = definition.Name
	if err := CreateRun(ctx, db, run, definition); err == nil {
		t.Fatal("expected duplicate run ID to be rejected")
	}

	run.ID = "run-2"
	run.Steps[1].ID = run.Steps[0].ID // Fail after the run and first step were inserted.
	if err := CreateRun(ctx, db, run, definition); err == nil {
		t.Fatal("expected duplicate step ID to reject the entire run")
	}
	var runs, steps int
	if err := db.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM runs),
        (SELECT COUNT(*) FROM run_steps)`).Scan(&runs, &steps); err != nil {
		t.Fatal(err)
	}
	if runs != 1 || steps != 3 {
		t.Fatalf("failed inserts changed history: %d runs, %d steps", runs, steps)
	}
	var snapshot string
	if err := db.QueryRowContext(ctx, "SELECT definition_json FROM runs WHERE id = 'run-1'").Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	var savedDefinition workflow.Definition
	if err := json.Unmarshal([]byte(snapshot), &savedDefinition); err != nil {
		t.Fatal(err)
	}
	original, _ := runFixture()
	if !reflect.DeepEqual(savedDefinition, original) {
		t.Fatal("duplicate run insert replaced the original workflow snapshot")
	}
}

func runFixture() (workflow.Definition, engine.Run) {
	started := time.UnixMilli(1750000000123).UTC()
	finished := started.Add(time.Second)
	config := workflow.HTTPConfig{URL: "http://localhost/health", ExpectedStatus: 200, TimeoutMS: 1000}
	definition := workflow.Definition{
		SchemaVersion: 1, ID: "checks", Name: "Service checks", Description: "Check local services",
		Steps: []workflow.Step{
			{ID: "healthy", Name: "Healthy service", Type: "http.check", Config: config},
			{ID: "broken", Name: "Broken service", Type: "http.check", Config: config},
			{ID: "skipped", Name: "Skipped service", Type: "http.check", Config: config},
		},
	}
	run := engine.Run{
		ID: "run-1", WorkflowID: definition.ID, WorkflowName: definition.Name,
		Status: "failed", StartedAt: started, FinishedAt: &finished,
		Steps: []engine.StepRun{
			{ID: "healthy", Name: "Healthy service", Status: "succeeded", StartedAt: &started, FinishedAt: &finished,
				Output: &workflow.HTTPResult{Healthy: true, URL: config.URL, ExpectedStatus: 200, StatusCode: 200, DurationMS: 42, Reason: "expected status"}},
			{ID: "broken", Name: "Broken service", Status: "failed", StartedAt: &started, FinishedAt: &finished, Error: "connection refused"},
			{ID: "skipped", Name: "Skipped service", Status: "skipped"},
		},
	}
	return definition, run
}
