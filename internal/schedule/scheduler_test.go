package schedule

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"patchbay/internal/engine"
	"patchbay/internal/workflow"
)

// The fake start functions below never contact this URL.
func definition(id string, schedule *workflow.Schedule) workflow.Definition {
	return workflow.Definition{SchemaVersion: 1, ID: id, Name: "Workflow " + id, Schedule: schedule, Steps: []workflow.Step{
		{ID: "check", Name: "Check", Type: "http.check", Config: workflow.CheckConfig{URL: "http://localhost/health", ExpectedStatus: 200, TimeoutMS: 1000}},
	}}
}

func every(cron string) *workflow.Schedule {
	return &workflow.Schedule{Cron: cron, Timezone: "UTC"}
}

// fakeClock hands each wait to the test, which decides when it fires.
type fakeClock struct {
	mu    sync.Mutex
	now   time.Time
	waits chan waitRequest
}

type waitRequest struct {
	duration time.Duration
	fire     chan time.Time
}

func newFakeClock(now time.Time) *fakeClock {
	return &fakeClock{now: now, waits: make(chan waitRequest, 16)}
}

func (c *fakeClock) Clock() Clock {
	return Clock{Now: c.Now, After: c.After}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) After(duration time.Duration) <-chan time.Time {
	fire := make(chan time.Time, 1)
	c.waits <- waitRequest{duration: duration, fire: fire}
	return fire
}

// fireAt moves the clock to the given instant, then delivers the pending wait.
func (c *fakeClock) fireAt(request waitRequest, at time.Time) {
	c.mu.Lock()
	c.now = at
	c.mu.Unlock()
	request.fire <- at
}

// nextWait returns the scheduler's next pending wait. A loop asks for its next
// wait only after the previous start returned, so this also joins that start.
func nextWait(t *testing.T, clock *fakeClock) waitRequest {
	t.Helper()
	select {
	case request := <-clock.waits:
		return request
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler did not wait for its next occurrence")
		return waitRequest{}
	}
}

func awaitStart(t *testing.T, started <-chan workflow.Definition) workflow.Definition {
	t.Helper()
	select {
	case d := <-started:
		return d
	case <-time.After(2 * time.Second):
		t.Fatal("scheduled run did not start")
		return workflow.Definition{}
	}
}

func TestStartsRunsAtOccurrencesAndSkipsMissedOnes(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 1, 1, 10, 3, 0, 0, time.UTC))
	started := make(chan workflow.Definition, 4)
	definitions := []workflow.Definition{definition("every-five", every("*/5 * * * *")), definition("manual", nil)}
	scheduler, err := New(definitions, func(d workflow.Definition) (engine.Run, error) {
		started <- d
		return engine.Run{ID: "run-" + d.ID}, nil
	}, clock.Clock())
	if err != nil {
		t.Fatal(err)
	}
	scheduler.Start()
	defer scheduler.Close()

	// The 10:00 occurrence passed before the scheduler started; it is not replayed.
	if next, ok := scheduler.NextRun("every-five"); !ok || !next.Equal(time.Date(2026, 1, 1, 10, 5, 0, 0, time.UTC)) {
		t.Fatalf("want the first occurrence at 10:05, got %s (%t)", next, ok)
	}
	if _, ok := scheduler.NextRun("manual"); ok {
		t.Fatal("an unscheduled workflow must not report a next run")
	}
	wait := nextWait(t, clock)
	if wait.duration != 2*time.Minute {
		t.Fatalf("want a two-minute wait, got %s", wait.duration)
	}
	select {
	case <-started:
		t.Fatal("run started before its occurrence")
	default:
	}

	clock.fireAt(wait, time.Date(2026, 1, 1, 10, 5, 0, 0, time.UTC))
	if d := awaitStart(t, started); d.ID != "every-five" || d.Schedule == nil || len(d.Steps) != 1 {
		t.Fatalf("scheduled start received a different definition: %+v", d)
	}
	wait = nextWait(t, clock)
	if wait.duration != 5*time.Minute {
		t.Fatalf("want a five-minute wait, got %s", wait.duration)
	}
	if next, _ := scheduler.NextRun("every-five"); !next.Equal(time.Date(2026, 1, 1, 10, 10, 0, 0, time.UTC)) {
		t.Fatalf("want the next occurrence at 10:10, got %s", next)
	}

	// The host slept: the 10:10 timer fires at 10:23. That run starts late, and the
	// missed 10:15 and 10:20 occurrences are skipped rather than replayed.
	clock.fireAt(wait, time.Date(2026, 1, 1, 10, 23, 0, 0, time.UTC))
	awaitStart(t, started)
	wait = nextWait(t, clock)
	if wait.duration != 2*time.Minute {
		t.Fatalf("want a two-minute wait until 10:25, got %s", wait.duration)
	}
	if next, _ := scheduler.NextRun("every-five"); !next.Equal(time.Date(2026, 1, 1, 10, 25, 0, 0, time.UTC)) {
		t.Fatalf("want the next occurrence at 10:25, got %s", next)
	}
	if len(started) != 0 {
		t.Fatal("missed occurrences were replayed")
	}
}

func TestBusyWorkflowSkipsTheOccurrenceWithoutRetry(t *testing.T) {
	release := make(chan struct{})
	runner, err := engine.New(1, 0, func(ctx context.Context, step workflow.Step) (workflow.CheckResult, error) {
		select {
		case <-release:
			return workflow.CheckResult{Healthy: true}, nil
		case <-ctx.Done():
			return workflow.CheckResult{}, ctx.Err()
		}
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()
	clock := newFakeClock(time.Date(2026, 1, 1, 10, 0, 30, 0, time.UTC))
	scheduler, err := New([]workflow.Definition{definition("minutely", every("* * * * *"))}, runner.Start, clock.Clock())
	if err != nil {
		t.Fatal(err)
	}
	scheduler.Start()
	defer scheduler.Close()

	wait := nextWait(t, clock)
	clock.fireAt(wait, time.Date(2026, 1, 1, 10, 1, 0, 0, time.UTC))
	wait = nextWait(t, clock)
	if runs := runner.List(); len(runs) != 1 || runs[0].Status != "running" {
		t.Fatalf("want one running run after the first occurrence, got %+v", runs)
	}

	// The second occurrence arrives while the first run still blocks in its executor.
	clock.fireAt(wait, time.Date(2026, 1, 1, 10, 2, 0, 0, time.UTC))
	wait = nextWait(t, clock)
	if runs := runner.List(); len(runs) != 1 {
		t.Fatalf("a busy workflow must skip its occurrence, got %d runs", len(runs))
	}
	if next, _ := scheduler.NextRun("minutely"); !next.Equal(time.Date(2026, 1, 1, 10, 3, 0, 0, time.UTC)) {
		t.Fatalf("the schedule must continue after a skipped occurrence, got next run %s", next)
	}

	close(release)
	deadline := time.After(2 * time.Second)
	for runner.List()[0].Status == "running" {
		select {
		case <-deadline:
			t.Fatal("the released run did not finish")
		case <-time.After(time.Millisecond):
		}
	}
	clock.fireAt(wait, time.Date(2026, 1, 1, 10, 3, 0, 0, time.UTC))
	nextWait(t, clock)
	if runs := runner.List(); len(runs) != 2 {
		t.Fatalf("want a new run once the workflow is free, got %d runs", len(runs))
	}
}

func TestCloseStopsWaitingWithoutStartingRuns(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 1, 1, 10, 3, 0, 0, time.UTC))
	started := make(chan workflow.Definition, 1)
	scheduler, err := New([]workflow.Definition{definition("every-five", every("*/5 * * * *"))}, func(d workflow.Definition) (engine.Run, error) {
		started <- d
		return engine.Run{}, nil
	}, clock.Clock())
	if err != nil {
		t.Fatal(err)
	}
	scheduler.Start()
	nextWait(t, clock)

	closed := make(chan struct{})
	go func() {
		scheduler.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not return while a wait was pending")
	}
	if len(started) != 0 {
		t.Fatal("a run started during shutdown")
	}
	scheduler.Close()
	scheduler.Start()
	select {
	case <-clock.waits:
		t.Fatal("Start after Close scheduled a new wait")
	default:
	}
}

func TestNewRejectsMissingDependenciesAndInvalidSchedules(t *testing.T) {
	clock := newFakeClock(time.Now())
	start := func(workflow.Definition) (engine.Run, error) { return engine.Run{}, nil }
	if _, err := New(nil, nil, clock.Clock()); err == nil {
		t.Fatal("accepted a nil start function")
	}
	if _, err := New(nil, start, Clock{}); err == nil {
		t.Fatal("accepted an empty clock")
	}
	_, err := New([]workflow.Definition{definition("broken", every("not a cron expression"))}, start, clock.Clock())
	if err == nil || !strings.Contains(err.Error(), "broken") {
		t.Fatalf("want an error naming the workflow, got %v", err)
	}
}
