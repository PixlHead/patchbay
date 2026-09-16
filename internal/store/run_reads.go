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
	return readRunInTx(ctx, tx, id)
}

// ListRuns returns complete runs, newest first by creation time, then ID descending.
// The limit must be between 1 and 100. An empty history returns an empty slice;
// any failure returns nil rather than a partial list.
func ListRuns(ctx context.Context, db *sql.DB, limit int) ([]engine.Run, error) {
	if limit < 1 || limit > 100 {
		return nil, fmt.Errorf("run history limit must be between 1 and 100 (got %d)", limit)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin run list read: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `SELECT id FROM runs
        ORDER BY created_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list run IDs: %w", err)
	}
	defer rows.Close()
	ids := make([]string, 0, limit)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("read listed run ID: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list run IDs: %w", err)
	}
	// Finish this result set before reading runs on the same connection.
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close run ID list: %w", err)
	}

	runs := make([]engine.Run, 0, len(ids))
	for _, id := range ids {
		run, err := readRunInTx(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, nil
}

// readRunInTx reads a run using the caller's existing database transaction.
func readRunInTx(ctx context.Context, tx *sql.Tx, id string) (engine.Run, error) {
	var run engine.Run
	var created int64
	var started, finished sql.NullInt64
	err := tx.QueryRowContext(ctx, `SELECT id, workflow_id, workflow_name, status,
        created_at, started_at, finished_at FROM runs WHERE id = ?`, id).
		Scan(&run.ID, &run.WorkflowID, &run.WorkflowName, &run.Status, &created, &started, &finished)
	if err != nil {
		return engine.Run{}, fmt.Errorf("read run %q: %w", id, err)
	}
	run.CreatedAt = time.UnixMilli(created).UTC()
	// A missing start uses zero time, omitted from JSON by the Run type.
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
