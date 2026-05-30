package apply

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
)

// State persists what Riced last applied. Lives at ~/.riced/state/current.toml.
//
// Carrying both the slug and the list of written paths lets revert know
// exactly which files Riced is responsible for, without having to scan
// the user's ~/.config heuristically.
type State struct {
	SchemaVersion int `toml:"schema_version"`

	Slug       string    `toml:"slug"`
	Repo       string    `toml:"repo,omitempty"`
	AppliedAt  time.Time `toml:"applied_at"`
	WrittenAt  []string  `toml:"written_at"`
	BackupRoot string    `toml:"backup_root,omitempty"`

	// FamilySlug, when set, is the [meta.modes] family theme the user
	// originally typed (e.g. "s4-red"). Slug above is the resolved
	// variant ("s4-red-dark"). Distinguishing the two lets `riced switch`
	// and `riced status` print the family identity instead of the
	// internal variant slug.
	FamilySlug string `toml:"family_slug,omitempty"`
	// Mode is "dark" or "light" when the apply came from a family theme.
	// Empty for flat manifests.
	Mode string `toml:"mode,omitempty"`
}

const stateSchema = 1

// LoadState reads the current applied state. A missing file returns
// (nil, nil) -- the "nothing applied yet" case is normal on first run.
//
// A present-but-corrupt file is rejected with a descriptive error so the
// caller can ask the user to either fix or remove ~/.riced/state/current.toml
// rather than silently degrading.
func LoadState(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var s State
	if _, err := toml.Decode(string(data), &s); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	// Pre-versioning compatibility: any state file written before we had
	// a schema_version field will decode with SchemaVersion=0. Treat as
	// the implicit v1 it was. Drop this once schemaVersion >= 2 lands.
	if s.SchemaVersion == 0 {
		s.SchemaVersion = stateSchema
	}
	if err := s.validate(); err != nil {
		return nil, fmt.Errorf("state file %s is corrupt: %w (remove it to start fresh)", path, err)
	}
	return &s, nil
}

// validate sanity-checks a State loaded from disk. It does NOT touch the
// filesystem -- that's `riced doctor`'s job. Here we only catch obviously
// invalid records (wrong schema, empty slug, relative paths, zero time).
func (s *State) validate() error {
	if s.SchemaVersion != stateSchema {
		return fmt.Errorf("schema_version %d not supported (want %d)", s.SchemaVersion, stateSchema)
	}
	if s.Slug == "" {
		return errors.New("slug is empty")
	}
	if s.AppliedAt.IsZero() {
		return errors.New("applied_at is the zero value")
	}
	for i, p := range s.WrittenAt {
		if !filepath.IsAbs(p) {
			return fmt.Errorf("written_at[%d]: %q is not an absolute path", i, p)
		}
	}
	if s.BackupRoot != "" && !filepath.IsAbs(s.BackupRoot) {
		return fmt.Errorf("backup_root %q is not an absolute path", s.BackupRoot)
	}
	return nil
}

// Save writes the state to path, creating intermediate dirs. The write is
// atomic: we encode to a temp file in the same directory, fsync nothing
// (Linux page cache is fine for a config file), then rename over the
// target. A crash mid-encode leaves the previous state intact.
func (s *State) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	if s.SchemaVersion == 0 {
		s.SchemaVersion = stateSchema
	}
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(s); err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	return atomicWriteFile(path, buf.Bytes(), 0o644)
}

// StatePath returns ~/.riced/state/current.toml for the given home dir.
func StatePath(homeDir string) string {
	return filepath.Join(homeDir, ".riced", "state", "current.toml")
}

// BackupRoot returns ~/.riced/state/backup/<timestamp>/ for the given home.
// The timestamp is RFC3339 with colons replaced (Windows-safe even though
// we target Linux -- costs nothing to be portable here).
func BackupRoot(homeDir string, t time.Time) string {
	ts := t.UTC().Format("2006-01-02T15-04-05Z")
	return filepath.Join(homeDir, ".riced", "state", "backup", ts)
}
