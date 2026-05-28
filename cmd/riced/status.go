package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/Altagen/Riced/internal/apply"
)

// runStatus implements `riced status`. Reads ~/.riced/state/current.toml
// and reports what's currently applied.
func runStatus(_ []string) int {
	home, err := os.UserHomeDir()
	if err != nil {
		slog.Error("resolve HOME", "err", err)
		return exitErr
	}
	statePath := apply.StatePath(home)
	state, err := apply.LoadState(statePath)
	if err != nil {
		slog.Error("load state", "err", err)
		return exitErr
	}
	if state == nil {
		fmt.Println("No theme is currently applied by Riced.")
		return exitOK
	}
	fmt.Printf("Applied:    %s\n", state.Slug)
	if state.Repo != "" {
		fmt.Printf("Repository: %s\n", state.Repo)
	}
	fmt.Printf("At:         %s\n", state.AppliedAt.Format("2006-01-02 15:04:05 MST"))
	if state.BackupRoot != "" {
		fmt.Printf("Backup:     %s\n", state.BackupRoot)
	}
	fmt.Printf("Files:      %d\n", len(state.WrittenAt))
	for _, p := range state.WrittenAt {
		fmt.Printf("  %s\n", p)
	}
	return exitOK
}
