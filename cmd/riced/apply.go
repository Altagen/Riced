package main

import (
	"bufio"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Altagen/Riced/internal/apply"
	"github.com/Altagen/Riced/internal/manifest"
	"github.com/Altagen/Riced/internal/registry"
)

// runApply implements `riced apply <slug> [--dry-run] [--yes]`.
//
// Pipeline:
//
//  1. Resolve the slug through the registry (qualified or bare).
//  2. Generate into ~/.riced/cache/<slug>/ (or --out if provided).
//  3. Compute an apply Plan against the live KDE locations under HOME.
//  4. Print the plan.
//  5. Unless --yes, prompt the user for explicit consent. Anything other
//     than y/Y/yes aborts without writing anything.
//  6. Execute: backup overwritten files, copy/symlink, run KDE adapter,
//     write state.
//
// Exit codes: 0 success, 1 any runtime error, 2 bad usage / refused.
func runApply(args []string) int {
	fs := flag.NewFlagSet("apply", flag.ContinueOnError)
	dryRun := fs.Bool("dry-run", false, "print the plan and exit, do not write anything")
	yes := fs.Bool("yes", false, "skip the interactive confirmation prompt")
	outOverride := fs.String("out", "", "override the build/cache directory (default ~/.riced/cache/<slug>/)")
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, "Usage: riced apply [--dry-run] [--yes] [--out DIR] <slug>\n")
		fmt.Fprint(os.Stderr, "(Flags must precede the slug -- stdlib flag parser stops at the first positional.)\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() < 1 {
		fs.Usage()
		return 2
	}
	slug := fs.Arg(0)

	home, err := os.UserHomeDir()
	if err != nil {
		slog.Error("resolve HOME", "err", err)
		return 1
	}

	// Resolve manifest through the registry.
	reg, err := registry.Load()
	if err != nil {
		slog.Error("load registry", "err", err)
		return 1
	}
	userThemes, _ := userThemesDir()
	src := registry.Source{Registry: reg, UserThemesDir: userThemes}

	themeDir, err := src.FindTheme(slug)
	if err != nil {
		slog.Error("resolve theme", "slug", slug, "err", err)
		return 1
	}
	m, err := manifest.ResolveDir(themeDir, []manifest.Source{src})
	if err != nil {
		slog.Error("load manifest", "err", err)
		return 1
	}
	if err := m.Validate(); err != nil {
		emitValidationIssues(err)
		return 1
	}

	// Generate to cache.
	outDir := *outOverride
	if outDir == "" {
		outDir = filepath.Join(home, ".riced", "cache")
	}
	themeOut := filepath.Join(outDir, m.Meta.Slug)
	if err := os.MkdirAll(themeOut, 0o755); err != nil {
		slog.Error("create cache dir", "dir", themeOut, "err", err)
		return 1
	}
	if _, err := renderAll(m, themeOut); err != nil {
		slog.Error("generate", "err", err)
		return 1
	}

	// Build plan.
	repoName, _ := registry.SplitQualified(slug)
	now := time.Now().UTC()
	statePath := apply.StatePath(home)

	// Load prev state BEFORE Build so the planner can mark Riced-managed
	// files as non-backupable (R3): backing up our own previous output
	// would otherwise overwrite the genuine user originals captured the
	// first time around.
	prevState, err := apply.LoadState(statePath)
	if err != nil {
		slog.Error("load previous state", "err", err)
		return 1
	}

	// Sticky BackupRoot: if a previous apply already captured originals
	// in a backup dir that still exists, reuse it. Keeps a single dir
	// reference for revert across A->B->A theme switches.
	backupRoot := apply.BackupRoot(home, now)
	if prevState != nil && prevState.BackupRoot != "" {
		if _, err := os.Stat(prevState.BackupRoot); err == nil {
			backupRoot = prevState.BackupRoot
		}
	}

	plan, err := apply.Build(m, themeOut, apply.NewTargets(home), statePath, backupRoot, prevState)
	if err != nil {
		slog.Error("build plan", "err", err)
		return 1
	}
	plan.Repo = repoName

	// Show the plan.
	fmt.Println(plan.Format())

	if *dryRun {
		slog.Info("dry-run, exiting before any write")
		return 0
	}

	// Refuse to touch the system if the KDE Plasma 6 tooling isn't even
	// installed. This catches "wrong desktop environment" cases before
	// any file is written, with a message the user can act on instead of
	// `exec: plasma-apply-colorscheme: file not found in $PATH` deep
	// inside Execute.
	if err := apply.CheckPrerequisites(); err != nil {
		slog.Error("prerequisites", "err", err)
		return 1
	}
	// Same idea for plasmashell -- our launcher icon JS depends on it.
	// Only warn (don't abort) when plasmashell is dead: the rest of the
	// pipeline (kdeglobals, GTK, Konsole files) still applies usefully.
	if err := apply.CheckPlasmashellRunning(); err != nil {
		slog.Warn("plasmashell probe failed; launcher icon may not update", "err", err)
	}

	if !*yes {
		if !confirm("Proceed?") {
			slog.Warn("aborted by user")
			return 2
		}
	}

	// Take the lock as late as possible (after user consent) so a stuck
	// confirmation prompt doesn't keep the system locked.
	lock, err := apply.AcquireLock(home)
	if err != nil {
		slog.Error("acquire lock", "err", err)
		return 1
	}
	defer lock.Release()

	// Lifecycle: when switching theme A → B, remove A's slug-scoped files
	// that won't be re-written by B. Without this, every apply accumulates
	// dead .colors / .profile files in ~/.local/share/. prevState was
	// loaded earlier for R3 backup planning; reuse it here.
	if removed := apply.CleanupPrevious(prevState, plan); len(removed) > 0 {
		slog.Info("removed files from previous apply", "count", len(removed))
		for _, p := range removed {
			slog.Debug("removed", "path", p)
		}
	}

	if err := apply.Execute(plan, apply.ExecOptions{
		KDE: apply.RealKDE{},
		Now: func() time.Time { return now },
	}); err != nil {
		slog.Error("execute", "err", err)
		return 1
	}

	slog.Info("applied", "slug", m.Meta.Slug, "backup", backupRoot)
	return 0
}

// confirm reads a line from stdin and returns true only for an explicit
// yes. Anything else (including EOF / non-TTY without --yes / a closed
// pipe / a read error) is treated as no -- when in doubt, abort.
func confirm(prompt string) bool {
	fmt.Fprintf(os.Stderr, "%s [y/N] ", prompt)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		// io.EOF on /dev/null, os.ErrClosed on a redirected stdin closed
		// mid-read, anything else from a wonky terminal -- be conservative.
		return false
	}
	ans := strings.ToLower(strings.TrimSpace(line))
	return ans == "y" || ans == "yes"
}
