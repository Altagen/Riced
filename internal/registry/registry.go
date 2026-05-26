// Package registry persists the set of theme repositories the user has
// registered with Riced. The on-disk format is a single TOML file under the
// user's XDG_CONFIG_HOME (or ~/.config/riced/repositories.toml as a
// fallback). The path can be overridden in tests via SetConfigPath.
//
// A "repository" is a directory containing a themes/ subdir with one or
// more theme manifests, marked by a repository.toml at its root. Riced
// only stores the local on-disk path -- it deliberately does NOT clone or
// pull anything: that's the user's job with git directly.
package registry

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/Altagen/Riced/internal/fsx"
)

const (
	// CurrentSchema is the only registry file format version we accept.
	// Bump deliberately on breaking layout changes.
	CurrentSchema = 1

	configSubdir = "riced"
	configFile   = "repositories.toml"
)

// Repository describes one registered theme source on disk.
//
// Note: no URL / remote field. Riced never clones or pulls -- the user is
// responsible for `git clone` / `git pull`. The upstream URL, if any,
// belongs in the repository's own repository.toml (homepage field), not
// in this registry which only tracks "where is the working copy on my
// machine".
type Repository struct {
	// Name is the unique identifier the user types ("S4Lnx").
	Name string `toml:"name"`
	// Path is an absolute path to the repository's root on disk.
	Path string `toml:"path"`
}

// Registry is the in-memory representation of repositories.toml.
type Registry struct {
	SchemaVersion int          `toml:"schema_version"`
	Repositories  []Repository `toml:"repository"`

	// path is where Save writes; populated by Load.
	path string
}

// configPathOverride lets tests force a different config file location
// without mucking with environment variables. Empty means "use the XDG
// resolver".
//
// CONCURRENCY: this is a package-level mutable global. It is NOT
// goroutine-safe -- tests that use SetConfigPath must NOT call t.Parallel
// against each other. The single-process Riced CLI never reads / writes
// this var concurrently, so production is unaffected. If parallel tests
// become important, refactor Load/Save to accept the path explicitly.
var configPathOverride string

// SetConfigPath overrides the location returned by ConfigPath. Intended
// for tests only. Pass "" to restore the XDG default. NOT goroutine-safe;
// see configPathOverride.
func SetConfigPath(p string) { configPathOverride = p }

// ConfigPath returns the absolute path of the registry file. It honors the
// SetConfigPath override first, then $XDG_CONFIG_HOME, then ~/.config/.
func ConfigPath() (string, error) {
	if configPathOverride != "" {
		return configPathOverride, nil
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, configSubdir, configFile), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".config", configSubdir, configFile), nil
}

// Load reads the registry file. A missing file returns an empty Registry
// (not an error) so first-run behavior is "you have no repositories yet".
func Load() (*Registry, error) {
	path, err := ConfigPath()
	if err != nil {
		return nil, err
	}
	r := &Registry{SchemaVersion: CurrentSchema, path: path}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return r, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if _, err := toml.Decode(string(data), r); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if r.SchemaVersion == 0 {
		r.SchemaVersion = CurrentSchema
	}
	if r.SchemaVersion != CurrentSchema {
		return nil, fmt.Errorf("%s: unsupported schema version %d (want %d)",
			path, r.SchemaVersion, CurrentSchema)
	}
	r.path = path
	return r, nil
}

// Save serializes the registry back to disk. Creates parent directories as
// needed. The write is atomic via tmp+rename so a crash never leaves a
// half-written registry file.
func (r *Registry) Save() error {
	if r.path == "" {
		path, err := ConfigPath()
		if err != nil {
			return err
		}
		r.path = path
	}
	if err := os.MkdirAll(filepath.Dir(r.path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(r.path), err)
	}
	r.sortStable()
	if r.SchemaVersion == 0 {
		r.SchemaVersion = CurrentSchema
	}
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(r); err != nil {
		return fmt.Errorf("encode registry: %w", err)
	}
	return fsx.AtomicWriteFile(r.path, buf.Bytes(), 0o644)
}

// Add registers a repository. Returns an error if a repository with the
// same name already exists -- the caller decides whether to remove first or
// surface the conflict. The name is validated through ValidateName so the
// registry can't be corrupted by direct-package callers that bypass the
// CLI.
func (r *Registry) Add(repo Repository) error {
	if err := ValidateName(repo.Name); err != nil {
		return err
	}
	if repo.Path == "" {
		return errors.New("repository path must not be empty")
	}
	abs, err := filepath.Abs(repo.Path)
	if err != nil {
		return fmt.Errorf("resolve path %s: %w", repo.Path, err)
	}
	repo.Path = abs
	if _, ok := r.Get(repo.Name); ok {
		return fmt.Errorf("repository %q already registered", repo.Name)
	}
	r.Repositories = append(r.Repositories, repo)
	return nil
}

// ValidateName accepts any non-empty identifier that won't collide with
// the qualified-slug "<repo>/<slug>" syntax used by Source.FindTheme.
// Looser than the kebab-case rule for theme slugs (which become filename
// stems) -- community repository names like "S4Lnx" or "Catppuccin-KDE"
// commonly use mixed case.
func ValidateName(name string) error {
	if name == "" {
		return errors.New("repository name must not be empty")
	}
	if strings.ContainsAny(name, "/\n\r\t ") {
		return fmt.Errorf("repository name %q must not contain '/' or whitespace", name)
	}
	if name == "." || name == ".." {
		return fmt.Errorf("repository name %q is reserved", name)
	}
	return nil
}

// Remove unregisters a repository by name. Returns false if the name was
// not registered. Files on disk are never touched.
func (r *Registry) Remove(name string) bool {
	for i, repo := range r.Repositories {
		if repo.Name == name {
			r.Repositories = append(r.Repositories[:i], r.Repositories[i+1:]...)
			return true
		}
	}
	return false
}

// Get returns a copy of the repository with the given name.
func (r *Registry) Get(name string) (Repository, bool) {
	for _, repo := range r.Repositories {
		if repo.Name == name {
			return repo, true
		}
	}
	return Repository{}, false
}

// List returns a stable, alphabetically sorted slice of all repositories.
func (r *Registry) List() []Repository {
	out := make([]Repository, len(r.Repositories))
	copy(out, r.Repositories)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Path returns the on-disk location the registry will be written to.
func (r *Registry) Path() string { return r.path }

// sortStable sorts in place so saved files are diff-friendly across runs.
// Uses the same case-sensitive comparison as List() so both surfaces show
// the same ordering -- important when a user's mental model of "what's
// in the registry" comes from one or the other.
func (r *Registry) sortStable() {
	sort.Slice(r.Repositories, func(i, j int) bool {
		return r.Repositories[i].Name < r.Repositories[j].Name
	})
}
