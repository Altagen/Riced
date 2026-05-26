package manifest

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Source is anything that can locate a theme directory by slug.
//
// Implementations return the absolute path of a directory containing a
// theme.toml file, or os.ErrNotExist (wrapped) when nothing matches. They
// must not load or parse the manifest themselves.
type Source interface {
	FindTheme(slug string) (string, error)
}

// DirSource is a Source rooted at a single themes/ directory. It treats every
// immediate subdirectory containing a theme.toml as a candidate.
type DirSource struct {
	Root string // absolute or relative path to a directory containing themes/<slug>/ entries
}

// FindTheme returns <Root>/<slug>/ if that directory contains a theme.toml.
func (d DirSource) FindTheme(slug string) (string, error) {
	candidate := filepath.Join(d.Root, slug)
	if !looksLikeThemeDir(candidate) {
		return "", fmt.Errorf("theme %q: %w", slug, os.ErrNotExist)
	}
	abs, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	return abs, nil
}

// looksLikeThemeDir is true when path/theme.toml exists and is a regular file.
func looksLikeThemeDir(path string) bool {
	info, err := os.Stat(filepath.Join(path, FileName))
	return err == nil && info.Mode().IsRegular()
}

// findFirst walks sources in order and returns the first hit. Errors from
// individual sources that wrap os.ErrNotExist are absorbed (we move on to
// the next source); any other error short-circuits. When NO source
// matched we return a single composite error naming the slug -- much more
// actionable than echoing whichever source happened to be last.
func findFirst(slug string, sources []Source) (string, error) {
	for _, s := range sources {
		dir, err := s.FindTheme(slug)
		if err == nil {
			return dir, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
	return "", fmt.Errorf("theme %q not found in any source (searched %d source(s)): %w",
		slug, len(sources), os.ErrNotExist)
}

// ResolveDir loads the theme at dir and recursively merges in any parent
// referenced through Meta.Inherits. The parent is searched in this order:
//
//  1. siblings of dir (i.e. parent-of-dir/<inherits-slug>/)
//  2. each Source in sources
//
// The returned Manifest has Dir set to dir; path fields inherited from a
// parent are rewritten to absolute paths so validate / render keep working
// against a single base directory.
func ResolveDir(dir string, sources []Source) (*Manifest, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve theme dir: %w", err)
	}
	info, err := os.Stat(absDir)
	if err != nil {
		return nil, fmt.Errorf("stat theme dir %s: %w", absDir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", absDir)
	}

	return resolveWithVisited(absDir, sources, map[string]bool{})
}

// resolveWithVisited is the entry point of the inheritance walk. It
// delegates the whole load-and-merge dance to loadRawForMerge (which is
// recursive), then decodes the final map into a typed Manifest and sets
// Dir on it. Keeping the walk in ONE function means cycle detection,
// inherits resolution and path absolutization can only diverge in one
// place.
func resolveWithVisited(dir string, sources []Source, visited map[string]bool) (*Manifest, error) {
	raw, err := loadRawForMerge(dir, sources, visited)
	if err != nil {
		return nil, err
	}
	m, err := decodeMerged(raw)
	if err != nil {
		return nil, err
	}
	m.Dir = dir
	return m, nil
}

// loadRawForMerge loads <dir>/theme.toml, recursively walks its
// inherits chain, and returns the fully-merged raw map. Used directly
// when a sub-load needs the map (parent of a child being resolved), and
// indirectly through resolveWithVisited when the caller wants a typed
// Manifest.
func loadRawForMerge(dir string, sources []Source, visited map[string]bool) (map[string]any, error) {
	if visited[dir] {
		return nil, fmt.Errorf("inheritance cycle detected at %s", dir)
	}
	visited[dir] = true

	raw, err := loadRawTOML(filepath.Join(dir, FileName))
	if err != nil {
		return nil, err
	}
	parentSlug := extractInherits(raw)
	if parentSlug == "" {
		return raw, nil
	}

	// Siblings of dir take priority for resolving inherits -- they are
	// almost always what the author means (variants of the same pack).
	siblingSrc := DirSource{Root: filepath.Dir(dir)}
	allSources := append([]Source{siblingSrc}, sources...)
	parentDir, err := findFirst(parentSlug, allSources)
	if err != nil {
		return nil, fmt.Errorf("resolve inherits %q from %s: %w", parentSlug, dir, err)
	}
	parentRaw, err := loadRawForMerge(parentDir, sources, visited)
	if err != nil {
		return nil, err
	}
	// Rewrite the parent's path fields to absolute so they still resolve
	// after the merge, at which point the typed Manifest's Dir is the
	// CHILD's directory.
	absolutizePaths(parentRaw, parentDir)
	return deepMerge(parentRaw, raw), nil
}

// extractInherits reads [meta].inherits from a raw manifest map.
func extractInherits(raw map[string]any) string {
	meta, ok := raw["meta"].(map[string]any)
	if !ok {
		return ""
	}
	s, _ := meta["inherits"].(string)
	return s
}
