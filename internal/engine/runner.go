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
	ErrBusy      = errors.New("another workflow is running; wait for it to finish")
	ErrClosed    = errors.New("the runner is shutting down")
	ErrRecordRun = errors.New("could not save run")
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
	StartedAt    time.Time  `json:"startedAt"`
	FinishedAt   *time.Time `json:"finishedAt,omitempty"`
	Steps        []StepRun  `json:"steps"`
}

// ExecuteStep is a function dependency, letting engine tests use a controlled
// executor without a network server or a plugin framework.
type ExecuteStep func(context.Context, workflow.Step) (workflow.HTTPResult, error)

// RecordNewRun saves the initial run and workflow snapshot before execution.
// Implementations must respect context cancellation and save the snapshot atomically.
type RecordNewRun func(context.Context, Run, workflow.Definition) error

// UpdateSavedRun saves progress for an existing run. Like RecordNewRun,
// implementations must respect cancellation and save the snapshot atomically.
type UpdateSavedRun func(context.Context, Run) error

type Runner struct {
	mu     sync.Mutex
	runs   map[string]Run
	order  []string
	active bool
	closed bool
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	execute        ExecuteStep
	recordNewRun   RecordNewRun
	updateSavedRun UpdateSavedRun
}

// Nil persistence callbacks keep the runner in memory for executor tests.
func New(execute ExecuteStep, recordNewRun RecordNewRun, updateSavedRun UpdateSavedRun) *Runner {
	ctx, cancel := context.WithCancel(context.Background())
	return &Runner{
		runs: make(map[string]Run), ctx: ctx, cancel: cancel,
		execute: execute, recordNewRun: recordNewRun, updateSavedRun: updateSavedRun,
	}
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
	if r.active {
		return Run{}, ErrBusy
	}
	run := Run{
		ID: rand.Text(), WorkflowID: definition.ID, WorkflowName: definition.Name,
		Status: "running", StartedAt: time.Now().UTC(), Steps: make([]StepRun, len(definition.Steps)),
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
	if len(r.order) == 100 {
		delete(r.runs, r.order[0])
		r.order = r.order[1:]
	}
	r.order = append(r.order, run.ID)
	r.runs[run.ID] = run
	r.active = true
	r.wg.Add(1)
	go r.run(run.ID, definition)
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
	r.mu.Unlock()
	r.wg.Wait()
}

func (r *Runner) run(id string, definition workflow.Definition) {
	defer r.wg.Done()
	// This goroutine owns its working copy; readers only see published snapshots.
	run, _ := r.Get(id)
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
			}
			// Preserve completed outputs, but do not execute any more steps.
			break
		}
		r.publishRun(run)
		if err != nil {
			break
		}
	}
	finished := time.Now().UTC()
	run.Status, run.FinishedAt = status, &finished
	for i := range run.Steps {
		if run.Steps[i].Status == "pending" {
			run.Steps[i].Status = "skipped"
		}
	}
	// One final attempt also runs after a progress-save failure or shutdown.
	// The helper logs failures; memory retains the actual execution results.
	_ = r.saveRunUpdate(context.WithoutCancel(r.ctx), run)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.runs[id] = copyRun(run)
	r.active = false
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
