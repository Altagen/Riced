package apply

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// CleanupPrevious removes files that the previous apply wrote but that
// the upcoming plan won't overwrite. This is the lifecycle fix for
// switching A → B: without it, A's slug-scoped files (e.g.
// ~/.local/share/color-schemes/s4-dark.colors) stay forever even after
// applying s4-red.
//
// Files in prev.WrittenAt that ARE also in plan.WrittenPaths are kept
// untouched -- they will be overwritten by Execute, which will back them
// up like any other pre-existing file.
//
// Returns the list of removed paths for the caller to log.
func CleanupPrevious(prev *State, plan *Plan) []string {
	if prev == nil {
		return nil
	}
	upcoming := map[string]struct{}{}
	for _, a := range plan.Actions {
		if a.Dst != "" {
			upcoming[a.Dst] = struct{}{}
		}
	}

	var removed []string
	for _, p := range prev.WrittenAt {
		if _, kept := upcoming[p]; kept {
			continue
		}
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			slog.Warn("cleanup: remove failed", "path", p, "err", err)
			continue
		}
		removed = append(removed, p)
	}

	// Best-effort empty-dir pruning: walk parent dirs of removed files and
	// rmdir them while empty. Limited to ~/.local/share/riced/wallpapers/<slug>
	// and similar Riced-owned dirs -- we never touch ~/.config/gtk-3.0/.
	for _, p := range removed {
		pruneEmptyRicedDirs(p)
	}
	return removed
}

// pruneEmptyRicedDirs walks up from path's directory and removes any
// empty directory that lives under ~/.local/share/riced/. Stops as soon
// as it reaches a non-empty directory or exits the Riced-managed tree.
//
// Safety: we ANCHOR the trusted prefix on os.UserHomeDir() rather than
// pattern-matching the path's segments. A path that doesn't sit under
// the user's actual home dir is rejected -- meaning no amount of weird
// path shape can lead us to rmdir anything outside ~/.local/share/riced.
func pruneEmptyRicedDirs(path string) {
	ricedRoot, ok := ricedShareRoot(path)
	if !ok {
		return
	}
	dir := filepath.Dir(path)
	for dir != "" && dir != "/" && dir != ricedRoot {
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) > 0 {
			return
		}
		if err := os.Remove(dir); err != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}

// ricedShareRoot returns the absolute path of <HOME>/.local/share/riced
// when path is inside that tree, and (_, false) otherwise. Anchoring on
// the live HOME (rather than walking the path's segments) is the safety
// net that lets pruneEmptyRicedDirs claim it can never touch unrelated
// directories.
func ricedShareRoot(path string) (string, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}
	root := filepath.Join(home, ".local", "share", "riced")
	rootWithSep := root + string(filepath.Separator)
	if path == root || strings.HasPrefix(path, rootWithSep) {
		return root, true
	}
	return "", false
}
