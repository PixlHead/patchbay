package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"patchbay/internal/workflow"
)

func TestQueueIsFIFOAndWaitsForFinalSave(t *testing.T) {
	started := make(chan string, 8)
	releaseA, releaseB := make(chan struct{}), make(chan struct{})
	finalA := make(chan struct{})
	releaseFinal := make(chan struct{})
	runner, err := New(1, 2, func(ctx context.Context, step workflow.Step) (workflow.HTTPResult, error) {
		started <- step.ID
		var release <-chan struct{}
		switch step.ID {
		case "a-one":
			release = releaseA
		case "b-one":
			release = releaseB
		default:
			return workflow.HTTPResult{Healthy: true, Reason: step.Name}, nil
		}
		select {
		case <-release:
			return workflow.HTTPResult{Healthy: true, Reason: step.Name}, nil
		case <-ctx.Done():
			return workflow.HTTPResult{}, ctx.Err()
		}
	}, nil, func(ctx context.Context, run Run) error {
		if run.WorkflowID != "a" || run.Status == "running" {
			return nil
		}
		close(finalA)
		select {
		case <-releaseFinal:
			return errors.New("final save unavailable")
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()
	defer func() {
		select {
		case <-releaseFinal:
		default:
			close(releaseFinal)
		}
	}()
	a, err := runner.Start(concurrentDefinition("a"))
	if err != nil {
		t.Fatal(err)
	}
	waitForStep(t, started, "a-one")
	definition := concurrentDefinition("b")
	b, err := runner.Start(definition)
	if err != nil {
		t.Fatal(err)
	}
	definition.Steps[0].Name = "Edited after admission"
	c, err := runner.Start(concurrentDefinition("c"))
	if err != nil {
		t.Fatal(err)
	}
	for _, run := range []Run{b, c} {
		if run.Status != "queued" || !run.StartedAt.IsZero() || run.CreatedAt.IsZero() || run.FinishedAt != nil {
			t.Fatalf("waiting run claims to have started: %+v", run)
		}
		data, err := json.Marshal(run)
		if err != nil || strings.Contains(string(data), "startedAt") {
			t.Fatalf("queued JSON must omit start times: %s, %v", data, err)
		}
	}
	if _, err := runner.Start(concurrentDefinition("b")); !errors.Is(err, ErrWorkflowBusy) {
		t.Fatalf("queued duplicate accepted: %v", err)
	}
	if _, err := runner.Start(concurrentDefinition("d")); !errors.Is(err, ErrCapacity) {
		t.Fatalf("full queue accepted another run: %v", err)
	}
	close(releaseA)
	waitForStep(t, started, "a-two")
	select {
	case <-finalA:
	case <-time.After(2 * time.Second):
		t.Fatal("a did not reach its final save")
	}
	select {
	case step := <-started:
		t.Fatalf("queue advanced before the final save returned: %s", step)
	default:
	}
	close(releaseFinal)
	waitForStep(t, started, "b-one")
	if current, _ := runner.Get(c.ID); current.Status != "queued" {
		t.Fatalf("c overtook b: %+v", current)
	}
	close(releaseB)
	waitForStep(t, started, "b-two")
	waitForStep(t, started, "c-one")
	waitForStep(t, started, "c-two")
	for _, initial := range []Run{a, b, c} {
		finished := awaitRun(t, runner, initial.ID)
		if finished.Status != "succeeded" || !finished.CreatedAt.Equal(initial.CreatedAt) || finished.StartedAt.Before(finished.CreatedAt) {
			t.Fatalf("unexpected completed queue entry: %+v", finished)
		}
		if initial.ID == b.ID && finished.Steps[0].Output.Reason != "One" {
			t.Fatal("editing the submitted definition changed a queued run")
		}
	}
	if _, err := runner.Start(concurrentDefinition("b")); err != nil {
		t.Fatalf("completed workflow kept its overlap guard: %v", err)
	}
}

func TestQueueSaveFailuresDoNotBlockFollowingWork(t *testing.T) {
	release := make(chan struct{})
	started := make(chan string, 4)
	failedAdmission := false
	runner, err := New(1, 2, func(ctx context.Context, step workflow.Step) (workflow.HTTPResult, error) {
		started <- step.ID
		if step.ID == "a-one" {
			select {
			case <-release:
			case <-ctx.Done():
				return workflow.HTTPResult{}, ctx.Err()
			}
		}
		return workflow.HTTPResult{Healthy: true}, nil
	}, func(_ context.Context, run Run, _ workflow.Definition) error {
		if run.WorkflowID == "b" && !failedAdmission {
			failedAdmission = true
			return errors.New("initial save failed")
		}
		return nil
	}, func(_ context.Context, run Run) error {
		if run.WorkflowID == "b" && run.Status == "running" {
			return errors.New("promotion save failed")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()
	if _, err := runner.Start(concurrentDefinition("a")); err != nil {
		t.Fatal(err)
	}
	waitForStep(t, started, "a-one")
	if _, err := runner.Start(concurrentDefinition("b")); !errors.Is(err, ErrRecordRun) {
		t.Fatalf("failed save was accepted: %v", err)
	}
	b, err := runner.Start(concurrentDefinition("b"))
	if err != nil {
		t.Fatalf("failed admission retained an overlap guard: %v", err)
	}
	c, err := runner.Start(concurrentDefinition("c"))
	if err != nil {
		t.Fatalf("failed admission consumed queue space: %v", err)
	}
	close(release)
	waitForStep(t, started, "a-two")
	waitForStep(t, started, "c-one")
	waitForStep(t, started, "c-two")
	finished := awaitRun(t, runner, b.ID)
	if finished.Status != "failed" || finished.Steps[0].Status != "skipped" || finished.Steps[1].Status != "skipped" {
		t.Fatalf("failed promotion executed a step: %+v", finished)
	}
	if finished := awaitRun(t, runner, c.ID); finished.Status != "succeeded" {
		t.Fatalf("failed promotion stopped the next queued workflow: %+v", finished)
	}
}

func waitForStep(t *testing.T, started <-chan string, want string) {
	t.Helper()
	select {
	case got := <-started:
		if got != want {
			t.Fatalf("want next step %s, got %s", want, got)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("step %s did not start", want)
	}
}

func TestCloseWaitsForQueuedCancellationSave(t *testing.T) {
	finalStarted, releaseFinal := make(chan struct{}), make(chan struct{}, 1)
	runner, err := New(1, 1, func(ctx context.Context, _ workflow.Step) (workflow.HTTPResult, error) {
		<-ctx.Done()
		return workflow.HTTPResult{}, ctx.Err()
	}, nil, func(ctx context.Context, run Run) error {
		if run.WorkflowID != "b" {
			return nil
		}
		if run.Status != "canceled" || !run.StartedAt.IsZero() || run.Steps[0].Status != "skipped" {
			t.Errorf("shutdown promoted queued work: %+v", run)
		}
		if ctx.Err() != nil {
			t.Error("queued cleanup received an already canceled context")
		}
		close(finalStarted)
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
	for _, id := range []string{"a", "b"} {
		if _, err := runner.Start(concurrentDefinition(id)); err != nil {
			t.Fatal(err)
		}
	}
	closed := make(chan struct{}, 2)
	go func() { runner.Close(); closed <- struct{}{} }()
	select {
	case <-finalStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("queued cleanup did not start")
	}
	go func() { runner.Close(); closed <- struct{}{} }()
	select {
	case <-closed:
		t.Fatal("Close returned before queued cleanup finished")
	default:
	}
	if _, err := runner.Start(concurrentDefinition("c")); !errors.Is(err, ErrClosed) {
		t.Fatalf("accepted work during shutdown: %v", err)
	}
	releaseFinal <- struct{}{}
	for range 2 {
		select {
		case <-closed:
		case <-time.After(2 * time.Second):
			t.Fatal("Close did not finish")
		}
	}
}
