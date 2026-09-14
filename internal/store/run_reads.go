package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"patchbay/internal/engine"
)

// GetRun reads a run and its steps from one consistent database snapshot.
// A missing run returns an error wrapping sql.ErrNoRows. Any failure returns
// a zero Run rather than partially decoded history.
func GetRun(ctx context.Context, db *sql.DB, id string) (engine.Run, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return engine.Run{}, fmt.Errorf("begin run read: %w", err)
	}
	// There are no writes to commit; Rollback ends the read transaction.
	defer tx.Rollback()

	var run engine.Run
	var started, finished sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT id, workflow_id, workflow_name, status,
        started_at, finished_at FROM runs WHERE id = ?`, id).
		Scan(&run.ID, &run.WorkflowID, &run.WorkflowName, &run.Status, &started, &finished)
	if err != nil {
		return engine.Run{}, fmt.Errorf("read run %q: %w", id, err)
	}
	// The current Run type represents a missing start with Go's zero time.
	if started.Valid {
		run.StartedAt = time.UnixMilli(started.Int64).UTC()
	}
	run.FinishedAt = timeFromUnixMilli(finished)

	rows, err := tx.QueryContext(ctx, `SELECT step_id, name, status, started_at,
        finished_at, output_json, error FROM run_steps WHERE run_id = ? ORDER BY position`, id)
	if err != nil {
		return engine.Run{}, fmt.Errorf("read run %q steps: %w", id, err)
	}
	defer rows.Close()
	run.Steps = make([]engine.StepRun, 0)
	for rows.Next() {
		var step engine.StepRun
		var start, finish sql.NullInt64
		var output sql.NullString
		if err := rows.Scan(&step.ID, &step.Name, &step.Status, &start, &finish, &output, &step.Error); err != nil {
			return engine.Run{}, fmt.Errorf("read run %q step: %w", id, err)
		}
		step.StartedAt = timeFromUnixMilli(start)
		step.FinishedAt = timeFromUnixMilli(finish)
		if output.Valid {
			if err := json.Unmarshal([]byte(output.String), &step.Output); err != nil {
				return engine.Run{}, fmt.Errorf("decode run %q step %q output: %w", id, step.ID, err)
			}
		}
		run.Steps = append(run.Steps, step)
	}
	if err := rows.Err(); err != nil {
		return engine.Run{}, fmt.Errorf("read run %q steps: %w", id, err)
	}
	return run, nil
}

func timeFromUnixMilli(value sql.NullInt64) *time.Time {
	if !value.Valid {
		return nil
	}
	instant := time.UnixMilli(value.Int64).UTC()
	return &instant
}
