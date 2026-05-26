package apply_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Altagen/Riced/internal/apply"
)

// mkBackup creates a fake backup directory under ~/.riced/state/backup/<ts>/.
func mkBackup(t *testing.T, home string, ts time.Time) string {
	t.Helper()
	path := apply.BackupRoot(home, ts)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	// Drop a placeholder file so the dir isn't empty (more realistic).
	_ = os.WriteFile(filepath.Join(path, "placeholder"), []byte("x"), 0o644)
	return path
}

func TestCleanBackups_KeepsMostRecentN(t *testing.T) {
	home := t.TempDir()
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	var paths []string
	for i := 0; i < 7; i++ {
		paths = append(paths, mkBackup(t, home, now.Add(-time.Duration(i)*time.Hour)))
	}

	removed, err := apply.CleanBackups(home, apply.GCOptions{
		Keep: 3,
		Now:  func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("CleanBackups: %v", err)
	}
	if len(removed) != 4 {
		t.Errorf("expected 4 removed, got %d (%v)", len(removed), removed)
	}
	// The first 3 (most recent) must still exist.
	for _, p := range paths[:3] {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected %s to be kept: %v", p, err)
		}
	}
}

func TestCleanBackups_OlderThan(t *testing.T) {
	home := t.TempDir()
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	old := mkBackup(t, home, now.Add(-30*24*time.Hour))
	recent := mkBackup(t, home, now.Add(-1*time.Hour))

	removed, err := apply.CleanBackups(home, apply.GCOptions{
		Keep:      0,
		OlderThan: 7 * 24 * time.Hour,
		Now:       func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || removed[0] != old {
		t.Errorf("expected %s removed, got %v", old, removed)
	}
	if _, err := os.Stat(recent); err != nil {
		t.Errorf("recent backup should have been kept: %v", err)
	}
}

func TestCleanBackups_NeverRemovesCurrent(t *testing.T) {
	home := t.TempDir()
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	current := mkBackup(t, home, now.Add(-100*time.Hour))
	// Also make a newer one so current would normally be off the Keep=1 list.
	newer := mkBackup(t, home, now.Add(-1*time.Hour))

	removed, err := apply.CleanBackups(home, apply.GCOptions{
		Keep:              1,
		Now:               func() time.Time { return now },
		CurrentBackupRoot: current,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range removed {
		if p == current {
			t.Errorf("current backup %s was removed by GC", current)
		}
	}
	if _, err := os.Stat(current); err != nil {
		t.Errorf("current backup should still exist: %v", err)
	}
	if _, err := os.Stat(newer); err != nil {
		t.Errorf("newer backup should still exist: %v", err)
	}
}
