package apply_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Altagen/Riced/internal/apply"
)

// TestCleanupPrevious_RemovesOrphans confirms that files left behind by
// a previous slug -- i.e. paths in prevState.WrittenAt that are NOT in the
// upcoming plan -- get removed when the next apply runs.
func TestCleanupPrevious_RemovesOrphans(t *testing.T) {
	home := t.TempDir()

	// Simulate state A: wrote two color-schemes-shaped files.
	orphan := filepath.Join(home, ".local/share/color-schemes/s4-dark.colors")
	keeper := filepath.Join(home, ".local/share/color-schemes/s4-red.colors")
	for _, p := range []string{orphan, keeper} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("stub"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	prev := &apply.State{
		Slug:      "s4-dark",
		WrittenAt: []string{orphan, keeper},
	}
	// Upcoming plan: keeps `keeper` (will overwrite it), but does NOT touch orphan.
	plan := &apply.Plan{
		Slug: "s4-red",
		Actions: []apply.Action{
			{Kind: "copy", Dst: keeper},
		},
	}

	removed := apply.CleanupPrevious(prev, plan)
	if len(removed) != 1 || removed[0] != orphan {
		t.Errorf("expected orphan to be removed; removed=%v", removed)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Errorf("orphan still exists after cleanup: %v", err)
	}
	if _, err := os.Stat(keeper); err != nil {
		t.Errorf("keeper was wrongly removed: %v", err)
	}
}

// TestCleanupPrevious_NilStateIsNoop guards against a nil prevState
// (first-ever apply) crashing.
func TestCleanupPrevious_NilStateIsNoop(t *testing.T) {
	got := apply.CleanupPrevious(nil, &apply.Plan{})
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

// TestCleanupPrevious_PrunesEmptyWallpaperDir verifies that removing the
// last symlink in ~/.local/share/riced/wallpapers/<slug>/ also rmdirs
// the now-empty <slug> directory. We never touch dirs outside
// ~/.local/share/riced/.
func TestCleanupPrevious_PrunesEmptyWallpaperDir(t *testing.T) {
	home := t.TempDir()
	// pruneEmptyRicedDirs anchors on os.UserHomeDir() (which reads $HOME
	// on Linux) so it never wipes anything outside the live user's
	// ~/.local/share/riced. Tell os.UserHomeDir about the temp tree.
	t.Setenv("HOME", home)
	wpDir := filepath.Join(home, ".local/share/riced/wallpapers/s4-dark")
	wallpaper := filepath.Join(wpDir, "01-pic.png")
	if err := os.MkdirAll(wpDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(wallpaper, []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}

	prev := &apply.State{Slug: "s4-dark", WrittenAt: []string{wallpaper}}
	plan := &apply.Plan{Slug: "s4-red"} // no overlap

	apply.CleanupPrevious(prev, plan)

	if _, err := os.Stat(wpDir); !os.IsNotExist(err) {
		t.Errorf("empty wallpaper dir should have been pruned; stat err=%v", err)
	}
	// Sibling dirs above (.local/share/riced/wallpapers/) -- fine to also be
	// pruned, since they're under the riced root. But the riced root itself
	// must survive.
	if _, err := os.Stat(filepath.Join(home, ".local/share/riced")); err != nil {
		t.Errorf("~/.local/share/riced was wrongly pruned: %v", err)
	}
}
