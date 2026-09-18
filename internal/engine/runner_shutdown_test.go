package engine

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"testing/synctest"
	"time"

	"patchbay/internal/workflow"
)

func TestShutdownKeepsCompletedExecutionOutcome(t *testing.T) {
	for _, test := range []struct {
		name, pauseStep, phase string
		executorError          error
		progressError          error
		finalSaveFails         bool
		wantStatus             string
		wantExecuted           int
	}{
		{
			name: "final executor succeeds", pauseStep: "two", phase: "executor",
			wantStatus: "succeeded", wantExecuted: 2,
		},
		{
			name: "final result save canceled", pauseStep: "two", phase: "result save",
			progressError: context.Canceled, wantStatus: "succeeded", wantExecuted: 2,
		},
		{
			name: "final result save succeeds", pauseStep: "two", phase: "result save",
			wantStatus: "succeeded", wantExecuted: 2,
		},
		{
			name: "final executor canceled", pauseStep: "two", phase: "executor",
			executorError: context.Canceled, wantStatus: "canceled", wantExecuted: 2,
		},
		{
			name: "earlier executor succeeds", pauseStep: "one", phase: "executor",
			wantStatus: "canceled", wantExecuted: 1,
		},
		{
			name: "final saves exhausted", pauseStep: "two", phase: "result save",
			progressError: context.Canceled, finalSaveFails: true, wantStatus: "succeeded", wantExecuted: 2,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			// The fake clock advances retry delays while Close waits for finalization.
			synctest.Test(t, func(t *testing.T) {
				paused := make(chan struct{})
				var executed []string
				var finalAttempts []Run
				// Unhealthy is still a successful execution of a check.
				output := workflow.HTTPResult{Healthy: false, StatusCode: 503, Reason: "service unavailable"}
				runner, err := New(1, 0, func(ctx context.Context, step workflow.Step) (workflow.HTTPResult, error) {
					executed = append(executed, step.ID)
					if test.phase == "executor" && step.ID == test.pauseStep {
						close(paused)
						<-ctx.Done()
						if test.executorError != nil {
							return workflow.HTTPResult{}, test.executorError
						}
					}
					return output, nil
				}, nil, func(ctx context.Context, run Run) error {
					if run.Status == "running" {
						if test.phase == "result save" && run.Steps[1].Status == "succeeded" {
							close(paused)
							<-ctx.Done()
							return test.progressError
						}
						return nil
					}
					deadline, hasDeadline := ctx.Deadline()
					if !hasDeadline || ctx.Err() != nil || time.Until(deadline) <= 0 || time.Until(deadline) > 5*time.Second {
						t.Error("final save must have a fresh context bounded by five seconds")
					}
					finalAttempts = append(finalAttempts, run)
					if test.finalSaveFails {
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
				select {
				case <-paused:
				case <-time.After(time.Second):
					t.Fatal("run did not reach the requested shutdown point")
				}
				runner.Close() // Cancel the paused operation and join all final-save attempts.
				finished, ok := runner.Get(started.ID)
				if !ok || finished.Status != test.wantStatus || finished.Error != "" || finished.FinishedAt == nil || finished.FinalSaveFailed != test.finalSaveFails || len(finished.Steps) != 2 {
					t.Fatalf("shutdown changed the execution outcome: %+v", finished)
				}
				wantExecuted := []string{"one", "two"}[:test.wantExecuted]
				if !reflect.DeepEqual(executed, wantExecuted) {
					t.Fatalf("shutdown started or repeated an action: got %v, want %v", executed, wantExecuted)
				}
				wantAttempts := 1
				if test.finalSaveFails {
					wantAttempts = 3
				}
				if len(finalAttempts) != wantAttempts {
					t.Fatalf("got %d final-save attempts, want %d", len(finalAttempts), wantAttempts)
				}
				// The warning is memory-only; every save attempts the same completed result.
				finalSnapshot := finished
				finalSnapshot.FinalSaveFailed = false
				for _, attempt := range finalAttempts {
					if !reflect.DeepEqual(attempt, finalSnapshot) {
						t.Fatalf("final save changed the completed snapshot: %+v", attempt)
					}
				}
				for i, step := range finished.Steps {
					if i >= test.wantExecuted {
						if step.Status != "skipped" || step.StartedAt != nil || step.FinishedAt != nil || step.Output != nil || step.Error != "" {
							t.Fatalf("unexecuted step was not skipped cleanly: %+v", step)
						}
					} else if step.ID == test.pauseStep && test.executorError != nil {
						if step.Status != "canceled" || step.StartedAt == nil || step.FinishedAt == nil || step.Output != nil || step.Error != test.executorError.Error() {
							t.Fatalf("interrupted executor was not recorded as canceled: %+v", step)
						}
					} else if step.Status != "succeeded" || step.StartedAt == nil || step.FinishedAt == nil || step.Error != "" || !reflect.DeepEqual(step.Output, &output) {
						t.Fatalf("shutdown lost a completed step result: %+v", step)
					}
				}
			})
		})
	}
}
