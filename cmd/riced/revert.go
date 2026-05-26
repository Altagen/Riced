package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/Altagen/Riced/internal/apply"
)

// runRevert implements `riced revert [--yes]`.
//
// It restores files from the most recent backup directory recorded in
// the state file. Riced-written files (which had no prior content) are
// removed. The state file itself is deleted on success.
func runRevert(args []string) int {
	fs := flag.NewFlagSet("revert", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "skip the confirmation prompt")
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}

	home, err := os.UserHomeDir()
	if err != nil {
		slog.Error("resolve HOME", "err", err)
		return 1
	}

	state, err := apply.LoadState(apply.StatePath(home))
	if err != nil {
		slog.Error("load state", "err", err)
		return 1
	}
	if state == nil {
		fmt.Println("No state to revert.")
		return 0
	}

	fmt.Printf("This will remove %d Riced-written file(s)", len(state.WrittenAt))
	if state.BackupRoot != "" {
		fmt.Printf(" and restore the originals from %s", state.BackupRoot)
	}
	fmt.Println(".")

	if !*yes {
		if !confirm("Proceed?") {
			slog.Warn("aborted by user")
			return 2
		}
	}

	// Acquire the same lock `riced apply` uses. Revert mutates the same
	// state/ directory, so concurrent apply + revert would race on the
	// state file and the live KDE config destinations.
	lock, err := apply.AcquireLock(home)
	if err != nil {
		slog.Error("acquire lock", "err", err)
		return 1
	}
	defer lock.Release()

	if err := apply.Revert(home); err != nil {
		slog.Error("revert", "err", err)
		return 1
	}
	slog.Info("reverted")
	return 0
}
