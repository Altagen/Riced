package render

import (
	"strconv"
	"strings"

	"github.com/Altagen/Riced/internal/manifest"
)

// INIEntry is a single (file, group, key, value) tuple the apply layer
// will push into a KDE config file via kwriteconfig6. The renderer
// emits a flat list -- the executor turns each entry into one kde
// Action. Keeping the contract this simple is what lets the renderer
// stay testable without filesystem access.
type INIEntry struct {
	File  string // basename, e.g. "kwinrc"
	Group string // INI group name, e.g. "Plugins"
	Key   string // INI key
	Value string // INI value (kwriteconfig6 accepts everything as string)
}

// KWinSettings returns the kwinrc settings that follow from a manifest's
// [window] section.
//
// Mapping:
//
//	window.decoration → org.kde.kdecoration2 / library (recognized values:
//	                    "klassy" → "org.kde.klassy", "breeze" → "org.kde.breeze")
//	window.blur       → Plugins / blurEnabled        (bool)
//	window.wobbly     → Plugins / wobblywindowsEnabled (bool)
//	window.animations → Plugins / <effect>Enabled    (toggle the named effect)
//
// Each call returns a fresh slice -- callers can mutate freely.
func KWinSettings(m *manifest.Manifest) []INIEntry {
	var out []INIEntry

	// Window decoration library. Three syntaxes supported:
	//
	//   - Bare names: "klassy", "breeze", "oxygen" -> the matching native
	//     C++ KWin decoration library (org.kde.<name>).
	//   - "aurorae:<theme-name>" -> the aurorae SVG engine
	//     (library=org.kde.kwin.aurorae) with the magic-prefixed theme key
	//     KWin expects (__aurorae__svg__<theme-name>). The theme dir must
	//     exist under ~/.local/share/aurorae/themes/ or /usr/share/aurorae/themes/.
	//   - "library:org.kde.<x>" -> escape hatch for unknown C++ decorations
	//     installed by hand. Passed through verbatim; no theme key written.
	//
	// Validation (in internal/manifest) catches malformed values before
	// they reach this point; unknown values are skipped silently so a
	// future schema addition doesn't crash the renderer first.
	libEntry, themeEntry, ok := resolveDecoration(m.Window.Decoration)
	if ok {
		out = append(out, libEntry)
		if themeEntry.Key != "" {
			out = append(out, themeEntry)
		}
	}

	// Blur + wobbly are simple toggles in [Plugins].
	out = append(out, INIEntry{
		File: "kwinrc", Group: "Plugins",
		Key: "blurEnabled", Value: boolStr(m.Window.Blur),
	})
	out = append(out, INIEntry{
		File: "kwinrc", Group: "Plugins",
		Key: "wobblywindowsEnabled", Value: boolStr(m.Window.Wobbly),
	})

	// Minimize/restore animation. Only one of the named animations is on
	// at a time -- the others must be explicitly off so switching themes
	// doesn't leave two effects fighting.
	animations := map[string]string{
		"magic-lamp": "magiclampEnabled",
		"scale":      "scaleEnabled",
		"glide":      "glideEnabled",
		"fade":       "fadeEnabled",
	}
	wantedKey := animations[m.Window.Animations] // "" for "none" or unknown
	for _, key := range animations {
		out = append(out, INIEntry{
			File: "kwinrc", Group: "Plugins",
			Key:   key,
			Value: boolStr(key == wantedKey),
		})
	}

	// Compositing slider speed. Only written when the manifest sets it --
	// leaving the user's existing kwinrc value alone when unspecified.
	if m.Window.AnimationSpeed != nil {
		out = append(out, INIEntry{
			File: "kwinrc", Group: "Compositing",
			Key: "AnimationSpeed", Value: strconv.Itoa(*m.Window.AnimationSpeed),
		})
	}

	// Per-effect duration override. Overrides the [Compositing]
	// AnimationSpeed slider for the named effect only. Useful when the
	// slider cap (~1s) isn't slow enough -- magic-lamp users often want
	// 1500-2500 ms for the effect to register visually.
	if m.Window.AnimationDurationMS != nil {
		effectGroup := map[string]string{
			"magic-lamp": "Effect-magiclamp",
			"scale":      "Effect-scale",
			"glide":      "Effect-glide",
			"fade":       "Effect-fade",
		}[m.Window.Animations]
		if effectGroup != "" {
			out = append(out, INIEntry{
				File: "kwinrc", Group: effectGroup,
				Key: "AnimationDuration", Value: strconv.Itoa(*m.Window.AnimationDurationMS),
			})
		}
	}

	return out
}

// KlassySettings returns klassyrc entries when the manifest opts into
// klassy as its window decoration library. Empty slice otherwise.
//
// We keep this minimal in v1: just a sensible corner radius. Klassy has
// dozens of options; we'll grow this as users ask for control. The
// per-decoration colors come from kdeglobals (which we already write),
// so the visual result is already on-theme without touching klassyrc.
func KlassySettings(m *manifest.Manifest) []INIEntry {
	if m.Window.Decoration != "klassy" {
		return nil
	}
	return []INIEntry{
		{File: "klassyrc", Group: "Common", Key: "CornerRadius", Value: "8"},
	}
}

// KvantumSettings returns the kvantum.kvconfig entries that switch
// Kvantum's active Qt theme to a manifest-declared value. Phase 12 is
// minimal on purpose: Kvantum themes ship their own SVG widget graphics,
// so Riced cannot synthesize one -- it can only pick from what the user
// has installed via kvantummanager.
//
// Today the choice is hard-coded to "KvLibadwaita-dark" / "KvLibadwaita"
// (depending on Meta.Mode) because those are bundled in most distros and
// they respect the system color palette. A future manifest field
// (`qt.kvantum`) could let the user override this.
//
// Returns nil when the manifest has no [palette] (no theming intent),
// to avoid clobbering a Kvantum config the user may have set by hand.
func KvantumSettings(m *manifest.Manifest) []INIEntry {
	if m.Palette.BG == "" {
		return nil
	}
	theme := "KvLibadwaita-dark"
	if m.Meta.Mode == "light" {
		theme = "KvLibadwaita"
	}
	return []INIEntry{
		{File: "Kvantum/kvantum.kvconfig", Group: "General", Key: "theme", Value: theme},
	}
}

// boolStr renders a Go bool the way kwriteconfig6 expects (lowercase).
func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// resolveDecoration turns the manifest's [window].decoration value into
// the (library, theme) INI tuple KWin needs. theme entry is zero when
// the decoration is library-only (the C++ libs do not use the theme key).
// ok reports whether the value was recognized -- callers should skip the
// emission when ok is false (it's an unknown value the validator missed,
// or an empty string).
func resolveDecoration(decoration string) (lib INIEntry, theme INIEntry, ok bool) {
	group := "org.kde.kdecoration2"
	switch decoration {
	case "":
		return INIEntry{}, INIEntry{}, false
	case "klassy":
		return INIEntry{File: "kwinrc", Group: group, Key: "library", Value: "org.kde.klassy"}, INIEntry{}, true
	case "breeze":
		return INIEntry{File: "kwinrc", Group: group, Key: "library", Value: "org.kde.breeze"}, INIEntry{}, true
	case "oxygen":
		return INIEntry{File: "kwinrc", Group: group, Key: "library", Value: "org.kde.oxygen"}, INIEntry{}, true
	}
	if name, ok := strings.CutPrefix(decoration, "aurorae:"); ok {
		return INIEntry{File: "kwinrc", Group: group, Key: "library", Value: "org.kde.kwin.aurorae"},
			INIEntry{File: "kwinrc", Group: group, Key: "theme", Value: "__aurorae__svg__" + name},
			true
	}
	if lib, ok := strings.CutPrefix(decoration, "library:"); ok {
		return INIEntry{File: "kwinrc", Group: group, Key: "library", Value: lib}, INIEntry{}, true
	}
	return INIEntry{}, INIEntry{}, false
}
