package render_test

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/Altagen/Riced/internal/manifest"
	"github.com/Altagen/Riced/internal/render"
)

// -update regenerates golden files in place. Useful after intentional
// template changes; never commit a green test that only passes because of
// -update without reading the diff.
var update = flag.Bool("update", false, "update golden files")

func TestRenderColorscheme_Golden(t *testing.T) {
	m, err := manifest.Load("../manifest/testdata/valid")
	if err != nil {
		t.Fatalf("load fixture manifest: %v", err)
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("fixture manifest is invalid: %v", err)
	}

	got, err := render.Colorscheme(m)
	if err != nil {
		t.Fatalf("Colorscheme: %v", err)
	}

	goldenPath := filepath.Join("testdata", "golden", "test-dark.colors")

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
		t.Fatalf("read golden: %v (run with -update to create it)", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("rendered output does not match golden %s\n"+
			"run `go test ./internal/render -update` if the change is intentional", goldenPath)
	}
}

func TestRenderColorscheme_PaletteFallbacks(t *testing.T) {
	// A manifest with only the required palette fields must still render --
	// the renderer's Resolve step fills the rest.
	m := &manifest.Manifest{
		SchemaVersion: 1,
		Meta:          manifest.Meta{Name: "Minimal", Slug: "minimal"},
		Palette: manifest.Palette{
			BG:     "#000000",
			Text:   "#ffffff",
			Accent: "#ff0000",
		},
	}
	out, err := render.Colorscheme(m)
	if err != nil {
		t.Fatalf("Colorscheme: %v", err)
	}
	// Sanity: the bg color must appear at least once as an RGB triplet.
	if !bytes.Contains(out, []byte("0,0,0")) {
		t.Errorf("rendered output is missing the bg color triplet 0,0,0\n---\n%s", out)
	}
}
