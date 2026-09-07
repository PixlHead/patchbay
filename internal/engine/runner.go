// Package engine runs steps in order and owns in-memory execution state.
package engine

import (
	"context"
	"crypto/rand"
	"errors"
	"slices"
	"sync"
	"time"

	"patchbay/internal/workflow"
)

var (
	ErrBusy   = errors.New("another workflow is running; wait for it to finish")
	ErrClosed = errors.New("the runner is shutting down")
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

type Runner struct {
	mu      sync.Mutex
	runs    map[string]Run
	order   []string
	active  bool
	closed  bool
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	execute ExecuteStep
}

func New(execute ExecuteStep) *Runner {
	ctx, cancel := context.WithCancel(context.Background())
	return &Runner{runs: make(map[string]Run), ctx: ctx, cancel: cancel, execute: execute}
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
	status := "succeeded"
	for i, step := range definition.Steps {
		if r.ctx.Err() != nil {
			status = "canceled"
			break
		}
		started := time.Now().UTC()
		r.mu.Lock()
		r.runs[id].Steps[i].Status = "running"
		r.runs[id].Steps[i].StartedAt = &started
		r.mu.Unlock()

		// Network work happens outside the lock; API reads can proceed.
		output, err := r.execute(r.ctx, step)
		finished := time.Now().UTC()
		r.mu.Lock()
		stepRun := &r.runs[id].Steps[i]
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
		r.mu.Unlock()
		if err != nil {
			break
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	run := r.runs[id]
	finished := time.Now().UTC()
	run.Status, run.FinishedAt = status, &finished
	for i := range run.Steps {
		if run.Steps[i].Status == "pending" {
			run.Steps[i].Status = "skipped"
		}
	}
	r.runs[id] = run
	r.active = false
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
