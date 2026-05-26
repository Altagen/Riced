package render_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/Altagen/Riced/internal/manifest"
	"github.com/Altagen/Riced/internal/render"
)

func TestGTKCSS_Golden(t *testing.T) {
	m, err := manifest.Load("../manifest/testdata/valid")
	if err != nil {
		t.Fatalf("load fixture manifest: %v", err)
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("fixture manifest is invalid: %v", err)
	}

	got, err := render.GTKCSS(m)
	if err != nil {
		t.Fatalf("GTKCSS: %v", err)
	}

	goldenPath := filepath.Join("testdata", "golden", "test-dark.gtk.css")

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

func TestGTKCSS_AccentSurfaces(t *testing.T) {
	// The user's accent must end up tagged on every accent_*-prefixed color
	// (accent_color, accent_bg_color). Smoke-checks the wiring.
	m := &manifest.Manifest{
		SchemaVersion: 1,
		Meta:          manifest.Meta{Name: "Acc", Slug: "acc"},
		Palette: manifest.Palette{
			BG:     "#000000",
			Text:   "#ffffff",
			Accent: "#ff00aa",
		},
	}
	out, err := render.GTKCSS(m)
	if err != nil {
		t.Fatalf("GTKCSS: %v", err)
	}
	for _, want := range []string{
		"@define-color accent_color #ff00aa;",
		"@define-color accent_bg_color #ff00aa;",
		"@define-color window_bg_color #000000;",
		"@define-color window_fg_color #ffffff;",
	} {
		if !bytes.Contains(out, []byte(want)) {
			t.Errorf("missing line %q in GTKCSS output:\n%s", want, out)
		}
	}
}
