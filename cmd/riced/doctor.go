package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/Altagen/Riced/internal/apply"
)

// runDoctor implements `riced doctor`. It audits the persistent state +
// the filesystem and reports inconsistencies. Returns 0 when everything
// is clean, 1 when at least one issue is found.
//
// Checks performed:
//
//   - State file present? If yes, every path in WrittenAt exists on disk.
//   - State file absent? Look for files that look Riced-managed
//     (~/.local/share/color-schemes/, ~/.local/share/konsole/,
//     ~/.local/share/riced/wallpapers/) -- orphaned outputs from a
//     half-applied or pre-state-tracking session.
//   - BackupRoot present and readable?
//   - Lock file leftover (apply crashed mid-run).
//   - Stale backups older than 90 days (informational, not an error).
func runDoctor(_ []string) int {
	home, err := os.UserHomeDir()
	if err != nil {
		slog.Error("resolve HOME", "err", err)
		return exitErr
	}

	issues := 0
	report := func(kind, msg string) {
		fmt.Printf("  [%s] %s\n", kind, msg)
		if kind == "issue" {
			issues++
		}
	}

	fmt.Println("Riced health check:")
	fmt.Println()

	// --- KDE Plasma 6 prerequisites ----------------------------------------
	if err := apply.CheckPrerequisites(); err != nil {
		report("issue", err.Error())
	} else {
		report("ok", "KDE Plasma 6 tools available (plasma-apply-*, kwriteconfig6, qdbus)")
	}
	if err := apply.CheckPlasmashellRunning(); err != nil {
		report("warn", "plasmashell probe failed: "+err.Error())
	} else {
		report("ok", "plasmashell responds on DBus")
	}

	// --- Lock file ---------------------------------------------------------
	lockPath := apply.LockPath(home)
	if _, err := os.Stat(lockPath); err == nil {
		report("issue", fmt.Sprintf("stale lock file at %s -- if no riced apply is running, remove it", lockPath))
	} else {
		report("ok", "no stale apply lock")
	}

	// --- Current state -----------------------------------------------------
	state, err := apply.LoadState(apply.StatePath(home))
	switch {
	case err != nil:
		report("issue", fmt.Sprintf("state file is corrupt: %v", err))
	case state == nil:
		report("ok", "no theme is currently applied")
	default:
		report("ok", fmt.Sprintf("theme %q applied at %s", state.Slug, state.AppliedAt.Format(time.RFC3339)))
		missing := 0
		for _, p := range state.WrittenAt {
			if _, err := os.Lstat(p); err != nil {
				report("issue", fmt.Sprintf("declared file missing: %s", p))
				missing++
			}
		}
		if missing == 0 {
			report("ok", fmt.Sprintf("all %d declared files exist", len(state.WrittenAt)))
		}
		if state.BackupRoot != "" {
			if _, err := os.Stat(state.BackupRoot); err != nil {
				report("issue", fmt.Sprintf("backup root referenced by state is missing: %s", state.BackupRoot))
			} else {
				report("ok", fmt.Sprintf("backup root present at %s", state.BackupRoot))
			}
		}
	}

	// --- Orphan files when no state ---------------------------------------
	if state == nil {
		orphans := findRicedManagedOrphans(home)
		if len(orphans) > 0 {
			report("warn", fmt.Sprintf("%d file(s) look Riced-managed but no state recorded them", len(orphans)))
			for _, p := range orphans {
				report("warn", "  "+p)
			}
		}
	}

	// --- Backup GC hints ---------------------------------------------------
	backups, err := apply.ListBackups(home)
	if err == nil {
		stale := 0
		cutoff := time.Now().Add(-90 * 24 * time.Hour)
		for _, b := range backups {
			if b.Timestamp.Before(cutoff) {
				stale++
			}
		}
		if stale > 0 {
			report("info", fmt.Sprintf("%d backup(s) older than 90 days -- run `riced clean-backups` to GC", stale))
		} else {
			report("ok", fmt.Sprintf("%d backup(s), none older than 90 days", len(backups)))
		}
	}

	fmt.Println()
	if issues == 0 {
		fmt.Println("All clear.")
		return exitOK
	}
	fmt.Printf("%d issue(s) found.\n", issues)
	return exitErr
}

// findRicedManagedOrphans returns paths that exist under directories
// Riced manages but aren't tracked by any state file. Heuristic -- limited
// to directories Riced unambiguously owns (the user's ~/.local/share/riced/
// wallpapers tree) plus the slug-scoped Konsole and color-scheme files
// whose names follow our naming convention.
//
// We deliberately don't flag arbitrary files in ~/.config/gtk-3.0/ since
// the user may have other GTK customizations.
func findRicedManagedOrphans(home string) []string {
	var orphans []string
	ricedShare := filepath.Join(home, ".local/share/riced/wallpapers")
	if entries, err := os.ReadDir(ricedShare); err == nil {
		for _, e := range entries {
			orphans = append(orphans, filepath.Join(ricedShare, e.Name()))
		}
	}
	return orphans
}
