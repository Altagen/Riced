package manifest_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/Altagen/Riced/internal/manifest"
)

func TestLoad_Valid(t *testing.T) {
	m, err := manifest.Load("testdata/valid")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if m.Meta.Slug != "test-dark" {
		t.Errorf("Meta.Slug = %q, want test-dark", m.Meta.Slug)
	}
	if m.SchemaVersion != 1 {
		t.Errorf("SchemaVersion = %d, want 1", m.SchemaVersion)
	}
	if m.Dir == "" {
		t.Error("Dir was not set by Load")
	}
	if len(m.Wallpapers.Paths) != 2 {
		t.Errorf("Wallpapers.Paths len = %d, want 2", len(m.Wallpapers.Paths))
	}
	if m.Palette.Accent != "#ff2a4b" {
		t.Errorf("Palette.Accent = %q, want #ff2a4b", m.Palette.Accent)
	}
}

func TestLoad_MissingDir(t *testing.T) {
	if _, err := manifest.Load("testdata/does-not-exist"); err == nil {
		t.Fatal("Load should have failed on missing dir")
	}
}

func TestLoad_NotADir(t *testing.T) {
	// Pointing Load at a file rather than a directory must fail cleanly.
	if _, err := manifest.Load("testdata/valid/theme.toml"); err == nil {
		t.Fatal("Load should have failed on a file path")
	}
}

func TestValidate_Valid(t *testing.T) {
	m, err := manifest.Load("testdata/valid")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := m.Validate(); err != nil {
		t.Errorf("Validate on valid manifest returned error: %v", err)
	}
}

func TestValidate_Invalid(t *testing.T) {
	cases := []struct {
		name      string
		dir       string
		wantField string // a substring expected to appear in the error
	}{
		{"bad schema version", "testdata/bad_schema", "schema_version"},
		{"bad hex color", "testdata/bad_color", "palette.bg"},
		{"missing wallpaper file", "testdata/missing_wallpaper", "wallpapers.paths[0]"},
		{"unknown enum values", "testdata/bad_enum", "wallpapers.mode"},
		{"non-kebab slug", "testdata/bad_slug", "meta.slug"},
		{"missing required palette fields", "testdata/missing_required", "palette.bg"},
		{"path traversal in wallpapers", "testdata/bad_path_traversal", "escapes the theme directory"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, err := manifest.Load(tc.dir)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			err = m.Validate()
			if err == nil {
				t.Fatal("expected Validate to fail, got nil")
			}
			var verr *manifest.ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("expected *manifest.ValidationError, got %T: %v", err, err)
			}
			if !strings.Contains(err.Error(), tc.wantField) {
				t.Errorf("error %q does not mention field %q", err.Error(), tc.wantField)
			}
		})
	}
}

// TestValidate_WallpapersScreensExclusivity checks the robustness rule
// the user explicitly asked for: a manifest cannot mix mirror layouts
// (top-level paths OR mirror=true) with per-screen lists. The two are
// distinct mental models and combining them invites silent surprise.
func TestValidate_WallpapersScreensExclusivity(t *testing.T) {
	cases := []struct {
		name      string
		m         manifest.Manifest
		wantField string
	}{
		{
			name: "paths + screens rejected",
			m: manifest.Manifest{
				SchemaVersion: 1,
				Meta:          manifest.Meta{Name: "x", Slug: "x", Mode: "dark"},
				Palette:       manifest.Palette{BG: "#000000", Surface: "#111111", Text: "#ffffff", Accent: "#ff2a4b"},
				Wallpapers: manifest.Wallpapers{
					Mode:     "slideshow",
					Interval: 600,
					Paths:    []string{"a.png"},
					Screens: []manifest.WallpapersScreen{
						{Index: intPtr(0), Paths: []string{"b.png"}},
					},
				},
			},
			wantField: "wallpapers.paths",
		},
		{
			name: "mirror + screens rejected",
			m: manifest.Manifest{
				SchemaVersion: 1,
				Meta:          manifest.Meta{Name: "x", Slug: "x", Mode: "dark"},
				Palette:       manifest.Palette{BG: "#000000", Surface: "#111111", Text: "#ffffff", Accent: "#ff2a4b"},
				Wallpapers: manifest.Wallpapers{
					Mode:     "slideshow",
					Interval: 600,
					Mirror:   true,
					Screens: []manifest.WallpapersScreen{
						{Index: intPtr(0), Paths: []string{"b.png"}},
					},
				},
			},
			wantField: "wallpapers.mirror",
		},
		{
			// 0.1.3 V2: mode=single is incompatible with [[screens]] --
			// the planner's per-screen JS sets wallpaperPlugin=slideshow,
			// so mixing the two would silently misbehave.
			name: "single mode + screens rejected",
			m: manifest.Manifest{
				SchemaVersion: 1,
				Meta:          manifest.Meta{Name: "x", Slug: "x", Mode: "dark"},
				Palette:       manifest.Palette{BG: "#000000", Surface: "#111111", Text: "#ffffff", Accent: "#ff2a4b"},
				Wallpapers: manifest.Wallpapers{
					Mode: "single",
					Screens: []manifest.WallpapersScreen{
						{Index: intPtr(0), Paths: []string{"a.png"}},
					},
				},
			},
			wantField: "wallpapers.screens",
		},
		{
			name: "duplicate screen index rejected",
			m: manifest.Manifest{
				SchemaVersion: 1,
				Meta:          manifest.Meta{Name: "x", Slug: "x", Mode: "dark"},
				Palette:       manifest.Palette{BG: "#000000", Surface: "#111111", Text: "#ffffff", Accent: "#ff2a4b"},
				Wallpapers: manifest.Wallpapers{
					Mode:     "slideshow",
					Interval: 600,
					Screens: []manifest.WallpapersScreen{
						{Index: intPtr(0), Paths: []string{"a.png"}},
						{Index: intPtr(0), Paths: []string{"b.png"}},
					},
				},
			},
			wantField: "wallpapers.screens[1].index",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.m.Validate()
			var verr *manifest.ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("expected ValidationError, got %T: %v", err, err)
			}
			var found bool
			for _, iss := range verr.Issues {
				if strings.Contains(iss.Field, tc.wantField) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("no issue mentioned %q\nissues: %v", tc.wantField, verr.Issues)
			}
		})
	}
}

func intPtr(v int) *int { return &v }

// TestValidate_LookSHA256Hex covers the 0.1.3 V1 fix: looks.sha256 must
// be a 64-char hex string. Pre-fix the validator only checked length,
// so "g" * 64 or "abc" would slip through and only fail much later at
// download time. We exercise the common typos.
func TestValidate_LookSHA256Hex(t *testing.T) {
	base := manifest.Manifest{
		SchemaVersion: 1,
		Meta:          manifest.Meta{Name: "x", Slug: "x", Mode: "dark"},
		Palette:       manifest.Palette{BG: "#000000", Surface: "#111111", Text: "#ffffff", Accent: "#ff2a4b"},
		Wallpapers: manifest.Wallpapers{
			Mode:  "single",
			Paths: []string{}, // satisfy required-at-least-one via screens... actually we leave invalid + filter
		},
	}
	// Drop the wallpapers requirement by giving it one entry that doesn't
	// need to resolve on disk: an absolute path that won't be checked by
	// checkRelFileExists -- it's relative-checked, so we can't easily.
	// Instead, accept the wallpapers issue and just check the sha256
	// issue is also surfaced (Validate accumulates).
	cases := []struct {
		name   string
		sha256 string
		want   bool // expect a sha256 error
	}{
		{"valid 64-char hex lower", strings.Repeat("a", 64), false},
		{"valid 64-char hex upper", strings.Repeat("A", 64), false},
		{"valid 64-char hex mixed", strings.Repeat("aF", 32), false},
		{"too short", strings.Repeat("a", 32), true},
		{"too long", strings.Repeat("a", 80), true},
		{"non-hex chars", strings.Repeat("g", 64), true},
		{"contains spaces", strings.Repeat("a", 63) + " ", true},
		{"empty", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := base
			m.Looks = []manifest.Look{{
				Name:   "tela",
				Type:   "icons",
				URL:    "https://example.test/x.tar.gz",
				SHA256: tc.sha256,
			}}
			err := m.Validate()
			var verr *manifest.ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("Validate returned %T, want *ValidationError: %v", err, err)
			}
			var hasShaIssue bool
			for _, iss := range verr.Issues {
				if strings.Contains(iss.Field, ".sha256") {
					hasShaIssue = true
					break
				}
			}
			if hasShaIssue != tc.want {
				t.Errorf("sha256 issue present = %v, want %v\nissues: %v", hasShaIssue, tc.want, verr.Issues)
			}
		})
	}
}

func TestValidate_AccumulatesIssues(t *testing.T) {
	// bad_enum has multiple invalid enums; Validate should surface several
	// issues rather than stopping at the first.
	m, err := manifest.Load("testdata/bad_enum")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	err = m.Validate()
	var verr *manifest.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("expected ValidationError, got %T", err)
	}
	if len(verr.Issues) < 4 {
		t.Errorf("expected >=4 issues, got %d: %v", len(verr.Issues), verr.Issues)
	}
}
