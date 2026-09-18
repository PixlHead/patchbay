// Package store provides SQLite access for persistent application data.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"path/filepath"

	_ "modernc.org/sqlite" // Register the pure-Go "sqlite" database driver.
)

// Open opens or creates a SQLite file, verifies its identity, and applies migrations.
// Its parent directory must already exist.
// The caller owns the returned database and must close it during shutdown.
func Open(ctx context.Context, path string) (*sql.DB, error) {
	if path == "" {
		return nil, fmt.Errorf("database path must not be empty")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve database path: %w", err)
	}

	// Encode the filename so characters such as ? and # remain part of the path.
	dsn := url.URL{Scheme: "file", Path: filepath.ToSlash(absolute)}
	options := url.Values{}
	// The driver applies these settings whenever it opens a new connection.
	options.Add("_pragma", "foreign_keys(1)")
	options.Add("_pragma", "busy_timeout(5000)")
	dsn.RawQuery = options.Encode()

	db, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	// Start with one reusable connection to keep database access serialized.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	// sql.Open is lazy; Ping verifies that the file can actually be opened.
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect to database: %w", err)
	}
	if err := migrate(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}
