package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"patchbay/internal/engine"
	"patchbay/internal/workflow"
)

// CreateRun inserts a run, its workflow snapshot, and all step results atomically.
// The caller supplies a validated definition and the corresponding run snapshot.
// An existing run ID is an error; this function never replaces saved history.
func CreateRun(ctx context.Context, db *sql.DB, run engine.Run, definition workflow.Definition) error {
	definitionJSON, err := json.Marshal(definition)
	if err != nil {
		return fmt.Errorf("encode workflow snapshot: %w", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin run insert: %w", err)
	}
	defer tx.Rollback() // Any failed insert rolls back the entire run.

	// Runs currently start immediately, so creation and start share a timestamp.
	// Queued runs will need a separate creation time when admission is added.
	started := run.StartedAt.UnixMilli()
	_, err = tx.ExecContext(ctx, `INSERT INTO runs
        (id, workflow_id, workflow_name, definition_json, status, created_at, started_at, finished_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		run.ID, run.WorkflowID, run.WorkflowName, string(definitionJSON), run.Status,
		started, started, unixMilliOrNull(run.FinishedAt))
	if err != nil {
		return fmt.Errorf("insert run %q: %w", run.ID, err)
	}

	for position, step := range run.Steps {
		// A nil output is SQL NULL, distinct from an encoded result object.
		var outputJSON any
		if step.Output != nil {
			encoded, err := json.Marshal(step.Output)
			if err != nil {
				return fmt.Errorf("encode step %q output: %w", step.ID, err)
			}
			outputJSON = string(encoded)
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO run_steps
            (run_id, step_id, position, name, status, started_at, finished_at, output_json, error)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			run.ID, step.ID, position, step.Name, step.Status,
			unixMilliOrNull(step.StartedAt), unixMilliOrNull(step.FinishedAt), outputJSON, step.Error)
		if err != nil {
			return fmt.Errorf("insert run %q step %q: %w", run.ID, step.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit run %q: %w", run.ID, err)
	}
	return nil
}

// UpdateRun saves execution state for an existing run and its steps atomically.
// Workflow details, creation time, step names, and step order remain unchanged.
// The caller supplies every step in its original order and serializes updates
// for each run; this function does not enforce execution status transitions.
func UpdateRun(ctx context.Context, db *sql.DB, run engine.Run) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin run update: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `UPDATE runs
        SET status = ?, started_at = ?, finished_at = ? WHERE id = ?`,
		run.Status, run.StartedAt.UnixMilli(), unixMilliOrNull(run.FinishedAt), run.ID)
	if err != nil {
		return fmt.Errorf("update run %q: %w", run.ID, err)
	}
	if affected, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("check run %q update: %w", run.ID, err)
	} else if affected != 1 {
		return fmt.Errorf("update run %q: %w", run.ID, sql.ErrNoRows)
	}

	// A full snapshot must describe exactly the steps saved when the run began.
	var stepCount int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM run_steps WHERE run_id = ?", run.ID).Scan(&stepCount); err != nil {
		return fmt.Errorf("count run %q steps: %w", run.ID, err)
	}
	if stepCount != len(run.Steps) {
		return fmt.Errorf("update run %q: expected %d steps, got %d", run.ID, stepCount, len(run.Steps))
	}

	for position, step := range run.Steps {
		var outputJSON any
		if step.Output != nil {
			encoded, err := json.Marshal(step.Output)
			if err != nil {
				return fmt.Errorf("encode step %q output: %w", step.ID, err)
			}
			outputJSON = string(encoded)
		}
		// Matching the original position also rejects reordered or duplicate IDs.
		result, err := tx.ExecContext(ctx, `UPDATE run_steps
            SET status = ?, started_at = ?, finished_at = ?, output_json = ?, error = ?
            WHERE run_id = ? AND step_id = ? AND position = ?`,
			step.Status, unixMilliOrNull(step.StartedAt), unixMilliOrNull(step.FinishedAt), outputJSON, step.Error,
			run.ID, step.ID, position)
		if err != nil {
			return fmt.Errorf("update run %q step %q: %w", run.ID, step.ID, err)
		}
		if affected, err := result.RowsAffected(); err != nil {
			return fmt.Errorf("check run %q step %q update: %w", run.ID, step.ID, err)
		} else if affected != 1 {
			return fmt.Errorf("update run %q step %q at position %d: %w", run.ID, step.ID, position, sql.ErrNoRows)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit run %q update: %w", run.ID, err)
	}
	return nil
}

func unixMilliOrNull(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UnixMilli()
}
