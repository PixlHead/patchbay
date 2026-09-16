package engine

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"patchbay/internal/workflow"
)

func TestRunSavesProgressAndCompletion(t *testing.T) {
	for _, executionFails := range []bool{false, true} {
		name := "success"
		if executionFails {
			name = "execution failure"
		}
		t.Run(name, func(t *testing.T) {
			var updates []Run
			runner, err := New(2, func(context.Context, workflow.Step) (workflow.HTTPResult, error) {
				if executionFails {
					return workflow.HTTPResult{}, errors.New("connection refused")
				}
				return workflow.HTTPResult{Healthy: true, StatusCode: 200}, nil
			}, nil, func(_ context.Context, run Run) error {
				updates = append(updates, run)
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			defer runner.Close()
			started, err := runner.Start(example())
			if err != nil {
				t.Fatal(err)
			}
			finished := awaitRun(t, runner, started.ID)
			runner.Close()
			want := [][3]string{
				{"running", "running", "pending"},
				{"running", "succeeded", "pending"},
				{"running", "succeeded", "running"},
				{"running", "succeeded", "succeeded"},
				{"succeeded", "succeeded", "succeeded"},
			}
			if executionFails {
				want = [][3]string{
					{"running", "running", "pending"},
					{"running", "failed", "pending"},
					{"failed", "failed", "skipped"},
				}
			}
			var got [][3]string
			for _, update := range updates {
				got = append(got, [3]string{update.Status, update.Steps[0].Status, update.Steps[1].Status})
			}
			if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(updates[len(updates)-1], finished) {
				t.Fatalf("wrong saved progression: %v; final memory result: %+v", got, finished)
			}
			if updates[0].Steps[0].StartedAt == nil || updates[0].Steps[0].FinishedAt != nil || updates[0].Steps[0].Output != nil {
				t.Fatal("the starting snapshot is incomplete or was mutated by later execution")
			}
			result := updates[1].Steps[0]
			if result.FinishedAt == nil || finished.FinishedAt == nil {
				t.Fatal("completion timestamps were not saved")
			}
			if executionFails {
				if result.Error != "connection refused" {
					t.Fatalf("execution error was not saved: %+v", result)
				}
			} else if result.Output == nil || result.Output.StatusCode != 200 {
				t.Fatalf("output was not saved: %+v", result)
			}
		})
	}
}

func TestSaveFailureStopsLaterStepsWithoutReplayingActions(t *testing.T) {
	for _, test := range []struct {
		name         string
		failAt       int
		keepFailing  bool
		wantWrites   int
		wantExecuted int
		wantStatus   string
	}{
		{"before first step", 1, false, 2, 0, "failed"},
		{"after first step", 2, false, 3, 1, "failed"},
		{"progress and final save", 2, true, 3, 1, "failed"},
		{"final save only", 5, false, 5, 2, "succeeded"},
	} {
		t.Run(test.name, func(t *testing.T) {
			writes, executed := 0, 0
			var lastAttempt Run
			runner, err := New(2, func(context.Context, workflow.Step) (workflow.HTTPResult, error) {
				executed++
				return workflow.HTTPResult{Healthy: true, StatusCode: 200}, nil
			}, nil, func(_ context.Context, run Run) error {
				writes++
				lastAttempt = run
				if writes == test.failAt || (test.keepFailing && writes > test.failAt) {
					return errors.New("storage unavailable")
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			defer runner.Close()
			started, err := runner.Start(example())
			if err != nil {
				t.Fatal(err)
			}
			finished := awaitRun(t, runner, started.ID)
			runner.Close()
			if writes != test.wantWrites || executed != test.wantExecuted || finished.Status != test.wantStatus {
				t.Fatalf("got %d saves, %d executions, status %s", writes, executed, finished.Status)
			}
			if !reflect.DeepEqual(lastAttempt, finished) || finished.FinishedAt == nil || len(runner.activeWorkflows) != 0 {
				t.Fatal("final save was not attempted or the active slot was not released")
			}
			for i, step := range finished.Steps {
				if i < executed {
					if step.Status != "succeeded" || step.Output == nil || step.Output.StatusCode != 200 || step.FinishedAt == nil {
						t.Fatalf("completed output was lost: %+v", step)
					}
				} else if step.Status != "skipped" || step.StartedAt != nil || step.FinishedAt != nil {
					t.Fatalf("an unexecuted step was not skipped: %+v", step)
				}
			}
		})
	}
}

func TestShutdownSavesCancellationAndWaitsForFinalWrite(t *testing.T) {
	for _, phase := range []string{"saving", "executing"} {
		t.Run(phase, func(t *testing.T) {
			begun := make(chan struct{})
			finalBegun := make(chan struct{})
			releaseFinal := make(chan struct{}, 1)
			var saved Run
			executed := 0
			runner, err := New(2, func(ctx context.Context, _ workflow.Step) (workflow.HTTPResult, error) {
				executed++
				close(begun)
				<-ctx.Done()
				return workflow.HTTPResult{}, ctx.Err()
			}, nil, func(ctx context.Context, run Run) error {
				if run.Status == "running" {
					if phase == "saving" {
						close(begun)
						<-ctx.Done()
						return ctx.Err()
					}
					return nil
				}
				if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) <= 0 || ctx.Err() != nil {
					t.Error("final save needs a fresh context with a future deadline")
				}
				saved = run
				close(finalBegun)
				select {
				case <-releaseFinal:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})
			if err != nil {
				t.Fatal(err)
			}
			defer runner.Close()
			defer close(releaseFinal)
			started, err := runner.Start(example())
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-begun:
			case <-time.After(2 * time.Second):
				t.Fatal("execution did not reach the requested phase")
			}
			read := make(chan struct{})
			go func() {
				runner.Get(started.ID)
				runner.List()
				close(read)
			}()
			select {
			case <-read:
			case <-time.After(2 * time.Second):
				t.Fatal("history reads were blocked")
			}
			closed := make(chan struct{})
			go func() { runner.Close(); close(closed) }()
			select {
			case <-finalBegun:
			case <-time.After(2 * time.Second):
				t.Fatal("shutdown did not reach the final save")
			}
			select {
			case <-closed:
				t.Fatal("Close returned before the final save finished")
			default:
			}
			if current, _ := runner.Get(started.ID); current.Status != "running" {
				t.Fatal("terminal status was published before the final save finished")
			}
			releaseFinal <- struct{}{}
			select {
			case <-closed:
			case <-time.After(2 * time.Second):
				t.Fatal("Close did not finish after the final save")
			}
			finished, _ := runner.Get(started.ID)
			if !reflect.DeepEqual(saved, finished) || saved.Status != "canceled" || saved.FinishedAt == nil || saved.Steps[1].Status != "skipped" {
				t.Fatalf("wrong saved cancellation: %+v", saved)
			}
			if phase == "saving" {
				if executed != 0 || saved.Steps[0].Status != "skipped" || saved.Steps[0].StartedAt != nil {
					t.Fatal("a canceled pre-execution save allowed the step to run")
				}
			} else if executed != 1 || saved.Steps[0].Status != "canceled" || saved.Steps[0].FinishedAt == nil || saved.Steps[0].Error == "" {
				t.Fatal("the interrupted step was not recorded as canceled")
			}
		})
	}
}
