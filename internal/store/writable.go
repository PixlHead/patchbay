package store

import (
	"context"
	"database/sql"
	"fmt"
)

// CheckWritable commits a small write without changing schema or run history.
// Call after Open succeeds, while holding database ownership and before starting
// the runner. The caller supplies a context deadline for the entire check.
func CheckWritable(ctx context.Context, db *sql.DB) error {
	// SQLite dirties the header page even when user_version keeps its value.
	// This exercises the journal and commit path even when history is empty.
	// A single autocommit statement keeps the commit inside ExecContext's
	// cancellation scope; this driver's sql.Tx.Commit uses a background context.
	if _, err := db.ExecContext(ctx, fmt.Sprintf("PRAGMA main.user_version = %d", schemaVersion)); err != nil {
		return fmt.Errorf("commit database write check: %w", err)
	}
	return nil
}
