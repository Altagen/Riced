package registry_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Altagen/Riced/internal/registry"
)

func TestRepositoryManifest_LoadMissingReturnsNil(t *testing.T) {
	tmp := t.TempDir()
	m, err := registry.LoadRepositoryManifest(tmp)
	if err != nil {
		t.Fatalf("LoadRepositoryManifest on empty dir should not error: %v", err)
	}
	if m != nil {
		t.Errorf("expected nil for missing manifest, got %+v", m)
	}
}

func TestRepositoryManifest_SaveLoadRoundtrip(t *testing.T) {
	tmp := t.TempDir()
	want := &registry.RepositoryManifest{
		Repository: registry.RepoHeader{
			Name:        "S4Lnx",
			Description: "S4 League rice",
			Author:      "Altagen",
			Homepage:    "https://example.com",
		},
	}
	if err := registry.SaveRepositoryManifest(tmp, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := registry.LoadRepositoryManifest(tmp)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got == nil {
		t.Fatal("Load returned nil after Save")
	}
	if got.Repository.Name != want.Repository.Name {
		t.Errorf("Name = %q, want %q", got.Repository.Name, want.Repository.Name)
	}
	if got.Repository.Author != want.Repository.Author {
		t.Errorf("Author = %q, want %q", got.Repository.Author, want.Repository.Author)
	}
}

func TestFindRepositoryRoot_Found(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "S4Lnx")
	deep := filepath.Join(repo, "themes", "s4-red")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := registry.SaveRepositoryManifest(repo, &registry.RepositoryManifest{
		Repository: registry.RepoHeader{Name: "S4Lnx"},
	}); err != nil {
		t.Fatal(err)
	}

	got := registry.FindRepositoryRoot(deep)
	if got != repo {
		t.Errorf("FindRepositoryRoot from %s = %q, want %q", deep, got, repo)
	}
}

func TestFindRepositoryRoot_NotFound(t *testing.T) {
	// A tmp dir with no repository.toml anywhere upward (well, hopefully --
	// we don't control / on the test runner, but tmp is far from any repo).
	got := registry.FindRepositoryRoot(t.TempDir())
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}
