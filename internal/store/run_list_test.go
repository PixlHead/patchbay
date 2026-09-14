package store

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"patchbay/internal/engine"
)

func TestListRunsOrdersAndLimitsCompleteHistory(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	saved := make(map[string]engine.Run)
	for _, entry := range []struct {
		id      string
		created int64
	}{
		{"run-z", 2000},
		{"run-b", 3000},
		{"run-a", 3000}, // Same creation time, but inserted after run-b.
		{"run-zz", 1000},
	} {
		definition, run := runFixture()
		run.ID = entry.id
		run.Steps[0].Output.Reason = entry.id
		if err := CreateRun(ctx, db, run, definition); err != nil {
			t.Fatal(err)
		}
		// Creation order must win over ID, insertion order, and start time.
		if _, err := db.ExecContext(ctx, "UPDATE runs SET created_at = ? WHERE id = ?", entry.created, run.ID); err != nil {
			t.Fatal(err)
		}
		saved[run.ID] = run
	}
	want := []engine.Run{saved["run-b"], saved["run-a"], saved["run-z"], saved["run-zz"]}
	for _, limit := range []int{1, 2, 100} {
		got, err := ListRuns(ctx, db, limit)
		if err != nil {
			t.Fatal(err)
		}
		expected := want[:min(limit, len(want))]
		if !reflect.DeepEqual(got, expected) {
			t.Fatalf("limit %d: history order or step results changed:\ngot  %#v\nwant %#v", limit, got, expected)
		}
	}
}

func TestListRunsEmptyHistoryAndInvalidLimits(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	got, err := ListRuns(ctx, db, 100)
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("expected a non-nil empty list, got %#v, %v", got, err)
	}
	for _, limit := range []int{-1, 0, 101} {
		got, err := ListRuns(ctx, db, limit)
		if err == nil || got != nil {
			t.Fatalf("limit %d: expected nil results and an error, got %#v, %v", limit, got, err)
		}
	}
}

func TestListRunsErrorsReturnNoPartialHistory(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	definition, bad := runFixture()
	bad.ID = "run-a"
	_, good := runFixture()
	good.ID = "run-b"
	for _, run := range []engine.Run{bad, good} {
		if err := CreateRun(ctx, db, run, definition); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `UPDATE run_steps SET output_json = '{'
        WHERE run_id = ? AND step_id = 'broken'`, bad.ID); err != nil {
		t.Fatal(err)
	}
	// A corrupt run outside the requested limit must not affect the result.
	got, err := ListRuns(ctx, db, 1)
	if err != nil || !reflect.DeepEqual(got, []engine.Run{good}) {
		t.Fatalf("could not read the limited history: %#v, %v", got, err)
	}
	got, err = ListRuns(ctx, db, 2)
	var syntaxError *json.SyntaxError
	if !errors.As(err, &syntaxError) || got != nil {
		t.Fatalf("expected nil results and a JSON error, got %#v, %v", got, err)
	}
	// The failed list must release its transaction so subsequent reads can work.
	if _, err := db.ExecContext(ctx, `UPDATE run_steps SET output_json = NULL
        WHERE run_id = ? AND step_id = 'broken'`, bad.ID); err != nil {
		t.Fatal(err)
	}
	got, err = ListRuns(ctx, db, 2)
	if err != nil || !reflect.DeepEqual(got, []engine.Run{good, bad}) {
		t.Fatalf("could not read the repaired history: %#v, %v", got, err)
	}
}
