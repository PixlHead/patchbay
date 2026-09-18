//go:build darwin || linux

package store

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestOpenCreatesPrivateDatabase(t *testing.T) {
	// Umask is process-wide, so change it only in a child running this test.
	if path := os.Getenv("PATCHBAY_PERMISSIONS_TEST_DATABASE"); path != "" {
		previous := syscall.Umask(0)
		defer syscall.Umask(previous)
		db, err := Open(context.Background(), path)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		return
	}

	directory := filepath.Join(t.TempDir(), "shared")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	// Make this fixture independent of the parent test process's umask.
	if err := os.Chmod(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "history.db")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestOpenCreatesPrivateDatabase$")
	cmd.Env = append(os.Environ(), "PATCHBAY_PERMISSIONS_TEST_DATABASE="+path)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("open with permissive umask: %v\n%s", err, output)
	}
	assertFilePermissions(t, path, 0o600)
	assertFilePermissions(t, directory, 0o755)
}

func TestOpenPreservesExistingDatabasePermissionsAndHistory(t *testing.T) {
	for _, test := range []struct {
		name    string
		mode    os.FileMode
		symlink bool
	}{
		{"group readable", 0o640, false},
		{"world readable", 0o644, false},
		{"symlink to existing database", 0o640, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			directory := t.TempDir()
			parent, err := os.Stat(directory)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(directory, "history.db")
			db, err := Open(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			definition, original := runFixture()
			original.Error = "Keep the saved run reason."
			if err := CreateRun(ctx, db, original, definition); err != nil {
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, test.mode); err != nil {
				t.Fatal(err)
			}
			before, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			openPath := path
			if test.symlink {
				openPath = filepath.Join(directory, "alias.db")
				if err := os.Symlink(path, openPath); err != nil {
					t.Fatal(err)
				}
			}
			reopened, err := Open(ctx, openPath)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			assertStoredRun(t, reopened, definition, original)
			if err := reopened.Close(); err != nil {
				t.Fatal(err)
			}
			assertFilePermissions(t, path, test.mode)
			assertFilePermissions(t, directory, parent.Mode().Perm())
			after, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if !os.SameFile(before, after) {
				t.Fatal("opening an existing database replaced its file")
			}
			if test.symlink {
				if target, err := os.Readlink(openPath); err != nil || target != path {
					t.Fatalf("database alias was replaced or changed: target=%q error=%v", target, err)
				}
			}
		})
	}
}

func TestOpenDoesNotCreateThroughDanglingDatabaseSymlink(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "missing.db")
	alias := filepath.Join(directory, "alias.db")
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}
	db, err := Open(context.Background(), alias)
	if db != nil {
		db.Close()
	}
	if err == nil || db != nil {
		t.Fatalf("dangling database symlink was not refused: db=%v error=%v", db, err)
	}
	if actual, err := os.Readlink(alias); err != nil || actual != target {
		t.Fatalf("dangling symlink was replaced or changed: target=%q error=%v", actual, err)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("opening a dangling symlink created its target: %v", err)
	}
}

func TestOpenWithCanceledContextDoesNotCreateDatabase(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	path := filepath.Join(t.TempDir(), "canceled.db")
	db, err := Open(ctx, path)
	if db != nil {
		db.Close()
	}
	if !errors.Is(err, context.Canceled) || db != nil {
		t.Fatalf("canceled open did not return cancellation: db=%v error=%v", db, err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("canceled open created a database: %v", err)
	}
}

func assertFilePermissions(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s: got permissions %04o, want %04o", path, got, want)
	}
}
