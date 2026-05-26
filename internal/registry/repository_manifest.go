package registry

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"

	"github.com/Altagen/Riced/internal/fsx"
)

// RepositoryManifest is the optional repository.toml at the root of a
// theme repository. It carries pack-level metadata that doesn't belong on
// any single theme -- name, description, author, homepage. Missing fields
// are filled with sensible defaults at scaffold time; missing file means
// "this repo has no metadata", which is fine.
//
// Distinct from the in-memory Repository struct used in the registry:
// Repository is "what I have registered", RepositoryManifest is "what the
// repo says about itself".
type RepositoryManifest struct {
	SchemaVersion int        `toml:"schema_version"`
	Repository    RepoHeader `toml:"repository"`
}

// RepoHeader holds the [repository] section of repository.toml.
type RepoHeader struct {
	Name        string `toml:"name"`
	Description string `toml:"description,omitempty"`
	Author      string `toml:"author,omitempty"`
	Homepage    string `toml:"homepage,omitempty"`
}

// RepositoryManifestSchema is the only repository.toml format we accept.
const RepositoryManifestSchema = 1

// RepositoryManifestFile is the well-known filename at the repo root.
const RepositoryManifestFile = "repository.toml"

// LoadRepositoryManifest reads <repoDir>/repository.toml. A missing file
// returns (nil, nil); a present but malformed file returns an error.
func LoadRepositoryManifest(repoDir string) (*RepositoryManifest, error) {
	path := filepath.Join(repoDir, RepositoryManifestFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var m RepositoryManifest
	if _, err := toml.Decode(string(data), &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if m.SchemaVersion == 0 {
		m.SchemaVersion = RepositoryManifestSchema
	}
	if m.SchemaVersion != RepositoryManifestSchema {
		return nil, fmt.Errorf("%s: schema_version %d not supported (want %d)",
			path, m.SchemaVersion, RepositoryManifestSchema)
	}
	return &m, nil
}

// SaveRepositoryManifest writes manifest to <repoDir>/repository.toml.
// Atomic write via fsx.AtomicWriteFile -- a crash leaves the previous
// content untouched.
func SaveRepositoryManifest(repoDir string, m *RepositoryManifest) error {
	if m.SchemaVersion == 0 {
		m.SchemaVersion = RepositoryManifestSchema
	}
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", repoDir, err)
	}
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(m); err != nil {
		return fmt.Errorf("encode repository manifest: %w", err)
	}
	return fsx.AtomicWriteFile(filepath.Join(repoDir, RepositoryManifestFile), buf.Bytes(), 0o644)
}

// FindRepositoryRoot walks upward from start looking for a directory that
// contains repository.toml. Returns the first match or "" if the search
// reaches the filesystem root without finding one.
//
// Used by `riced new theme` to detect "are we inside a repo?".
func FindRepositoryRoot(start string) string {
	dir, err := filepath.Abs(start)
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, RepositoryManifestFile)); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
