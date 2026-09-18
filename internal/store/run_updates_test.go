package store

import (
	"context"
	"path/filepath"
	"testing"

	"patchbay/internal/engine"
)

func TestUpdateRunSavesProgressAndPreservesWorkflow(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	definition, finished := runFixture()
	progress := finished
	progress.Status, progress.FinishedAt = "running", nil
	progress.Steps = make([]engine.StepRun, len(finished.Steps))
	for i, step := range finished.Steps {
		progress.Steps[i] = engine.StepRun{ID: step.ID, Name: step.Name, Status: "pending"}
	}
	if err := CreateRun(ctx, db, progress, definition); err != nil {
		t.Fatal(err)
	}

	progress.Steps[0].Status = "running"
	progress.Steps[0].StartedAt = &progress.StartedAt
	if err := UpdateRun(ctx, db, progress); err != nil {
		t.Fatal(err)
	}
	assertStoredRun(t, db, definition, progress)

	// Execution updates cannot rewrite the workflow or its original step names.
	finished.WorkflowID, finished.WorkflowName = "edited-id", "Edited workflow"
	finished.Steps[0].Name = "Edited step"
	// Saving the same reason again must succeed; an empty value clears it.
	for _, message := range []string{"Execution could not continue.", "Execution could not continue.", ""} {
		finished.Error = message
		if err := UpdateRun(ctx, db, finished); err != nil {
			t.Fatal(err)
		}
		_, expected := runFixture()
		expected.Error = message
		assertStoredRun(t, db, definition, expected)
	}
}

func TestUpdateRunRejectsMismatchedSnapshotsWithoutChangingHistory(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*engine.Run)
	}{
		{"missing run", func(run *engine.Run) { run.ID = "missing" }},
		{"missing step", func(run *engine.Run) { run.Steps = run.Steps[:2] }},
		{"extra step", func(run *engine.Run) { run.Steps = append(run.Steps, engine.StepRun{ID: "extra"}) }},
		{"unknown step", func(run *engine.Run) { run.Steps[2].ID = "missing" }},
		{"duplicate step", func(run *engine.Run) { run.Steps[2].ID = run.Steps[0].ID }},
		{"reordered steps", func(run *engine.Run) { run.Steps[0], run.Steps[1] = run.Steps[1], run.Steps[0] }},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			db, err := Open(ctx, filepath.Join(t.TempDir(), "history.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			definition, original := runFixture()
			original.Error = "Original run reason."
			if err := CreateRun(ctx, db, original, definition); err != nil {
				t.Fatal(err)
			}
			_, update := runFixture()
			update.Error = "Replacement run reason."
			update.Status, update.FinishedAt = "running", nil
			for i := range update.Steps {
				update.Steps[i].Status = "pending"
				update.Steps[i].StartedAt, update.Steps[i].FinishedAt = nil, nil
				update.Steps[i].Output, update.Steps[i].Error = nil, ""
			}
			test.change(&update)
			// A bad third step must undo updates to the run and earlier steps too.
			if err := UpdateRun(ctx, db, update); err == nil {
				t.Fatal("expected the mismatched snapshot to be rejected")
			}
			assertStoredRun(t, db, definition, original)
			var count int
			if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM runs").Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 1 {
				t.Fatalf("update created an unexpected run: %d runs", count)
			}
		})
	}
}
