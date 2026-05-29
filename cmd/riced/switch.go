package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/Altagen/Riced/internal/apply"
)

// runSwitch implements `riced switch [--yes] [--dry-run]`.
//
// Toggles the currently-applied family theme between its dark and light
// variants. Reads ~/.riced/state/current.toml, flips Mode, and delegates
// straight to runApply with the family slug + flipped --mode. Refuses
// when the current theme is not a family (no point toggling a flat slug).
func runSwitch(args []string) int {
	fs := flag.NewFlagSet("switch", flag.ContinueOnError)
	dryRun := fs.Bool("dry-run", false, "print the plan for the flipped variant and exit")
	yes := fs.Bool("yes", false, "skip the interactive confirmation prompt")
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, "Usage: riced switch [--dry-run] [--yes]\n")
		fmt.Fprint(os.Stderr, "Toggles the currently-applied family theme between dark and light.\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}

	home, err := os.UserHomeDir()
	if err != nil {
		slog.Error("resolve HOME", "err", err)
		return exitErr
	}

	state, err := apply.LoadState(apply.StatePath(home))
	if err != nil {
		slog.Error("load state", "err", err)
		return exitErr
	}
	if state == nil {
		slog.Error("no theme is currently applied -- nothing to switch")
		return exitErr
	}
	if state.FamilySlug == "" || state.Mode == "" {
		slog.Error("current theme is not a family variant -- apply a [meta.modes] theme first",
			"current_slug", state.Slug)
		return exitErr
	}

	flipped := ""
	switch state.Mode {
	case "dark":
		flipped = "light"
	case "light":
		flipped = "dark"
	default:
		slog.Error("unknown current mode in state.toml", "mode", state.Mode)
		return exitErr
	}

	slog.Info("switching family variant",
		"family", state.FamilySlug, "from", state.Mode, "to", flipped)

	// Delegate to runApply -- it owns the registry resolution, plan
	// building, prereq checks, lock, etc. We just reassemble its argv.
	delegated := []string{"--mode=" + flipped}
	if *dryRun {
		delegated = append(delegated, "--dry-run")
	}
	if *yes {
		delegated = append(delegated, "--yes")
	}
	delegated = append(delegated, state.FamilySlug)
	return runApply(delegated)
}
