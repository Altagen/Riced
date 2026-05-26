package render_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/Altagen/Riced/internal/manifest"
	"github.com/Altagen/Riced/internal/render"
)

func TestKonsole_Golden(t *testing.T) {
	m, err := manifest.Load("../manifest/testdata/valid")
	if err != nil {
		t.Fatalf("load fixture manifest: %v", err)
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("fixture manifest is invalid: %v", err)
	}

	cases := []struct {
		name   string
		fn     func(*manifest.Manifest) ([]byte, error)
		golden string
	}{
		{"colorscheme", render.KonsoleColorscheme, "test-dark.colorscheme"},
		{"profile", render.KonsoleProfile, "test-dark.profile"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.fn(m)
			if err != nil {
				t.Fatalf("renderer: %v", err)
			}

			goldenPath := filepath.Join("testdata", "golden", tc.golden)

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
				t.Fatalf("read golden: %v (run tests with -args -update to create it)", err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("rendered output does not match %s\n"+
					"run `go test ./internal/render -args -update` if intentional", goldenPath)
			}
		})
	}
}

// Konsole renders must succeed against a manifest that has no [terminal] and
// no [fonts] sections. The defaults defined in the renderer take over.
func TestKonsole_DefaultsWhenMissingOptionalSections(t *testing.T) {
	m := &manifest.Manifest{
		SchemaVersion: 1,
		Meta:          manifest.Meta{Name: "Bare", Slug: "bare"},
		Palette: manifest.Palette{
			BG:     "#000000",
			Text:   "#ffffff",
			Accent: "#ff0000",
		},
	}
	scheme, err := render.KonsoleColorscheme(m)
	if err != nil {
		t.Fatalf("KonsoleColorscheme: %v", err)
	}
	if !bytes.Contains(scheme, []byte("Opacity=1.00")) {
		t.Errorf("expected Opacity=1.00 (default) in colorscheme output, got:\n%s", scheme)
	}

	profile, err := render.KonsoleProfile(m)
	if err != nil {
		t.Fatalf("KonsoleProfile: %v", err)
	}
	if !bytes.Contains(profile, []byte("Font=Monospace,11")) {
		t.Errorf("expected default mono font in profile output, got:\n%s", profile)
	}
}
