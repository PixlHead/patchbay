package store

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"patchbay/internal/engine"
	"patchbay/internal/workflow"
)

func TestQueuedRunPersistsBeforeExecutionAndKeepsCreationTime(t *testing.T) {
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "queue.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	release := make(chan struct{})
	completed := make(chan struct{}, 1)
	var idsMu sync.Mutex
	ids := make(map[string]string)
	runner, err := engine.New(1, 1, func(ctx context.Context, step workflow.Step) (workflow.HTTPResult, error) {
		// Both definitions have one step whose ID also identifies its workflow.
		idsMu.Lock()
		id := ids[step.ID]
		idsMu.Unlock()
		saved, err := GetRun(ctx, db, id)
		if err != nil {
			return workflow.HTTPResult{}, err
		}
		if saved.Status != "running" || saved.StartedAt.IsZero() || saved.Steps[0].Status != "running" {
			return workflow.HTTPResult{}, fmt.Errorf("execution began before promotion was saved: %+v", saved)
		}
		if step.ID == "a" {
			select {
			case <-release:
			case <-ctx.Done():
				return workflow.HTTPResult{}, ctx.Err()
			}
		}
		return workflow.HTTPResult{Healthy: true}, nil
	}, func(ctx context.Context, run engine.Run, definition workflow.Definition) error {
		idsMu.Lock()
		ids[definition.ID] = run.ID
		idsMu.Unlock()
		return CreateRun(ctx, db, run, definition)
	}, func(ctx context.Context, run engine.Run) error {
		if err := UpdateRun(ctx, db, run); err != nil {
			return err
		}
		if run.WorkflowID == "b" && run.Status != "running" {
			completed <- struct{}{}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()
	var queued engine.Run
	for _, id := range []string{"a", "b"} {
		definition, _ := runFixture()
		definition.ID, definition.Steps = id, definition.Steps[:1]
		definition.Steps[0].ID = id
		run, err := runner.Start(definition)
		if err != nil {
			t.Fatal(err)
		}
		if id == "b" {
			queued = run
			assertStoredRun(t, db, definition, queued)
		}
	}
	if queued.Status != "queued" || !queued.StartedAt.IsZero() {
		t.Fatalf("expected queued admission: %+v", queued)
	}
	close(release)
	select {
	case <-completed:
	case <-time.After(2 * time.Second):
		t.Fatal("queued workflow never completed")
	}
	runner.Close()
	finished, err := GetRun(context.Background(), db, queued.ID)
	if err != nil {
		t.Fatal(err)
	}
	if finished.Status != "succeeded" || finished.StartedAt.IsZero() || finished.CreatedAt.UnixMilli() != queued.CreatedAt.UnixMilli() || finished.StartedAt.Before(finished.CreatedAt) || finished.Steps[0].Output == nil {
		t.Fatalf("queued lifecycle lost timestamps or output: %+v", finished)
	}
}
