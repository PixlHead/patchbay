// Package schedule starts workflow runs at cron occurrences. It owns timing only:
// the runner still owns admission, overlap, capacity, and execution.
package schedule

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"patchbay/internal/engine"
	"patchbay/internal/workflow"
)

// StartRun admits one run. Production passes the runner's Start method, so a
// scheduled start follows the same capacity and overlap rules as a manual one.
type StartRun func(workflow.Definition) (engine.Run, error)

// Clock lets tests control time. After must deliver once the duration has
// passed, like time.After.
type Clock struct {
	Now   func() time.Time
	After func(time.Duration) <-chan time.Time
}

func SystemClock() Clock {
	return Clock{Now: time.Now, After: time.After}
}

type entry struct {
	definition workflow.Definition
	timetable  workflow.Timetable
}

type Scheduler struct {
	start   StartRun
	clock   Clock
	entries []entry

	mu      sync.Mutex
	next    map[string]time.Time
	started bool

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New keeps the definitions that carry a schedule and ignores the rest.
// The definitions must already have passed workflow.Validate.
func New(definitions []workflow.Definition, start StartRun, clock Clock) (*Scheduler, error) {
	if start == nil || clock.Now == nil || clock.After == nil {
		return nil, fmt.Errorf("scheduler needs a start function and a complete clock")
	}
	entries := make([]entry, 0)
	for _, definition := range definitions {
		if definition.Schedule == nil {
			continue
		}
		timetable, err := workflow.NewTimetable(*definition.Schedule)
		if err != nil {
			return nil, fmt.Errorf("workflow %q: %w", definition.ID, err)
		}
		entries = append(entries, entry{definition: definition, timetable: timetable})
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Scheduler{
		start: start, clock: clock, entries: entries,
		next: make(map[string]time.Time), ctx: ctx, cancel: cancel,
	}, nil
}

// Start computes every first occurrence, then waits for each one in its own
// goroutine. NextRun reports those occurrences as soon as Start returns.
// A second call, or a call after Close, does nothing.
func (s *Scheduler) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started || s.ctx.Err() != nil {
		return
	}
	s.started = true
	now := s.clock.Now()
	for _, e := range s.entries {
		occurrence := e.timetable.Next(now)
		s.next[e.definition.ID] = occurrence
		slog.Info("schedule active", "workflow_id", e.definition.ID, "cron", e.definition.Schedule.Cron,
			"timezone", e.definition.Schedule.Timezone, "next_run", occurrence)
		s.wg.Add(1)
		go s.loop(e, occurrence)
	}
}

// Close stops future starts and waits for the waiting goroutines to exit.
// Runs that already started belong to the runner and continue there.
func (s *Scheduler) Close() {
	s.cancel()
	s.wg.Wait()
}

// NextRun reports a scheduled workflow's next occurrence in its own time zone.
// It reports false for an unscheduled workflow or a schedule with no occurrence left.
func (s *Scheduler) NextRun(workflowID string) (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next, ok := s.next[workflowID]
	return next, ok && !next.IsZero()
}

func (s *Scheduler) loop(e entry, occurrence time.Time) {
	defer s.wg.Done()
	id := e.definition.ID
	for !occurrence.IsZero() {
		select {
		case <-s.ctx.Done():
			return
		case <-s.clock.After(occurrence.Sub(s.clock.Now())):
		}
		if s.ctx.Err() != nil {
			// Close raced with the timer; shutdown must not admit new work.
			return
		}
		run, err := s.start(e.definition)
		switch {
		case err == nil:
			slog.Info("scheduled run started", "workflow_id", id, "run_id", run.ID, "occurrence", occurrence)
		case errors.Is(err, engine.ErrWorkflowBusy):
			slog.Info("scheduled run skipped while the previous run is active", "workflow_id", id, "occurrence", occurrence)
		default:
			slog.Warn("scheduled run skipped", "workflow_id", id, "occurrence", occurrence, "error", err)
		}
		// Occurrences that passed during a long wait or a slow start are skipped, not replayed.
		from := s.clock.Now()
		if from.Before(occurrence) {
			from = occurrence
		}
		occurrence = e.timetable.Next(from)
		s.mu.Lock()
		s.next[id] = occurrence
		s.mu.Unlock()
	}
	slog.Warn("schedule has no further occurrences", "workflow_id", id)
}
