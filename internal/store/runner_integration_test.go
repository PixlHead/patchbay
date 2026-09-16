package store

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"patchbay/internal/engine"
	"patchbay/internal/workflow"
)

func TestRunnerPersistsExecutionToSQLite(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "runs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	definition, _ := runFixture()
	var id string
	completed := make(chan struct{}, 1)
	runner, err := engine.New(2, 0, func(ctx context.Context, step workflow.Step) (workflow.HTTPResult, error) {
		saved, err := GetRun(ctx, db, id)
		if err != nil {
			return workflow.HTTPResult{}, err
		}
		for i, result := range saved.Steps {
			if result.ID == step.ID {
				if result.Status != "running" || result.StartedAt == nil || (i > 0 && saved.Steps[i-1].Status != "succeeded") {
					return workflow.HTTPResult{}, fmt.Errorf("step executed before its progress was saved: %+v", saved)
				}
			}
		}
		return workflow.HTTPResult{Healthy: true, StatusCode: 200}, nil
	}, func(ctx context.Context, run engine.Run, definition workflow.Definition) error {
		id = run.ID
		return CreateRun(ctx, db, run, definition)
	}, func(ctx context.Context, run engine.Run) error {
		if err := UpdateRun(ctx, db, run); err != nil {
			return err
		}
		if run.Status != "running" {
			completed <- struct{}{}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()
	if _, err := runner.Start(definition); err != nil {
		t.Fatal(err)
	}
	select {
	case <-completed:
	case <-time.After(2 * time.Second):
		t.Fatal("no final snapshot was saved")
	}
	runner.Close()
	saved, err := GetRun(ctx, db, id)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Status != "succeeded" || saved.FinishedAt == nil || len(saved.Steps) != len(definition.Steps) {
		t.Fatalf("incomplete saved run: %+v", saved)
	}
	for _, step := range saved.Steps {
		if step.Status != "succeeded" || step.StartedAt == nil || step.FinishedAt == nil || step.Output == nil || step.Output.StatusCode != 200 {
			t.Fatalf("incomplete saved step: %+v", step)
		}
	}
}
