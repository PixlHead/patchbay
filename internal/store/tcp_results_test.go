package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"patchbay/internal/engine"
	"patchbay/internal/workflow"
)

func TestMixedHTTPAndTCPResultsSurviveUpdatesAndReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "history.db")
	db, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	definition, run := runFixture()
	definition.Steps = definition.Steps[:2]
	definition.Steps[1] = workflow.Step{
		ID: "tcp", Name: "TCP service", Type: "tcp.check",
		Config: workflow.CheckConfig{Host: "127.0.0.1", Port: 8080, TimeoutMS: 1000},
	}
	run.Status = "succeeded"
	run.Steps = run.Steps[:2]
	run.Steps[1] = engine.StepRun{
		ID: "tcp", Name: "TCP service", Status: "succeeded",
		StartedAt: &run.StartedAt, FinishedAt: run.FinishedAt,
		Output: &workflow.CheckResult{
			Type: "tcp.check", Healthy: true, Host: "127.0.0.1", Port: 8080,
			DurationMS: 12, Reason: "Connected to the TCP port",
		},
	}
	if err := CreateRun(ctx, db, run, definition); err != nil {
		t.Fatal(err)
	}
	assertStoredRun(t, db, definition, run)
	if saved, err := GetRun(ctx, db, run.ID); err != nil || !reflect.DeepEqual(saved, run) {
		t.Fatalf("mixed results changed after creation: got=%+v error=%v", saved, err)
	}

	// Unreachable TCP is health data, so the step and run still succeeded.
	// Updating its result must retain the HTTP output and the saved definition.
	run.Steps[1].Output = &workflow.CheckResult{
		Type: "tcp.check", Host: "127.0.0.1", Port: 8080,
		DurationMS: 3, Reason: "Connection refused",
	}
	if err := UpdateRun(ctx, db, run); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	assertStoredRun(t, reopened, definition, run)
	saved, err := GetRun(ctx, reopened, run.ID)
	if err != nil || !reflect.DeepEqual(saved, run) {
		t.Fatalf("mixed results changed after reopening: got=%+v error=%v", saved, err)
	}
	listed, err := ListRuns(ctx, reopened, 100)
	if err != nil || !reflect.DeepEqual(listed, []engine.Run{run}) {
		t.Fatalf("history lost mixed results: got=%+v error=%v", listed, err)
	}
}

func TestVersionTwoMigrationPreservesHTTPJSON(t *testing.T) {
	// These are legacy flat HTTP objects, including their original whitespace.
	const savedDefinition = `{
  "schemaVersion": 1, "id": "checks", "name": "Service checks",
  "description": "Check local services",
  "steps": [{"id":"healthy","name":"Healthy service","type":"http.check",
    "config":{"url":"http://localhost/health","expectedStatus":200,"timeoutMs":1000}}]
}`
	const savedOutput = `{
  "healthy": true, "url": "http://localhost/health", "expectedStatus": 200,
  "statusCode": 200, "durationMs": 42, "reason": "expected status"
}`
	for _, marked := range []bool{false, true} {
		name := "unmarked legacy"
		if marked {
			name = "marked legacy"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "legacy.db")
			legacy, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			defer legacy.Close()
			if _, err := legacy.ExecContext(ctx, initialSchema+runErrorMigration); err != nil {
				t.Fatal(err)
			}
			if marked {
				if _, err := legacy.ExecContext(ctx, fmt.Sprintf("PRAGMA application_id = %d", applicationID)); err != nil {
					t.Fatal(err)
				}
			}
			definition, run := runFixture()
			definition.Steps = definition.Steps[:1]
			run.Status = "succeeded"
			run.Steps = run.Steps[:1]
			if err := CreateRun(ctx, legacy, run, definition); err != nil {
				t.Fatal(err)
			}
			if _, err := legacy.ExecContext(ctx, "UPDATE runs SET definition_json = ? WHERE id = ?", savedDefinition, run.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := legacy.ExecContext(ctx, "UPDATE run_steps SET output_json = ? WHERE run_id = ?", savedOutput, run.ID); err != nil {
				t.Fatal(err)
			}
			var schemaCookie int
			if err := legacy.QueryRowContext(ctx, "PRAGMA schema_version").Scan(&schemaCookie); err != nil {
				t.Fatal(err)
			}
			if err := legacy.Close(); err != nil {
				t.Fatal(err)
			}

			// First open upgrades v2; reopening v3 must retain the same old JSON.
			for range 2 {
				db, err := Open(ctx, path)
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				var version, marker, reopenedCookie int
				for query, destination := range map[string]*int{
					"PRAGMA user_version": &version, "PRAGMA application_id": &marker, "PRAGMA schema_version": &reopenedCookie,
				} {
					if err := db.QueryRowContext(ctx, query).Scan(destination); err != nil {
						t.Fatal(err)
					}
				}
				if version != 3 || marker != applicationID || reopenedCookie != schemaCookie {
					t.Fatalf("migration changed table structure or lost its version gate: version=%d marker=%x schema=%d", version, marker, reopenedCookie)
				}
				var definitionJSON, outputJSON string
				if err := db.QueryRowContext(ctx, `SELECT definition_json, output_json
                    FROM runs JOIN run_steps ON run_steps.run_id = runs.id
                    WHERE runs.id = ?`, run.ID).Scan(&definitionJSON, &outputJSON); err != nil {
					t.Fatal(err)
				}
				if definitionJSON != savedDefinition || outputJSON != savedOutput {
					t.Fatal("migration rewrote a legacy workflow snapshot or HTTP result")
				}
				assertStoredRun(t, db, definition, run)
				if saved, err := GetRun(ctx, db, run.ID); err != nil || !reflect.DeepEqual(saved, run) {
					t.Fatalf("legacy HTTP history changed: got=%+v error=%v", saved, err)
				}
				if err := db.Close(); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
