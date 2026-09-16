package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"patchbay/internal/engine"
)

func TestGetRunReadsPersistedHistory(t *testing.T) {
	for _, state := range []string{"finished", "running", "not started"} {
		t.Run(state, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			path := filepath.Join(t.TempDir(), "history.db")
			db, err := Open(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			definition, want := runFixture()
			if state != "finished" {
				want.Status, want.FinishedAt = "running", nil
				for i, step := range want.Steps {
					want.Steps[i] = engine.StepRun{ID: step.ID, Name: step.Name, Status: "pending"}
				}
				if state == "running" {
					want.Steps[0].Status = "running"
					want.Steps[0].StartedAt = &want.StartedAt
				}
			}
			if state == "not started" {
				want.Status, want.StartedAt = "queued", time.Time{}
			}
			if err := CreateRun(ctx, db, want, definition); err != nil {
				t.Fatal(err)
			}
			// Another run may reuse every step ID without mixing its results in.
			_, other := runFixture()
			other.ID = "run-2"
			other.Steps[0].Output.Reason = "second run output"
			if err := CreateRun(ctx, db, other, definition); err != nil {
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := Open(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			got, err := GetRun(ctx, reopened, want.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("saved run changed after reopening:\ngot  %#v\nwant %#v", got, want)
			}
		})
	}
}

func TestGetRunErrorsReturnNoPartialHistory(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	definition, run := runFixture()
	if err := CreateRun(ctx, db, run, definition); err != nil {
		t.Fatal(err)
	}

	got, err := GetRun(ctx, db, "missing")
	if !errors.Is(err, sql.ErrNoRows) || !reflect.DeepEqual(got, engine.Run{}) {
		t.Fatalf("expected an empty run and sql.ErrNoRows, got %#v, %v", got, err)
	}
	// Fail decoding the second step, after reading the run and first result.
	if _, err := db.ExecContext(ctx, `UPDATE run_steps SET output_json = '{'
        WHERE run_id = ? AND step_id = 'broken'`, run.ID); err != nil {
		t.Fatal(err)
	}
	got, err = GetRun(ctx, db, run.ID)
	var syntaxError *json.SyntaxError
	if !errors.As(err, &syntaxError) || !reflect.DeepEqual(got, engine.Run{}) {
		t.Fatalf("expected an empty run and a JSON error, got %#v, %v", got, err)
	}
	// The failed read must release its transaction so later operations can proceed.
	if _, err := db.ExecContext(ctx, `UPDATE run_steps SET output_json = NULL
        WHERE run_id = ? AND step_id = 'broken'`, run.ID); err != nil {
		t.Fatal(err)
	}
	got, err = GetRun(ctx, db, run.ID)
	if err != nil || !reflect.DeepEqual(got, run) {
		t.Fatalf("could not read the repaired record: %#v, %v", got, err)
	}
}
