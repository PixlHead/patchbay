package engine

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"patchbay/internal/workflow"
)

func TestNewRejectsInvalidActiveRunLimits(t *testing.T) {
	for _, limit := range []int{0, -1} {
		runner, err := New(limit, 0, nil, nil, nil)
		if runner != nil {
			runner.Close()
		}
		if err == nil || runner != nil {
			t.Fatalf("accepted invalid limit %d: %v", limit, err)
		}
	}
}

func TestDifferentWorkflowsOverlapButTheirStepsStaySequential(t *testing.T) {
	releaseA, releaseB := make(chan struct{}), make(chan struct{})
	started := make(chan string, 10)
	blocked := map[string]<-chan struct{}{"a-one": releaseA, "b-one": releaseB}
	runner, err := New(2, 0, func(ctx context.Context, step workflow.Step) (workflow.CheckResult, error) {
		started <- step.ID
		if release, ok := blocked[step.ID]; ok {
			select {
			case <-release:
			case <-ctx.Done():
				return workflow.CheckResult{}, ctx.Err()
			}
		}
		return workflow.CheckResult{Healthy: true, Reason: step.ID}, nil
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()
	var runs []Run
	for _, id := range []string{"a", "b"} {
		run, err := runner.Start(concurrentDefinition(id))
		if err != nil {
			t.Fatal(err)
		}
		runs = append(runs, run)
	}
	seen := make(map[string]bool)
	for range 2 {
		select {
		case step := <-started:
			seen[step] = true
		case <-time.After(2 * time.Second):
			t.Fatal("both workflows must start before either first step is released")
		}
	}
	if !seen["a-one"] || !seen["b-one"] {
		t.Fatalf("later steps ran before their preceding steps finished: %v", seen)
	}
	for _, run := range runs {
		current, _ := runner.Get(run.ID)
		if current.Steps[1].Status != "pending" {
			t.Fatalf("second step started too early: %+v", current)
		}
	}
	close(releaseA)
	finished := awaitRun(t, runner, runs[0].ID)
	if finished.Status != "succeeded" || finished.Steps[1].StartedAt.Before(*finished.Steps[0].FinishedAt) {
		t.Fatalf("steps did not finish in sequence: %+v", finished)
	}
	// The finished workflow releases its own overlap guard and capacity slot.
	restarted, err := runner.Start(concurrentDefinition("a"))
	if err != nil {
		t.Fatalf("could not reuse the released slot while b remains active: %v", err)
	}
	awaitRun(t, runner, restarted.ID)
	if current, _ := runner.Get(runs[1].ID); current.Status != "running" {
		t.Fatal("finishing a different workflow stopped b")
	}
	close(releaseB)
	finishedB := awaitRun(t, runner, runs[1].ID)
	for _, run := range []Run{finished, finishedB} {
		for _, step := range run.Steps {
			if step.Output == nil || step.Output.Reason != step.ID {
				t.Fatalf("outputs crossed between concurrent runs: %+v", run)
			}
		}
	}
}

func TestSimultaneousStartsRespectCapacityAndWorkflowOverlap(t *testing.T) {
	for _, test := range []struct {
		duplicate bool
		queued    int
	}{{false, 0}, {false, 4}, {true, 4}} {
		t.Run(fmt.Sprintf("same-workflow=%t/queue=%d", test.duplicate, test.queued), func(t *testing.T) {
			var recorded atomic.Int32
			started := make(chan struct{}, 24)
			runner, err := New(3, test.queued, func(ctx context.Context, _ workflow.Step) (workflow.CheckResult, error) {
				started <- struct{}{}
				<-ctx.Done()
				return workflow.CheckResult{}, ctx.Err()
			}, func(context.Context, Run, workflow.Definition) error {
				recorded.Add(1)
				return nil
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer runner.Close()
			type outcome struct {
				run Run
				err error
			}
			results := make(chan outcome, 24)
			begin := make(chan struct{})
			for i := range 24 {
				go func() {
					<-begin
					id := fmt.Sprintf("workflow-%d", i)
					if test.duplicate {
						id = "same"
					}
					run, err := runner.Start(concurrentDefinition(id))
					runner.List() // Exercise snapshots during concurrent admission.
					results <- outcome{run, err}
				}()
			}
			close(begin)
			wantCount, wantActive, wantError := 3+test.queued, 3, ErrCapacity
			if test.duplicate {
				wantCount, wantActive, wantError = 1, 1, ErrWorkflowBusy
			}
			var accepted []Run
			for range 24 {
				select {
				case result := <-results:
					if result.err == nil {
						accepted = append(accepted, result.run)
					} else if !errors.Is(result.err, wantError) || result.run.ID != "" {
						t.Fatalf("unexpected rejection: %+v, %v", result.run, result.err)
					}
				case <-time.After(2 * time.Second):
					t.Fatal("concurrent admission did not finish")
				}
			}
			if len(accepted) != wantCount || int(recorded.Load()) != wantCount || len(runner.List()) != wantCount {
				t.Fatalf("want %d accepted and saved runs, got %d accepted, %d saves", wantCount, len(accepted), recorded.Load())
			}
			for range wantActive {
				select {
				case <-started:
				case <-time.After(2 * time.Second):
					t.Fatal("an accepted workflow did not begin execution")
				}
			}
			running, queued := 0, 0
			for _, run := range accepted {
				if run.Status == "running" {
					running++
				} else if run.Status == "queued" {
					queued++
				}
			}
			if running != wantActive || queued != wantCount-wantActive {
				t.Fatalf("wrong admission counts: %d running, %d queued", running, queued)
			}
			runner.Close()
			for _, run := range accepted {
				finished, _ := runner.Get(run.ID)
				wantStep := "canceled"
				if run.Status == "queued" {
					wantStep = "skipped"
					if !finished.StartedAt.IsZero() {
						t.Fatal("shutdown started queued work")
					}
				}
				if finished.Status != "canceled" || finished.Steps[0].Status != wantStep || finished.Steps[1].Status != "skipped" {
					t.Fatalf("shutdown did not finish every active run: %+v", finished)
				}
			}
		})
	}
}

func TestActiveRunSurvivesMemoryHistoryTrimming(t *testing.T) {
	runner, err := New(2, 0, func(ctx context.Context, step workflow.Step) (workflow.CheckResult, error) {
		if step.ID == "slow-one" {
			<-ctx.Done()
			return workflow.CheckResult{}, ctx.Err()
		}
		return workflow.CheckResult{Healthy: true}, nil
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()
	slow, err := runner.Start(concurrentDefinition("slow"))
	if err != nil {
		t.Fatal(err)
	}
	for range 101 {
		fast, err := runner.Start(concurrentDefinition("fast"))
		if err != nil {
			t.Fatal(err)
		}
		awaitRun(t, runner, fast.ID)
	}
	if run, ok := runner.Get(slow.ID); !ok || run.Status != "running" || len(runner.List()) != 100 {
		t.Fatal("history trimming discarded the active run or exceeded the limit")
	}
}

func TestRunKeepsItsSlotUntilFinalSaveReturns(t *testing.T) {
	finalStarted := make(chan struct{}, 2)
	var finalOnce sync.Once
	release := make(chan struct{})
	runner, err := New(1, 0, func(context.Context, workflow.Step) (workflow.CheckResult, error) {
		return workflow.CheckResult{Healthy: true}, nil
	}, nil, func(ctx context.Context, run Run) error {
		if run.Status == "running" {
			return nil
		}
		finalOnce.Do(func() { finalStarted <- struct{}{} })
		select {
		case <-release:
			return errors.New("final save unavailable")
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()
	// Release a blocked final save even if an assertion fails.
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	run, err := runner.Start(concurrentDefinition("a"))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-finalStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("final save did not start")
	}
	if _, err := runner.Start(concurrentDefinition("a")); !errors.Is(err, ErrWorkflowBusy) {
		t.Fatalf("overlap was allowed during the final save: %v", err)
	}
	if _, err := runner.Start(concurrentDefinition("b")); !errors.Is(err, ErrCapacity) {
		t.Fatalf("capacity was released during the final save: %v", err)
	}
	close(release)
	awaitRun(t, runner, run.ID)
	if _, err := runner.Start(concurrentDefinition("a")); err != nil {
		t.Fatalf("failed final save leaked the slot or overlap guard: %v", err)
	}
}

func concurrentDefinition(id string) workflow.Definition {
	definition := example()
	definition.ID = id
	for i := range definition.Steps {
		definition.Steps[i].ID = id + "-" + definition.Steps[i].ID
	}
	return definition
}

func TestQueuedRunsSurviveHistoryTrimming(t *testing.T) {
	runner, err := New(1, 101, func(ctx context.Context, _ workflow.Step) (workflow.CheckResult, error) {
		<-ctx.Done()
		return workflow.CheckResult{}, ctx.Err()
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()
	for i := range 102 {
		if _, err := runner.Start(concurrentDefinition(fmt.Sprintf("workflow-%d", i))); err != nil {
			t.Fatal(err)
		}
	}
	if len(runner.List()) != 102 {
		t.Fatal("unfinished entries were evicted")
	}
	runner.Close()
	if len(runner.List()) != 100 || runner.activeRuns != 0 || len(runner.busyWorkflows) != 0 {
		t.Fatal("shutdown left unfinished work or unbounded history")
	}
}

func TestNewRejectsNegativeQueueLimit(t *testing.T) {
	runner, err := New(1, -1, nil, nil, nil)
	if runner != nil {
		runner.Close()
	}
	if runner != nil || err == nil {
		t.Fatalf("accepted negative queue limit: %v", err)
	}
}
