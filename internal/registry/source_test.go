package registry_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Altagen/Riced/internal/registry"
)

// scaffoldRepo creates <root>/<repoName>/themes/<slug>/theme.toml so the
// fake repo looks like a real one to the resolver. Returns the repo path.
func scaffoldRepo(t *testing.T, root, repoName string, themeSlugs ...string) string {
	t.Helper()
	repoPath := filepath.Join(root, repoName)
	for _, slug := range themeSlugs {
		dir := filepath.Join(repoPath, "themes", slug)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "theme.toml"), []byte("schema_version = 1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return repoPath
}

func TestSource_QualifiedSlug(t *testing.T) {
	setupTempRegistry(t)
	root := t.TempDir()
	s4Path := scaffoldRepo(t, root, "S4Lnx", "s4-dark", "s4-red")

	r, _ := registry.Load()
	_ = r.Add(registry.Repository{Name: "S4Lnx", Path: s4Path})

	src := registry.Source{Registry: r}
	got, err := src.FindTheme("S4Lnx/s4-red")
	if err != nil {
		t.Fatalf("FindTheme: %v", err)
	}
	want := filepath.Join(s4Path, "themes", "s4-red")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSource_QualifiedSlug_UnknownRepo(t *testing.T) {
	setupTempRegistry(t)
	r, _ := registry.Load()
	src := registry.Source{Registry: r}
	_, err := src.FindTheme("Ghost/anything")
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected ErrNotExist for unknown repo, got: %v", err)
	}
}

func TestSource_BareSlug_SearchesAllRepos(t *testing.T) {
	setupTempRegistry(t)
	root := t.TempDir()
	scaffoldRepo(t, root, "A", "alpha")
	bPath := scaffoldRepo(t, root, "B", "bravo")

	r, _ := registry.Load()
	_ = r.Add(registry.Repository{Name: "A", Path: filepath.Join(root, "A")})
	_ = r.Add(registry.Repository{Name: "B", Path: bPath})

	src := registry.Source{Registry: r}
	got, err := src.FindTheme("bravo")
	if err != nil {
		t.Fatalf("FindTheme: %v", err)
	}
	if got != filepath.Join(bPath, "themes", "bravo") {
		t.Errorf("resolved to %q", got)
	}
}

func TestSource_UserThemesDirTakesPrecedence(t *testing.T) {
	setupTempRegistry(t)
	root := t.TempDir()
	repoPath := scaffoldRepo(t, root, "Pack", "red")

	// Mirror the same slug in user themes; it should win.
	userThemes := filepath.Join(root, "user-themes")
	userRed := filepath.Join(userThemes, "red")
	if err := os.MkdirAll(userRed, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userRed, "theme.toml"), []byte("schema_version = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r, _ := registry.Load()
	_ = r.Add(registry.Repository{Name: "Pack", Path: repoPath})

	src := registry.Source{Registry: r, UserThemesDir: userThemes}
	got, err := src.FindTheme("red")
	if err != nil {
		t.Fatalf("FindTheme: %v", err)
	}
	if got != userRed {
		t.Errorf("got %q, want user override %q", got, userRed)
	}
}

// TestSource_BareSlug_AmbiguousReturnsFirst verifies the K7 behavior:
// when the same bare slug exists in two registered repos, the bare lookup
// still resolves deterministically (alphabetical repo order, picking the
// first hit) so non-interactive callers stay predictable. The warning is
// a side effect on slog we don't assert here -- the value-under-test is
// that we don't error out and we return a stable choice.
func TestSource_BareSlug_AmbiguousReturnsFirst(t *testing.T) {
	setupTempRegistry(t)
	root := t.TempDir()
	aPath := scaffoldRepo(t, root, "A-repo", "shared-slug")
	bPath := scaffoldRepo(t, root, "B-repo", "shared-slug")

	r, _ := registry.Load()
	_ = r.Add(registry.Repository{Name: "A-repo", Path: aPath})
	_ = r.Add(registry.Repository{Name: "B-repo", Path: bPath})

	src := registry.Source{Registry: r}
	got, err := src.FindTheme("shared-slug")
	if err != nil {
		t.Fatalf("FindTheme should succeed even when ambiguous: %v", err)
	}
	// A-repo sorts before B-repo, so its match wins.
	wantPrefix := filepath.Join(aPath, "themes", "shared-slug")
	if got != wantPrefix {
		t.Errorf("ambiguous resolution = %q, want %q (alphabetically first)", got, wantPrefix)
	}
}

// TestSource_QualifiedSlug_MultiSlashRejected verifies K8: a "<repo>/<a>/<b>"
// string is malformed (slugs cannot contain '/') and must fail fast with a
// clear error rather than leaking a "no such file" from the filesystem.
func TestSource_QualifiedSlug_MultiSlashRejected(t *testing.T) {
	setupTempRegistry(t)
	r, _ := registry.Load()
	src := registry.Source{Registry: r}
	_, err := src.FindTheme("S4Lnx/foo/bar")
	if err == nil {
		t.Fatal("FindTheme should reject a multi-slash qualified slug")
	}
	if !strings.Contains(err.Error(), "malformed slug") {
		t.Errorf("error should mention 'malformed slug', got: %v", err)
	}
}

func TestSplitQualified(t *testing.T) {
	cases := []struct {
		in   string
		repo string
		slug string
	}{
		{"S4Lnx/s4-red", "S4Lnx", "s4-red"},
		{"s4-red", "", "s4-red"},
		{"a/b/c", "a", "b/c"}, // only first slash splits
	}
	for _, tc := range cases {
		repo, slug := registry.SplitQualified(tc.in)
		if repo != tc.repo || slug != tc.slug {
			t.Errorf("SplitQualified(%q) = (%q, %q), want (%q, %q)",
				tc.in, repo, slug, tc.repo, tc.slug)
		}
	}
}
