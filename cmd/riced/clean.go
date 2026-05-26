package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/Altagen/Riced/internal/apply"
)

// runCleanBackups implements `riced clean-backups [--keep N] [--older-than DUR]`.
//
// Default: --keep 5. The backup referenced by the current state file is
// never removed, even if it would otherwise be selected.
func runCleanBackups(args []string) int {
	fs := flag.NewFlagSet("clean-backups", flag.ContinueOnError)
	keep := fs.Int("keep", 5, "keep the N most recent backups (0 = no count limit)")
	olderThan := fs.Duration("older-than", 0, "also remove backups older than this duration (e.g. 720h for 30 days)")
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, "Usage: riced clean-backups [--keep N] [--older-than DUR]\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	home, err := os.UserHomeDir()
	if err != nil {
		slog.Error("resolve HOME", "err", err)
		return 1
	}

	// Lock against concurrent apply/revert. Without this, GC could delete
	// the BackupRoot of an in-progress apply (which isn't yet referenced
	// in state.toml until apply completes).
	lock, err := apply.AcquireLock(home)
	if err != nil {
		slog.Error("acquire lock", "err", err)
		return 1
	}
	defer lock.Release()

	state, err := apply.LoadState(apply.StatePath(home))
	if err != nil {
		slog.Error("load state", "err", err)
		return 1
	}
	current := ""
	if state != nil {
		current = state.BackupRoot
	}

	removed, err := apply.CleanBackups(home, apply.GCOptions{
		Keep:              *keep,
		OlderThan:         *olderThan,
		Now:               func() time.Time { return time.Now().UTC() },
		CurrentBackupRoot: current,
	})
	if err != nil {
		slog.Error("clean backups", "err", err)
		return 1
	}
	if len(removed) == 0 {
		fmt.Println("Nothing to clean.")
		return 0
	}
	fmt.Printf("Removed %d backup dir(s):\n", len(removed))
	for _, p := range removed {
		fmt.Printf("  %s\n", p)
	}
	return 0
}
