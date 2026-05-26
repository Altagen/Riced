package render_test

import (
	"testing"

	"github.com/Altagen/Riced/internal/manifest"
	"github.com/Altagen/Riced/internal/render"
)

func TestKWinSettings_ManifestDriven(t *testing.T) {
	m := &manifest.Manifest{
		Window: manifest.Window{
			Decoration: "klassy",
			Animations: "magic-lamp",
			Blur:       true,
			Wobbly:     false,
		},
	}
	entries := render.KWinSettings(m)

	want := map[string]string{
		"kwinrc/[org.kde.kdecoration2]/library": "org.kde.klassy",
		"kwinrc/[Plugins]/blurEnabled":          "true",
		"kwinrc/[Plugins]/wobblywindowsEnabled": "false",
		"kwinrc/[Plugins]/magiclampEnabled":     "true",
		"kwinrc/[Plugins]/scaleEnabled":         "false",
		"kwinrc/[Plugins]/glideEnabled":         "false",
		"kwinrc/[Plugins]/fadeEnabled":          "false",
	}
	got := map[string]string{}
	for _, e := range entries {
		got[e.File+"/["+e.Group+"]/"+e.Key] = e.Value
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("entry %s = %q, want %q", k, got[k], v)
		}
	}
}

func TestKWinSettings_BreezeDecoration(t *testing.T) {
	m := &manifest.Manifest{Window: manifest.Window{Decoration: "breeze"}}
	entries := render.KWinSettings(m)
	var lib string
	for _, e := range entries {
		if e.Group == "org.kde.kdecoration2" && e.Key == "library" {
			lib = e.Value
		}
	}
	if lib != "org.kde.breeze" {
		t.Errorf("library = %q, want org.kde.breeze", lib)
	}
}

func TestKvantumSettings_RespectMode(t *testing.T) {
	dark := &manifest.Manifest{
		Meta:    manifest.Meta{Mode: "dark"},
		Palette: manifest.Palette{BG: "#000000"},
	}
	light := &manifest.Manifest{
		Meta:    manifest.Meta{Mode: "light"},
		Palette: manifest.Palette{BG: "#ffffff"},
	}
	noPalette := &manifest.Manifest{}

	if got := render.KvantumSettings(dark); len(got) != 1 || got[0].Value != "KvLibadwaita-dark" {
		t.Errorf("dark Kvantum theme = %+v, want KvLibadwaita-dark", got)
	}
	if got := render.KvantumSettings(light); len(got) != 1 || got[0].Value != "KvLibadwaita" {
		t.Errorf("light Kvantum theme = %+v, want KvLibadwaita", got)
	}
	if got := render.KvantumSettings(noPalette); len(got) != 0 {
		t.Errorf("no-palette manifest must produce 0 Kvantum entries, got %d", len(got))
	}
}

func TestKlassySettings_OnlyWhenKlassy(t *testing.T) {
	m := &manifest.Manifest{Window: manifest.Window{Decoration: "breeze"}}
	if got := render.KlassySettings(m); len(got) != 0 {
		t.Errorf("breeze decoration should produce 0 klassy entries, got %d", len(got))
	}

	m.Window.Decoration = "klassy"
	if got := render.KlassySettings(m); len(got) == 0 {
		t.Errorf("klassy decoration should produce >=1 klassy entry, got 0")
	}
}
