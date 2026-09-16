//go:build darwin || linux

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

var errDatabaseInUse = errors.New("database is already in use by another Patchbay instance")

// lockDatabase reserves a database for one server on Linux/macOS.
// Its parent directory must exist. Close the returned file only after the
// runner and SQLite have stopped. The OS also releases the lock on process exit.
func lockDatabase(dbPath string) (*os.File, error) {
	if dbPath == "" {
		return nil, fmt.Errorf("database path must not be empty")
	}
	absolute, err := filepath.Abs(dbPath)
	if err != nil {
		return nil, fmt.Errorf("resolve database path: %w", err)
	}

	// Existing databases may be symlinks. For a new database, resolve its parent
	// directory instead. A dangling database symlink must fail, not get a second lock.
	info, err := os.Lstat(absolute)
	switch {
	case err == nil:
		if info.IsDir() {
			return nil, fmt.Errorf("database path is a directory")
		}
		absolute, err = filepath.EvalSymlinks(absolute)
	case errors.Is(err, os.ErrNotExist):
		var directory string
		directory, err = filepath.EvalSymlinks(filepath.Dir(absolute))
		absolute = filepath.Join(directory, filepath.Base(absolute))
	}
	if err != nil {
		return nil, fmt.Errorf("resolve database path: %w", err)
	}

	// Keep this file on disk after closing it. Unlinking it could let another
	// process create a different file and acquire a second, independent lock.
	lock, err := os.OpenFile(absolute+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open database lock file: %w", err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errDatabaseInUse
		}
		return nil, fmt.Errorf("acquire database lock: %w", err)
	}
	return lock, nil
}
