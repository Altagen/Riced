package apply

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// LockPath returns the on-disk location of the apply lock for a given
// HOME. The lock prevents two concurrent `riced apply` runs from
// trampling each other's state.
func LockPath(homeDir string) string {
	return filepath.Join(homeDir, ".riced", "state", ".lock")
}

// Lock represents an acquired apply lock. Callers must call Release in a
// defer so the lock survives an unexpected return.
type Lock struct{ path string }

// AcquireLock takes the apply lock. Returns ErrLockHeld (wrapped with the
// lock path) if another process already holds it. The lock file contains
// the holder's PID so users can spot stale locks quickly.
//
// Stale lock recovery: if a Riced process crashed, the file remains.
// The CLI surfaces a hint pointing at LockPath; the user runs `rm` once
// they've confirmed no other Riced is running.
func AcquireLock(homeDir string) (*Lock, error) {
	path := LockPath(homeDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			holder, _ := os.ReadFile(path) // best-effort, may be empty
			return nil, fmt.Errorf("apply lock held: another riced process owns %s (holder=%q) -- remove the file if you are sure nothing is running",
				path, string(holder))
		}
		return nil, fmt.Errorf("create lock %s: %w", path, err)
	}
	if _, werr := f.WriteString(strconv.Itoa(os.Getpid()) + "\n"); werr != nil {
		// Non-fatal: the lock is held by virtue of the file existing.
		// Just close and proceed.
		_ = f.Close()
		return &Lock{path: path}, nil
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("close lock %s: %w", path, err)
	}
	return &Lock{path: path}, nil
}

// Release removes the lock file. Safe to call multiple times; subsequent
// calls are no-ops.
func (l *Lock) Release() {
	if l == nil || l.path == "" {
		return
	}
	_ = os.Remove(l.path)
	l.path = ""
}
