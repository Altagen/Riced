package render_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/Altagen/Riced/internal/manifest"
	"github.com/Altagen/Riced/internal/render"
)

// fixedDir is a stable absolute path passed to the wallpaper renderer so the
// golden file is deterministic across machines.
const fixedDir = "/test/path/build/test-dark/wallpapers"

func TestWallpapers_GoldenSlideshow(t *testing.T) {
	m, err := manifest.Load("../manifest/testdata/valid")
	if err != nil {
		t.Fatalf("load fixture manifest: %v", err)
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("fixture manifest is invalid: %v", err)
	}

	got, err := render.Wallpapers(m, render.WallpaperOpts{WallpapersDir: fixedDir})
	if err != nil {
		t.Fatalf("Wallpapers: %v", err)
	}

	goldenPath := filepath.Join("testdata", "golden", "test-dark.wallpaper.ini")

	if *update {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("updated %s", goldenPath)
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden: %v (run with -args -update to create it)", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("rendered output does not match %s", goldenPath)
	}
}

// In single mode the renderer points Image= at the symlink that *will* be
// created for the first manifest entry (NN-<basename>).
func TestWallpapers_SingleMode(t *testing.T) {
	m := &manifest.Manifest{
		SchemaVersion: 1,
		Meta:          manifest.Meta{Name: "Solo", Slug: "solo"},
		Palette: manifest.Palette{
			BG: "#000000", Text: "#ffffff", Accent: "#ff0000",
		},
		Wallpapers: manifest.Wallpapers{
			Mode:  "single",
			Paths: []string{"backgrounds/cool.png"},
		},
	}
	out, err := render.Wallpapers(m, render.WallpaperOpts{WallpapersDir: "/x"})
	if err != nil {
		t.Fatalf("Wallpapers: %v", err)
	}
	for _, want := range []string{
		"wallpaperPlugin=org.kde.image",
		"[Wallpaper][org.kde.image][General]",
		"Image=/x/01-cool.png",
	} {
		if !bytes.Contains(out, []byte(want)) {
			t.Errorf("missing line %q in output:\n%s", want, out)
		}
	}
}

func TestWallpapers_RejectsEmpty(t *testing.T) {
	m := &manifest.Manifest{
		Wallpapers: manifest.Wallpapers{Paths: nil},
	}
	if _, err := render.Wallpapers(m, render.WallpaperOpts{WallpapersDir: "/x"}); err == nil {
		t.Fatal("expected error for empty wallpaper list")
	}
}

func TestWallpapers_RejectsMissingDir(t *testing.T) {
	m := &manifest.Manifest{
		Wallpapers: manifest.Wallpapers{Paths: []string{"a.png"}},
	}
	if _, err := render.Wallpapers(m, render.WallpaperOpts{}); err == nil {
		t.Fatal("expected error for missing WallpapersDir")
	}
}
