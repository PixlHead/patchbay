//go:build darwin || linux

package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestDatabaseLockExcludesAliasesAndAllowsOtherDatabases(t *testing.T) {
	directory := t.TempDir()
	dbPath := filepath.Join(directory, "history.db")
	first, err := lockDatabase(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if second, err := lockDatabase(dbPath); !errors.Is(err, errDatabaseInUse) {
		if second != nil {
			second.Close()
		}
		t.Fatalf("new database was not reserved before SQLite creation: %v", err)
	}
	if err := os.WriteFile(dbPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	aliasDirectory := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(directory, aliasDirectory); err != nil {
		t.Fatal(err)
	}
	aliasFile := filepath.Join(t.TempDir(), "alias.db")
	if err := os.Symlink(dbPath, aliasFile); err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(cwd, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, alias := range []string{dbPath, relative, aliasFile, filepath.Join(aliasDirectory, "history.db")} {
		if second, err := lockDatabase(alias); !errors.Is(err, errDatabaseInUse) {
			if second != nil {
				second.Close()
			}
			t.Fatalf("database alias %q bypassed ownership: %v", alias, err)
		}
	}
	other, err := lockDatabase(filepath.Join(directory, "other.db"))
	if err != nil {
		t.Fatalf("independent database was blocked: %v", err)
	}
	defer other.Close()
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(first.Name()); err != nil {
		t.Fatalf("lock file must remain for reuse: %v", err)
	}
	reopened, err := lockDatabase(dbPath)
	if err != nil {
		t.Fatalf("closed lock could not be acquired again: %v", err)
	}
	defer reopened.Close()
}

func TestDatabaseLockRejectsDanglingSymlink(t *testing.T) {
	directory := t.TempDir()
	alias := filepath.Join(directory, "alias.db")
	if err := os.Symlink(filepath.Join(directory, "missing.db"), alias); err != nil {
		t.Fatal(err)
	}
	lock, err := lockDatabase(alias)
	if lock != nil {
		lock.Close()
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected unresolved database symlink to fail, got %v", err)
	}
	if _, err := os.Stat(alias + ".lock"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dangling alias created a separate lock file: %v", err)
	}
}

func TestDatabaseLockReleasedAfterProcessExit(t *testing.T) {
	// Run just this test in a child process to check real process ownership.
	if dbPath := os.Getenv("PATCHBAY_LOCK_TEST_DATABASE"); dbPath != "" {
		lock, err := lockDatabase(dbPath)
		if err != nil {
			t.Fatal(err)
		}
		defer lock.Close()
		fmt.Fprintln(os.Stdout, "locked")
		_, _ = io.Copy(io.Discard, os.Stdin) // Parent kills us while holding the lock.
		return
	}
	dbPath := filepath.Join(t.TempDir(), "history.db")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestDatabaseLockReleasedAfterProcessExit$")
	cmd.Env = append(os.Environ(), "PATCHBAY_LOCK_TEST_DATABASE="+dbPath)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	if line, err := bufio.NewReader(stdout).ReadString('\n'); err != nil || line != "locked\n" {
		t.Fatalf("child did not acquire lock: %q, %v", line, err)
	}
	if lock, err := lockDatabase(dbPath); !errors.Is(err, errDatabaseInUse) {
		if lock != nil {
			lock.Close()
		}
		t.Fatalf("another process bypassed ownership: %v", err)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatalf("expected killed child to fail; stderr: %s", stderr.String())
	}
	lock, err := lockDatabase(dbPath)
	if err != nil {
		t.Fatalf("process exit left a stale lock: %v", err)
	}
	defer lock.Close()
}
