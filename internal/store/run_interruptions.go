package store

import (
	"context"
	"database/sql"
	"fmt"
)

// MarkUnfinishedRunsInterrupted marks saved queued/running runs and their running steps
// interrupted, and pending steps skipped. All existing results and timestamps
// are preserved: a missing finish time stays unknown.
// Call only at startup before any runner uses this database; this changes saved
// history and does not stop executors or resume work.
// It returns the number of runs changed, or zero if the transaction fails.
func MarkUnfinishedRunsInterrupted(ctx context.Context, db *sql.DB) (int64, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin run interruption: %w", err)
	}
	defer tx.Rollback()

	// Update steps first while their parent runs still have status queued or running.
	// Terminal steps and steps belonging to other runs keep their saved state.
	_, err = tx.ExecContext(ctx, `UPDATE run_steps
        SET status = CASE status WHEN 'running' THEN 'interrupted' ELSE 'skipped' END
        WHERE status IN ('running', 'pending')
          AND run_id IN (SELECT id FROM runs WHERE status IN ('queued', 'running'))`)
	if err != nil {
		return 0, fmt.Errorf("mark unfinished run steps: %w", err)
	}
	result, err := tx.ExecContext(ctx, `UPDATE runs
        SET status = 'interrupted' WHERE status IN ('queued', 'running')`)
	if err != nil {
		return 0, fmt.Errorf("mark unfinished runs: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count interrupted runs: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit run interruption: %w", err)
	}
	return count, nil
}
