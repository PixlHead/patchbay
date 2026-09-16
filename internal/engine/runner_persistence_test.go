package engine

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"patchbay/internal/workflow"
)

func TestStartRecordsSnapshotBeforeExecution(t *testing.T) {
	var saved Run
	var savedDefinition workflow.Definition
	records := 0
	runner, err := New(2, 0, func(context.Context, workflow.Step) (workflow.HTTPResult, error) {
		if saved.ID == "" {
			return workflow.HTTPResult{}, errors.New("step started before the run was saved")
		}
		return workflow.HTTPResult{Healthy: true}, nil
	}, func(ctx context.Context, run Run, definition workflow.Definition) error {
		records++
		saved, savedDefinition = run, definition
		return nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()
	definition := example()
	started, err := runner.Start(definition)
	if err != nil {
		t.Fatal(err)
	}
	definition.Name = "Edited afterward"
	definition.Steps[0].Name = "Edited step"
	finished := awaitRun(t, runner, started.ID)
	if finished.Status != "succeeded" || records != 1 {
		t.Fatalf("expected one save before successful execution: %s, %d saves", finished.Status, records)
	}
	// Completion must not mutate the initial snapshot given to the recorder.
	if !reflect.DeepEqual(saved, started) || !reflect.DeepEqual(savedDefinition, example()) {
		t.Fatalf("initial snapshot changed: %+v, %+v", saved, savedDefinition)
	}
}

func TestStartRejectsFailedSaveAndAllowsRetry(t *testing.T) {
	storageError := errors.New("storage unavailable")
	attempts, executed := 0, 0
	runner, err := New(2, 0, func(context.Context, workflow.Step) (workflow.HTTPResult, error) {
		executed++
		return workflow.HTTPResult{Healthy: true}, nil
	}, func(context.Context, Run, workflow.Definition) error {
		attempts++
		if attempts == 1 {
			return storageError
		}
		return nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()
	rejected, err := runner.Start(example())
	if !errors.Is(err, ErrRecordRun) || !errors.Is(err, storageError) || !reflect.DeepEqual(rejected, Run{}) {
		t.Fatalf("expected an empty run and the save error, got %+v, %v", rejected, err)
	}
	if len(runner.List()) != 0 {
		t.Fatal("failed save appeared in history")
	}
	started, err := runner.Start(example())
	if err != nil {
		t.Fatalf("failed save left the runner busy: %v", err)
	}
	finished := awaitRun(t, runner, started.ID)
	runner.Close()
	if finished.Status != "succeeded" || attempts != 2 || executed != len(example().Steps) || len(runner.List()) != 1 {
		t.Fatalf("expected only the retry to execute: %s, %d saves, %d steps", finished.Status, attempts, executed)
	}
}
