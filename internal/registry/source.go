package registry

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// Source adapts a Registry to manifest.Source semantics without creating an
// import cycle. It supports two slug formats:
//
//   - bare slug ("s4-red") -- searched across every registered repository,
//     plus the user's ~/.riced/themes/ override directory if it exists
//   - qualified slug ("S4Lnx/s4-red") -- resolved against exactly one repo
//
// We re-implement the FindTheme signature here (rather than importing
// manifest.Source) so that manifest stays the leaf package. Callers in
// cmd/riced compose the two.
type Source struct {
	Registry      *Registry
	UserThemesDir string // optional ~/.riced/themes/; ignored when empty
}

// FindTheme implements manifest.Source.
//
// Qualified form is rejected up front if the slug part contains another
// '/' (e.g. "S4Lnx/foo/bar") -- slugs are kebab-case identifiers and
// can never contain a slash, so it's better to catch this here than to
// let it leak a confusing "directory not found" error from resolveInDir.
//
// For bare lookups we walk every registered repo (and the user override
// dir if set), collect every match, and warn when more than one repo
// satisfies the slug. The first match still wins deterministically so
// non-interactive callers behave predictably; the warning tells the
// user how to disambiguate by qualifying with <repo>/.
func (s Source) FindTheme(slug string) (string, error) {
	repoName, themeSlug := SplitQualified(slug)

	if repoName != "" {
		if strings.ContainsRune(themeSlug, '/') {
			return "", fmt.Errorf("malformed slug %q: a theme slug cannot contain '/' (got %q after the repo)", slug, themeSlug)
		}
		repo, ok := s.Registry.Get(repoName)
		if !ok {
			return "", fmt.Errorf("repository %q is not registered: %w", repoName, os.ErrNotExist)
		}
		return resolveInDir(filepath.Join(repo.Path, "themes"), themeSlug)
	}

	// Bare slug. User override dir takes precedence -- it's where the
	// user puts their own customizations and they shouldn't be shadowed
	// by anything in a registered repo.
	var matches []string
	if s.UserThemesDir != "" {
		if dir, err := resolveInDir(s.UserThemesDir, slug); err == nil {
			matches = append(matches, dir)
		}
	}
	var repoMatches []struct{ repo, dir string }
	for _, repo := range s.Registry.List() {
		if dir, err := resolveInDir(filepath.Join(repo.Path, "themes"), slug); err == nil {
			matches = append(matches, dir)
			repoMatches = append(repoMatches, struct{ repo, dir string }{repo.Name, dir})
		}
	}

	if len(matches) == 0 {
		return "", fmt.Errorf("theme %q not found in any repository: %w", slug, os.ErrNotExist)
	}
	if len(matches) > 1 {
		// Ambiguous bare lookup. We still return the first match -- behavior
		// stays deterministic -- but the user almost certainly meant to
		// qualify. Help them disambiguate next time.
		hints := make([]string, 0, len(repoMatches))
		for _, m := range repoMatches {
			hints = append(hints, m.repo+"/"+slug)
		}
		slog.Warn("slug is ambiguous; picked first match",
			"slug", slug,
			"matches", matches,
			"hint", "qualify with one of: "+strings.Join(hints, ", "))
	}
	return matches[0], nil
}

// SplitQualified separates "<repo>/<slug>" into ("<repo>", "<slug>"). A
// bare slug returns ("", slug). Only the FIRST slash is honored -- any
// additional slashes stay in the themeSlug part and are detected by
// FindTheme (since slugs may not contain '/').
func SplitQualified(slug string) (repo, themeSlug string) {
	if i := strings.IndexByte(slug, '/'); i >= 0 {
		return slug[:i], slug[i+1:]
	}
	return "", slug
}

// resolveInDir returns <base>/<slug>/ when it contains a theme.toml, or a
// wrapped os.ErrNotExist otherwise. Absolute path on success.
func resolveInDir(base, slug string) (string, error) {
	candidate := filepath.Join(base, slug)
	if _, err := os.Stat(filepath.Join(candidate, "theme.toml")); err != nil {
		return "", fmt.Errorf("%s: %w", candidate, os.ErrNotExist)
	}
	return filepath.Abs(candidate)
}
