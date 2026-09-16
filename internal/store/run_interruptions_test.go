package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"patchbay/internal/engine"
	"patchbay/internal/workflow"
)

func TestMarkUnfinishedRunsInterruptedPreservesSavedResults(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if count, err := MarkUnfinishedRunsInterrupted(ctx, db); err != nil || count != 0 {
		t.Fatalf("empty history: got %d changed runs, %v", count, err)
	}

	definition, active := unfinishedRunFixture()
	_, beforeExecution := runFixture()
	beforeExecution.ID, beforeExecution.Status, beforeExecution.FinishedAt = "before-execution", "running", nil
	for i, step := range beforeExecution.Steps {
		beforeExecution.Steps[i] = engine.StepRun{ID: step.ID, Name: step.Name, Status: "pending"}
	}
	_, queued := runFixture()
	queued.ID, queued.Status, queued.StartedAt, queued.FinishedAt = "queued", "queued", time.Time{}, nil
	for i, step := range queued.Steps {
		queued.Steps[i] = engine.StepRun{ID: step.ID, Name: step.Name, Status: "pending"}
	}
	// Every step result was saved, but the final run update never completed.
	// The known failure and its error must survive interruption handling.
	_, afterExecution := runFixture()
	afterExecution.ID, afterExecution.Status, afterExecution.FinishedAt = "after-execution", "running", nil
	var terminalRuns []engine.Run
	for _, status := range []string{"succeeded", "failed", "canceled", "interrupted"} {
		_, run := runFixture()
		run.ID, run.Status = status, status
		// Even inconsistent steps in a terminal run are outside this helper's scope.
		run.Steps[1].Status, run.Steps[2].Status = "running", "pending"
		terminalRuns = append(terminalRuns, run)
	}
	for _, run := range append([]engine.Run{active, beforeExecution, afterExecution, queued}, terminalRuns...) {
		if err := CreateRun(ctx, db, run, definition); err != nil {
			t.Fatal(err)
		}
	}

	active.Status, active.Steps[1].Status, active.Steps[2].Status = "interrupted", "interrupted", "skipped"
	beforeExecution.Status = "interrupted"
	for i := range beforeExecution.Steps {
		beforeExecution.Steps[i].Status = "skipped"
	}
	afterExecution.Status, queued.Status = "interrupted", "interrupted"
	for i := range queued.Steps {
		queued.Steps[i].Status = "skipped"
	}
	// The second call must not change already reconciled history.
	for _, wantCount := range []int64{4, 0} {
		if count, err := MarkUnfinishedRunsInterrupted(ctx, db); err != nil || count != wantCount {
			t.Fatalf("want %d changed runs, got %d, %v", wantCount, count, err)
		}
		for _, expected := range append([]engine.Run{active, beforeExecution, afterExecution, queued}, terminalRuns...) {
			// Also checks definition JSON, creation time, step order, outputs and errors.
			assertStoredRun(t, db, definition, expected)
		}
	}
}

func TestMarkUnfinishedRunsInterruptedRollsBackStepChanges(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	definition, original := unfinishedRunFixture()
	if err := CreateRun(ctx, db, original, definition); err != nil {
		t.Fatal(err)
	}
	// Force the run update to fail after the helper has updated its steps.
	_, err = db.ExecContext(ctx, `CREATE TRIGGER reject_interruption
        BEFORE UPDATE OF status ON runs
        WHEN NEW.status = 'interrupted'
        BEGIN SELECT RAISE(ABORT, 'forced interruption failure'); END`)
	if err != nil {
		t.Fatal(err)
	}
	if count, err := MarkUnfinishedRunsInterrupted(ctx, db); err == nil || count != 0 {
		t.Fatalf("expected a rolled-back failure, got %d changed runs, %v", count, err)
	}
	assertStoredRun(t, db, definition, original)
}

func TestMarkUnfinishedRunsInterruptedHonorsCancellation(t *testing.T) {
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	definition, original := unfinishedRunFixture()
	if err := CreateRun(context.Background(), db, original, definition); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if count, err := MarkUnfinishedRunsInterrupted(ctx, db); !errors.Is(err, context.Canceled) || count != 0 {
		t.Fatalf("expected cancellation without changes, got %d changed runs, %v", count, err)
	}
	assertStoredRun(t, db, definition, original)
}

func unfinishedRunFixture() (workflow.Definition, engine.Run) {
	definition, run := runFixture()
	run.Status, run.FinishedAt = "running", nil
	run.Steps[1].Status, run.Steps[1].FinishedAt, run.Steps[1].Error = "running", nil, ""
	run.Steps[2].Status = "pending"
	return definition, run
}
