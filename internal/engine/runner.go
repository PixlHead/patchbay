// Package engine runs steps in order and owns in-memory execution state.
package engine

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"patchbay/internal/workflow"
)

var (
	ErrWorkflowBusy = errors.New("this workflow is already queued or running; wait for it to finish")
	ErrCapacity     = errors.New("workflow capacity reached; wait for a run to finish")
	ErrClosed       = errors.New("the runner is shutting down")
	ErrRecordRun    = errors.New("could not save run")
)

type StepRun struct {
	ID         string               `json:"id"`
	Name       string               `json:"name"`
	Status     string               `json:"status"`
	StartedAt  *time.Time           `json:"startedAt,omitempty"`
	FinishedAt *time.Time           `json:"finishedAt,omitempty"`
	Output     *workflow.HTTPResult `json:"output,omitempty"`
	Error      string               `json:"error,omitempty"`
}

type Run struct {
	ID           string     `json:"id"`
	WorkflowID   string     `json:"workflowId"`
	WorkflowName string     `json:"workflowName"`
	Status       string     `json:"status"`
	Error        string     `json:"error,omitempty"` // Run-level reason, separate from step errors.
	CreatedAt    time.Time  `json:"createdAt"`
	StartedAt    time.Time  `json:"startedAt,omitzero"` // Zero until started; omit it from queued JSON.
	FinishedAt   *time.Time `json:"finishedAt,omitempty"`
	Steps        []StepRun  `json:"steps"`
	// Memory-only warning after final-save retries fail; independent of execution status.
	FinalSaveFailed bool `json:"finalSaveFailed,omitempty"`
}

// ExecuteStep is a function dependency, letting engine tests use a controlled
// executor without a network server or a plugin framework. Implementations must
// support concurrent calls from different runs.
type ExecuteStep func(context.Context, workflow.Step) (workflow.HTTPResult, error)

// RecordNewRun saves the initial run and workflow snapshot before execution.
// Implementations must respect context cancellation and save the snapshot atomically.
type RecordNewRun func(context.Context, Run, workflow.Definition) error

// UpdateSavedRun saves progress for an existing run. Like RecordNewRun,
// implementations must respect cancellation and save the snapshot atomically.
// Updates for different runs can happen concurrently.
type UpdateSavedRun func(context.Context, Run) error

type Runner struct {
	mu            sync.Mutex
	runs          map[string]Run
	order         []string
	busyWorkflows map[string]struct{}
	activeRuns    int
	pending       []queuedRun
	maxQueuedRuns int
	maxActiveRuns int
	closed        bool
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup

	execute        ExecuteStep
	recordNewRun   RecordNewRun
	updateSavedRun UpdateSavedRun
}

// queuedRun retains the definition captured at admission; waiting work owns no goroutine.
type queuedRun struct {
	id         string
	definition workflow.Definition
}

// New requires a positive active limit and a nonnegative queue limit.
// A queue limit of zero rejects starts whenever all active slots are occupied.
// Nil persistence callbacks keep the runner in memory for executor tests.
func New(maxActiveRuns, maxQueuedRuns int, execute ExecuteStep, recordNewRun RecordNewRun, updateSavedRun UpdateSavedRun) (*Runner, error) {
	if maxActiveRuns < 1 {
		return nil, fmt.Errorf("max active runs must be at least 1")
	}
	if maxQueuedRuns < 0 {
		return nil, fmt.Errorf("max queued runs must be at least 0")
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Runner{
		runs: make(map[string]Run), ctx: ctx, cancel: cancel,
		busyWorkflows: make(map[string]struct{}), maxActiveRuns: maxActiveRuns, maxQueuedRuns: maxQueuedRuns,
		execute: execute, recordNewRun: recordNewRun, updateSavedRun: updateSavedRun,
	}, nil
}

func (r *Runner) Start(definition workflow.Definition) (Run, error) {
	if err := workflow.Validate(definition); err != nil {
		return Run{}, err
	}
	definition.Steps = slices.Clone(definition.Steps)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return Run{}, ErrClosed
	}
	if _, exists := r.busyWorkflows[definition.ID]; exists {
		return Run{}, ErrWorkflowBusy
	}
	queued := r.activeRuns >= r.maxActiveRuns
	if queued && len(r.pending) >= r.maxQueuedRuns {
		return Run{}, fmt.Errorf("%w (active limit %d, queue limit %d)", ErrCapacity, r.maxActiveRuns, r.maxQueuedRuns)
	}
	run := Run{
		ID: rand.Text(), WorkflowID: definition.ID, WorkflowName: definition.Name,
		Status: "queued", CreatedAt: time.Now().UTC(), Steps: make([]StepRun, len(definition.Steps)),
	}
	if !queued {
		run.Status, run.StartedAt = "running", run.CreatedAt
	}
	for i, step := range definition.Steps {
		run.Steps[i] = StepRun{ID: step.ID, Name: step.Name, Status: "pending"}
	}
	if r.recordNewRun != nil {
		// Keep admission locked during the save; bound how long other calls wait.
		// This context belongs to the runner, not the originating HTTP request.
		saveCtx, cancel := context.WithTimeout(r.ctx, 5*time.Second)
		err := r.recordNewRun(saveCtx, copyRun(run), definition)
		cancel()
		if err != nil {
			return Run{}, fmt.Errorf("%w: %w", ErrRecordRun, err)
		}
	}
	r.order = append(r.order, run.ID)
	r.runs[run.ID] = run
	r.busyWorkflows[definition.ID] = struct{}{}
	r.trimHistoryLocked()
	entry := queuedRun{id: run.ID, definition: definition}
	if queued {
		r.pending = append(r.pending, entry)
	} else {
		r.launchLocked(entry)
	}
	return copyRun(run), nil
}

func (r *Runner) Get(id string) (Run, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, ok := r.runs[id]
	return copyRun(run), ok
}

func (r *Runner) List() []Run {
	r.mu.Lock()
	defer r.mu.Unlock()
	runs := make([]Run, 0, len(r.order))
	for i := len(r.order) - 1; i >= 0; i-- {
		runs = append(runs, copyRun(r.runs[r.order[i]]))
	}
	return runs
}

func (r *Runner) Close() {
	r.mu.Lock()
	r.closed = true
	r.cancel()
	pending := r.pending
	r.pending = nil
	if len(pending) > 0 {
		// Other concurrent Close calls must also wait for queued-run cleanup.
		r.wg.Add(1)
	}
	r.mu.Unlock()
	if len(pending) > 0 {
		r.cancelQueued(pending)
	}
	r.wg.Wait()
}

// Caller holds mu. Reserve the slot before another submission can take it.
func (r *Runner) launchLocked(entry queuedRun) {
	r.activeRuns++
	r.wg.Add(1)
	go r.run(entry.id, entry.definition)
}

func (r *Runner) cancelQueued(pending []queuedRun) {
	defer r.wg.Done()
	// Bound the whole queue's cleanup, rather than adding five seconds per entry.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.ctx), 5*time.Second)
	defer cancel()
	for _, entry := range pending {
		run, _ := r.Get(entry.id)
		run = r.finishRun(ctx, run, "canceled")
		r.mu.Lock()
		r.runs[run.ID] = copyRun(run)
		delete(r.busyWorkflows, run.WorkflowID)
		r.trimHistoryLocked()
		r.mu.Unlock()
	}
}

func (r *Runner) run(id string, definition workflow.Definition) {
	defer r.wg.Done()
	// This goroutine owns its working copy; readers only see published snapshots.
	run, _ := r.Get(id)
	if run.Status == "queued" && r.ctx.Err() == nil {
		// The first step-start save persists this transition before any action runs.
		run.Status, run.StartedAt = "running", time.Now().UTC()
	}
	status := "succeeded"
	for i, step := range definition.Steps {
		if r.ctx.Err() != nil {
			status = "canceled"
			break
		}
		stepRun := &run.Steps[i]
		started := time.Now().UTC()
		stepRun.Status, stepRun.StartedAt = "running", &started
		if err := r.saveRunUpdate(r.ctx, run); err != nil || r.ctx.Err() != nil {
			status = "failed"
			if r.ctx.Err() != nil {
				status = "canceled"
			} else {
				run.Error = fmt.Sprintf("Could not save progress before step %q. Execution stopped. Check server logs.", step.Name)
			}
			// The executor never started; finalization will mark this step skipped.
			stepRun.Status, stepRun.StartedAt = "pending", nil
			break
		}
		r.publishRun(run)

		// Neither external work nor database writes hold the history mutex.
		output, err := r.execute(r.ctx, step)
		finished := time.Now().UTC()
		stepRun.FinishedAt = &finished
		if err != nil {
			status = "failed"
			if r.ctx.Err() != nil {
				status = "canceled"
			}
			stepRun.Status, stepRun.Error = status, err.Error()
		} else {
			stepRun.Status, stepRun.Output = "succeeded", &output
		}
		if r.ctx.Err() != nil {
			// Finalization saves these results with a fresh cleanup context.
			status = "canceled"
			break
		}
		if err := r.saveRunUpdate(r.ctx, run); err != nil {
			status = "failed"
			if r.ctx.Err() != nil {
				status = "canceled"
			} else {
				run.Error = fmt.Sprintf("Could not save the result of step %q. Execution stopped. Check server logs.", step.Name)
			}
			// Preserve completed outputs, but do not execute any more steps.
			break
		}
		r.publishRun(run)
		if err != nil {
			break
		}
	}
	run = r.finishRun(context.WithoutCancel(r.ctx), run, status)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.runs[id] = copyRun(run)
	delete(r.busyWorkflows, definition.ID)
	r.activeRuns--
	if !r.closed && len(r.pending) > 0 {
		next := r.pending[0]
		r.pending = slices.Delete(r.pending, 0, 1)
		r.launchLocked(next)
	}
	r.trimHistoryLocked()
}

func (r *Runner) finishRun(ctx context.Context, run Run, status string) Run {
	finished := time.Now().UTC()
	run.Status, run.FinishedAt = status, &finished
	for i := range run.Steps {
		if run.Steps[i].Status == "pending" {
			run.Steps[i].Status = "skipped"
		}
	}
	// Retry only this completed snapshot, never the actions that produced it.
	if err := r.saveFinalRun(ctx, run); err != nil {
		run.FinalSaveFailed = true
		slog.Error("could not save final run", "run_id", run.ID, "status", run.Status, "error", err)
	}
	return run
}

func (r *Runner) saveFinalRun(ctx context.Context, run Run) error {
	if r.updateSavedRun == nil {
		return nil
	}
	// All attempts and delays share one budget. A shorter parent deadline,
	// such as queued-run shutdown cleanup, still takes precedence.
	saveCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			timer := time.NewTimer(time.Duration(attempt) * 100 * time.Millisecond)
			select {
			case <-saveCtx.Done():
				timer.Stop()
				return errors.Join(err, saveCtx.Err())
			case <-timer.C:
			}
		}
		if saveCtx.Err() != nil {
			return errors.Join(err, saveCtx.Err())
		}
		err = r.updateSavedRun(saveCtx, copyRun(run))
		if err == nil || saveCtx.Err() != nil {
			return err
		}
	}
	return err
}

func (r *Runner) saveRunUpdate(ctx context.Context, run Run) error {
	if r.updateSavedRun == nil {
		return nil
	}
	saveCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	err := r.updateSavedRun(saveCtx, copyRun(run))
	if err != nil && ctx.Err() == nil {
		slog.Error("could not save run update", "run_id", run.ID, "status", run.Status, "error", err)
	}
	return err
}

func (r *Runner) publishRun(run Run) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.runs[run.ID] = copyRun(run)
}

// Caller holds mu. Never evict queued or running work from memory.
func (r *Runner) trimHistoryLocked() {
	for len(r.order) > 100 {
		i := slices.IndexFunc(r.order, func(id string) bool {
			status := r.runs[id].Status
			return status != "running" && status != "queued"
		})
		if i < 0 {
			return // More than 100 unfinished runs: trim again when one finishes.
		}
		delete(r.runs, r.order[i])
		r.order = slices.Delete(r.order, i, i+1)
	}
}

// Return snapshots so JSON encoding never races with the execution goroutine.
func copyRun(run Run) Run {
	run.Steps = slices.Clone(run.Steps)
	for i := range run.Steps {
		if run.Steps[i].Output != nil {
			output := *run.Steps[i].Output
			run.Steps[i].Output = &output
		}
	}
	return run
}
