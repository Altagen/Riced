package registry_test

import (
	"path/filepath"
	"testing"

	"github.com/Altagen/Riced/internal/registry"
)

// setupTempRegistry redirects ConfigPath to a unique file under t.TempDir.
// Restores the previous override on test cleanup.
func setupTempRegistry(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "repositories.toml")
	registry.SetConfigPath(path)
	t.Cleanup(func() { registry.SetConfigPath("") })
	return path
}

func TestRegistry_LoadMissingReturnsEmpty(t *testing.T) {
	setupTempRegistry(t)
	r, err := registry.Load()
	if err != nil {
		t.Fatalf("Load on missing file should not error: %v", err)
	}
	if len(r.Repositories) != 0 {
		t.Errorf("expected empty registry, got %d entries", len(r.Repositories))
	}
}

func TestRegistry_AddSaveLoadRoundtrip(t *testing.T) {
	setupTempRegistry(t)
	r, _ := registry.Load()
	s4Path := t.TempDir()
	if err := r.Add(registry.Repository{Name: "S4Lnx", Path: s4Path}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := r.Add(registry.Repository{Name: "catppuccin", Path: t.TempDir()}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := r.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	r2, err := registry.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(r2.Repositories) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(r2.Repositories))
	}
	if got, _ := r2.Get("S4Lnx"); got.Path != s4Path {
		t.Errorf("Path not roundtripped: got %q want %q", got.Path, s4Path)
	}
}

func TestRegistry_DuplicateAddRejected(t *testing.T) {
	setupTempRegistry(t)
	r, _ := registry.Load()
	if err := r.Add(registry.Repository{Name: "a", Path: t.TempDir()}); err != nil {
		t.Fatalf("first Add: %v", err)
	}
	if err := r.Add(registry.Repository{Name: "a", Path: t.TempDir()}); err == nil {
		t.Fatal("duplicate Add should have errored")
	}
}

func TestRegistry_Remove(t *testing.T) {
	setupTempRegistry(t)
	r, _ := registry.Load()
	_ = r.Add(registry.Repository{Name: "a", Path: t.TempDir()})
	_ = r.Add(registry.Repository{Name: "b", Path: t.TempDir()})

	if ok := r.Remove("a"); !ok {
		t.Fatal("Remove returned false for existing repo")
	}
	if ok := r.Remove("ghost"); ok {
		t.Error("Remove returned true for missing repo")
	}
	if len(r.Repositories) != 1 || r.Repositories[0].Name != "b" {
		t.Errorf("after remove want [b], got %v", r.Repositories)
	}
}

func TestRegistry_ListIsAlphabetical(t *testing.T) {
	setupTempRegistry(t)
	r, _ := registry.Load()
	_ = r.Add(registry.Repository{Name: "zeta", Path: t.TempDir()})
	_ = r.Add(registry.Repository{Name: "alpha", Path: t.TempDir()})
	_ = r.Add(registry.Repository{Name: "mu", Path: t.TempDir()})

	got := r.List()
	want := []string{"alpha", "mu", "zeta"}
	for i, w := range want {
		if got[i].Name != w {
			t.Errorf("List[%d] = %q, want %q", i, got[i].Name, w)
		}
	}
}

func TestRegistry_AddAbsolutizesPath(t *testing.T) {
	setupTempRegistry(t)
	r, _ := registry.Load()
	// relative path
	rel := "."
	if err := r.Add(registry.Repository{Name: "rel", Path: rel}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	got, _ := r.Get("rel")
	if !filepath.IsAbs(got.Path) {
		t.Errorf("expected absolute path, got %q", got.Path)
	}
}
