package apply

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// ListBackups returns every backup directory under ~/.riced/state/backup/,
// most recent first. Each entry's name is the RFC3339-ish timestamp the
// backup was created with.
func ListBackups(homeDir string) ([]Backup, error) {
	root := filepath.Join(homeDir, ".riced", "state", "backup")
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", root, err)
	}
	var out []Backup
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		ts, err := time.Parse("2006-01-02T15-04-05Z", e.Name())
		if err != nil {
			// Not a Riced-formatted dir; skip rather than guess.
			continue
		}
		out = append(out, Backup{
			Path:      filepath.Join(root, e.Name()),
			Timestamp: ts,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Timestamp.After(out[j].Timestamp) })
	return out, nil
}

// Backup is one snapshot in ~/.riced/state/backup/.
type Backup struct {
	Path      string
	Timestamp time.Time
}

// GCOptions configures CleanBackups.
type GCOptions struct {
	// Keep is the number of most-recent backups to retain. Zero means
	// "keep all" -- combine with OlderThan to do time-based pruning only.
	Keep int

	// OlderThan, when non-zero, removes backups whose timestamp is older
	// than now() - OlderThan.
	OlderThan time.Duration

	// Now defaults to time.Now (UTC).
	Now func() time.Time

	// CurrentBackupRoot is the backup directory referenced by the active
	// state file. It is NEVER removed even if it falls outside Keep /
	// OlderThan -- losing it would break revert.
	CurrentBackupRoot string
}

// CleanBackups removes backups according to opts. Returns the list of
// paths that were removed.
func CleanBackups(homeDir string, opts GCOptions) ([]string, error) {
	now := opts.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	backups, err := ListBackups(homeDir)
	if err != nil {
		return nil, err
	}

	var removed []string
	for i, b := range backups {
		if b.Path == opts.CurrentBackupRoot {
			continue // never delete the live revert target
		}
		shouldRemove := false
		if opts.Keep > 0 && i >= opts.Keep {
			shouldRemove = true
		}
		if opts.OlderThan > 0 && now().Sub(b.Timestamp) > opts.OlderThan {
			shouldRemove = true
		}
		if !shouldRemove {
			continue
		}
		if err := os.RemoveAll(b.Path); err != nil {
			return removed, fmt.Errorf("remove %s: %w", b.Path, err)
		}
		removed = append(removed, b.Path)
	}
	return removed, nil
}
