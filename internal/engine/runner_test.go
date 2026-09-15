package engine

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"patchbay/internal/workflow"
)

func example() workflow.Definition {
	return workflow.Definition{SchemaVersion: 1, ID: "test", Name: "Test", Steps: []workflow.Step{
		{ID: "one", Name: "One", Type: "http.check", Config: workflow.HTTPConfig{URL: "http://localhost", ExpectedStatus: 200, TimeoutMS: 100}},
		{ID: "two", Name: "Two", Type: "http.check", Config: workflow.HTTPConfig{URL: "http://localhost", ExpectedStatus: 200, TimeoutMS: 100}},
	}}
}

func awaitRun(t *testing.T, runner *Runner, id string) Run {
	t.Helper()
	deadline := time.After(2 * time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		run, ok := runner.Get(id)
		if !ok {
			t.Fatal("missing run")
		}
		if run.Status != "running" {
			return run
		}
		select {
		case <-deadline:
			t.Fatal("run did not finish")
		case <-ticker.C:
		}
	}
}

func TestSequentialChecksAndSnapshots(t *testing.T) {
	var called []string
	runner := New(func(ctx context.Context, step workflow.Step) (workflow.HTTPResult, error) {
		called = append(called, step.ID)
		return workflow.HTTPResult{Healthy: false, Reason: "maintenance"}, nil
	}, nil, nil)
	defer runner.Close()
	run, err := runner.Start(example())
	if err != nil {
		t.Fatal(err)
	}
	result := awaitRun(t, runner, run.ID)
	if result.Status != "succeeded" || !reflect.DeepEqual(called, []string{"one", "two"}) {
		t.Fatalf("wrong execution: %+v %v", result, called)
	}
	if result.Steps[1].StartedAt.Before(*result.Steps[0].FinishedAt) {
		t.Fatal("steps overlapped")
	}
	result.Steps[0].Output.Reason = "changed by reader"
	again, _ := runner.Get(run.ID)
	if again.Steps[0].Output.Reason != "maintenance" {
		t.Fatal("reader mutated stored result")
	}
}

func TestExecutionFailureSkipsLaterSteps(t *testing.T) {
	runner := New(func(context.Context, workflow.Step) (workflow.HTTPResult, error) {
		return workflow.HTTPResult{}, errors.New("executor failed")
	}, nil, nil)
	defer runner.Close()
	run, err := runner.Start(example())
	if err != nil {
		t.Fatal(err)
	}
	result := awaitRun(t, runner, run.ID)
	if result.Status != "failed" || result.Steps[1].Status != "skipped" {
		t.Fatalf("wrong failure handling: %+v", result)
	}
}

func TestBusyShutdownAndConcurrentReads(t *testing.T) {
	started := make(chan struct{})
	runner := New(func(ctx context.Context, step workflow.Step) (workflow.HTTPResult, error) {
		close(started)
		<-ctx.Done()
		return workflow.HTTPResult{}, ctx.Err()
	}, nil, nil)
	defer runner.Close()
	run, err := runner.Start(example())
	if err != nil {
		t.Fatal(err)
	}
	<-started
	if _, err := runner.Start(example()); !errors.Is(err, ErrBusy) {
		t.Fatalf("expected busy, got %v", err)
	}
	var readers sync.WaitGroup
	for range 10 {
		readers.Go(func() {
			for range 20 {
				runner.Get(run.ID)
				runner.List()
			}
		})
	}
	runner.Close()
	readers.Wait()
	result, _ := runner.Get(run.ID)
	if result.Status != "canceled" || result.Steps[1].Status != "skipped" {
		t.Fatalf("wrong shutdown result: %+v", result)
	}
	if _, err := runner.Start(example()); !errors.Is(err, ErrClosed) {
		t.Fatalf("accepted work after shutdown: %v", err)
	}
}

func TestInvalidDefinitionNeverRunsAndHistoryIsBounded(t *testing.T) {
	runner := New(func(context.Context, workflow.Step) (workflow.HTTPResult, error) {
		return workflow.HTTPResult{Healthy: true}, nil
	}, nil, nil)
	defer runner.Close()
	bad := example()
	bad.Steps = nil
	if _, err := runner.Start(bad); err == nil {
		t.Fatal("accepted invalid definition")
	}
	var first string
	for i := range 101 {
		run, err := runner.Start(example())
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = run.ID
		}
		awaitRun(t, runner, run.ID)
	}
	if len(runner.List()) != 100 {
		t.Fatal("history bound was not enforced")
	}
	if _, ok := runner.Get(first); ok {
		t.Fatal("oldest run was not evicted")
	}
}
