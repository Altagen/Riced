package manifest_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Altagen/Riced/internal/manifest"
)

func TestResolveDir_NoInherits(t *testing.T) {
	// Plain themes without an inherits field must keep working through the
	// new resolver -- backwards compat with Phase 2.
	m, err := manifest.ResolveDir("testdata/valid", nil)
	if err != nil {
		t.Fatalf("ResolveDir: %v", err)
	}
	if m.Meta.Slug != "test-dark" {
		t.Errorf("Meta.Slug = %q, want test-dark", m.Meta.Slug)
	}
	if err := m.Validate(); err != nil {
		t.Errorf("Validate on valid manifest: %v", err)
	}
}

func TestResolveDir_SimpleInheritance(t *testing.T) {
	m, err := manifest.ResolveDir("testdata/inherit/child", nil)
	if err != nil {
		t.Fatalf("ResolveDir: %v", err)
	}

	// Child overrides accent.
	if m.Palette.Accent != "#ff2a4b" {
		t.Errorf("Palette.Accent = %q, want #ff2a4b (child override)", m.Palette.Accent)
	}
	// Inherited from parent.
	if m.Palette.BG != "#0a0a14" {
		t.Errorf("Palette.BG = %q, want #0a0a14 (inherited)", m.Palette.BG)
	}
	if m.Palette.Text != "#e8e8f0" {
		t.Errorf("Palette.Text = %q, want inherited #e8e8f0", m.Palette.Text)
	}
	if m.Window.Decoration != "klassy" {
		t.Errorf("Window.Decoration = %q, want klassy (inherited)", m.Window.Decoration)
	}
	if m.Fonts.UI != "Inter" {
		t.Errorf("Fonts.UI = %q, want Inter (inherited)", m.Fonts.UI)
	}

	// Path fields inherited from the parent must be absolute now, pointing
	// into the parent's directory.
	if len(m.Wallpapers.Paths) != 1 {
		t.Fatalf("Wallpapers.Paths len = %d, want 1", len(m.Wallpapers.Paths))
	}
	got := m.Wallpapers.Paths[0]
	if !filepath.IsAbs(got) {
		t.Errorf("inherited wallpaper path is not absolute: %q", got)
	}
	if !strings.HasSuffix(got, "/testdata/inherit/base/wall.png") {
		t.Errorf("inherited wallpaper path %q does not point into base/", got)
	}

	// Validation must still pass -- the absolute path resolves to the real
	// stub file under base/.
	if err := m.Validate(); err != nil {
		t.Errorf("Validate on merged manifest: %v", err)
	}
}

func TestResolveDir_MultiLevelInheritance(t *testing.T) {
	m, err := manifest.ResolveDir("testdata/inherit/grandchild", nil)
	if err != nil {
		t.Fatalf("ResolveDir: %v", err)
	}

	// Grandchild → Child → Base
	// bg from grandchild, accent from child, text from base.
	checks := map[string]string{
		"Palette.BG":     "#000000",
		"Palette.Accent": "#ff2a4b",
		"Palette.Text":   "#e8e8f0",
	}
	got := map[string]string{
		"Palette.BG":     m.Palette.BG,
		"Palette.Accent": m.Palette.Accent,
		"Palette.Text":   m.Palette.Text,
	}
	for field, want := range checks {
		if got[field] != want {
			t.Errorf("%s = %q, want %q", field, got[field], want)
		}
	}

	if err := m.Validate(); err != nil {
		t.Errorf("Validate on merged manifest: %v", err)
	}
}

func TestResolveDir_CycleDetected(t *testing.T) {
	_, err := manifest.ResolveDir("testdata/inherit_cycle/a", nil)
	if err == nil {
		t.Fatal("expected cycle detection error, got nil")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("error %q does not mention 'cycle'", err.Error())
	}
}

func TestResolveDir_UnknownParent(t *testing.T) {
	// Point at a child that inherits from "base", but with no sibling and
	// no source available → must error cleanly.
	tmp := t.TempDir()
	themePath := filepath.Join(tmp, "orphan")
	if err := writeFile(t, filepath.Join(themePath, manifest.FileName), `
schema_version = 1
[meta]
name = "Orphan"
slug = "orphan"
inherits = "ghost"
[palette]
bg = "#000000"
text = "#ffffff"
accent = "#ff0000"
[wallpapers]
paths = ["w.png"]
`); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(t, filepath.Join(themePath, "w.png"), "stub\n"); err != nil {
		t.Fatal(err)
	}
	_, err := manifest.ResolveDir(themePath, nil)
	if err == nil {
		t.Fatal("expected error for missing parent")
	}
	if !strings.Contains(err.Error(), "ghost") {
		t.Errorf("error %q does not mention the missing parent slug", err.Error())
	}
}

func TestResolveDir_DirSource(t *testing.T) {
	// A child theme that lives outside the parent's directory must be
	// resolvable through a Source.
	tmp := t.TempDir()

	// Layout:
	//   tmp/pack/themes/base/theme.toml
	//   tmp/other/themes/leaf/theme.toml  (inherits = "base")
	mustWrite := func(p, body string) {
		if err := writeFile(t, p, body); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(filepath.Join(tmp, "pack/themes/base/theme.toml"), `
schema_version = 1
[meta]
name = "Pack Base"
slug = "base"
[palette]
bg = "#111111"
text = "#eeeeee"
accent = "#aabbcc"
[wallpapers]
paths = ["wall.png"]
`)
	mustWrite(filepath.Join(tmp, "pack/themes/base/wall.png"), "stub\n")

	mustWrite(filepath.Join(tmp, "other/themes/leaf/theme.toml"), `
schema_version = 1
[meta]
name = "Leaf"
slug = "leaf"
inherits = "base"
[palette]
accent = "#ff00ff"
`)

	src := manifest.DirSource{Root: filepath.Join(tmp, "pack", "themes")}
	m, err := manifest.ResolveDir(filepath.Join(tmp, "other/themes/leaf"), []manifest.Source{src})
	if err != nil {
		t.Fatalf("ResolveDir: %v", err)
	}
	if m.Palette.Accent != "#ff00ff" {
		t.Errorf("Palette.Accent = %q, want #ff00ff", m.Palette.Accent)
	}
	if m.Palette.BG != "#111111" {
		t.Errorf("Palette.BG = %q, want #111111 (from external source)", m.Palette.BG)
	}
	if !filepath.IsAbs(m.Wallpapers.Paths[0]) ||
		!strings.HasSuffix(m.Wallpapers.Paths[0], "pack/themes/base/wall.png") {
		t.Errorf("inherited wallpaper path = %q, want absolute pointing into the external source",
			m.Wallpapers.Paths[0])
	}
}
