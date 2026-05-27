// Package manifest defines the theme.toml schema (the contract between Riced
// and any theme repository) and provides parsing + validation.
package manifest

import "fmt"

// CurrentSchemaVersion is the only schema version Riced understands today.
// Future breaking changes bump this; minor additive fields stay on the same
// version.
const CurrentSchemaVersion = 1

// Manifest is the in-memory representation of a theme.toml file.
//
// Field paths in the manifest that reference files on disk (wallpapers,
// launcher icon) are interpreted relative to the directory containing
// theme.toml. That directory is stored in Dir after a successful Load.
type Manifest struct {
	SchemaVersion int         `toml:"schema_version"`
	Meta          Meta        `toml:"meta"`
	Palette       Palette     `toml:"palette"`
	Wallpapers    Wallpapers  `toml:"wallpapers"`
	Panel         Panel       `toml:"panel"`
	Window        Window      `toml:"window"`
	Fonts         Fonts       `toml:"fonts"`
	Terminal      Terminal    `toml:"terminal"`
	Icons         Icons       `toml:"icons"`
	Cursors       Cursors     `toml:"cursors"`
	Plasma        Plasma      `toml:"plasma"`
	LookAndFeel   LookAndFeel `toml:"lookandfeel"`
	Looks         []Look      `toml:"looks"`

	// Dir is the absolute path of the directory containing theme.toml.
	// Set by Load; not serialized.
	Dir string `toml:"-"`

	// unknownKeys collects keys present in the TOML file but not in this
	// schema. Populated by Load; surfaced as soft issues by Validate.
	unknownKeys []string
}

type Meta struct {
	Name   string `toml:"name"`
	Slug   string `toml:"slug"`
	Mode   string `toml:"mode"`  // "dark" | "light" -- optional
	Theme  string `toml:"theme"` // optional design-family identifier for UI grouping (e.g. "red")
	Author string `toml:"author"`

	// Inherits, when set, is the slug of another theme whose fields are
	// merged in beneath this one. Resolved by manifest.ResolveDir, which
	// walks the chain with cycle detection.
	Inherits string `toml:"inherits"`
}

// Palette holds the named colors. All values are hex strings, either
// "#RRGGBB" or "#RRGGBBAA". Required: BG, Text, Accent.
type Palette struct {
	BG      string `toml:"bg"`
	Surface string `toml:"surface"`
	Text    string `toml:"text"`
	Subtext string `toml:"subtext"`
	Muted   string `toml:"muted"`
	Accent  string `toml:"accent"`
	Accent2 string `toml:"accent2"`
	Accent3 string `toml:"accent3"`
	Warning string `toml:"warning"`
	Error   string `toml:"error"`
}

type Wallpapers struct {
	Mode     string   `toml:"mode"`     // "single" | "slideshow"
	Interval int      `toml:"interval"` // seconds; only used when Mode == "slideshow"
	Paths    []string `toml:"paths"`    // relative to the theme directory
	// LockImage is the wallpaper shown on the lock screen. Relative to
	// the theme directory; absolute path also accepted. Optional: when
	// empty, the lock screen keeps whatever Plasma had configured.
	LockImage string `toml:"lock_image"`
	// Mirror, when true, sends the top-level Paths to every screen
	// individually. Default (false) keeps the legacy behavior of one
	// shared slideshow directory cycled independently per screen.
	Mirror bool `toml:"mirror"`
	// Screens, when non-empty, overrides the top-level Paths with a
	// per-screen distribution. Each entry binds an explicit screen
	// index (Plasma's desktops()[i].screen) to its own image list.
	// Screens whose index isn't listed fall back to the top-level
	// Paths (or get no slideshow at all when Paths is also empty).
	Screens []WallpapersScreen `toml:"screens"`
}

// WallpapersScreen is one screen entry inside [[wallpapers.screens]].
//
// Exactly one matching criterion must be set per entry:
//
//   - Index pins to a specific Plasma screen number (0-based, matches
//     desktop.screen). Most specific.
//   - Orientation matches any screen whose width/height ratio is "vertical"
//     or "horizontal" at apply time (geometry from screenGeometry()).
//   - Match = "*" is a catch-all for screens not claimed by an earlier rule.
//
// Matching order is the manifest order: first rule that fits a desktop
// wins (CSS-like cascade). Validation enforces the exclusivity rule.
type WallpapersScreen struct {
	// Index pinned to a specific Plasma screen (0-based). Pointer so
	// "not set" is distinct from "explicitly 0".
	Index *int `toml:"index"`
	// Orientation: "vertical" | "horizontal" (geometry detected at runtime).
	Orientation string `toml:"orientation"`
	// Match: "*" = fallback for any screen not claimed earlier.
	Match string `toml:"match"`
	// Paths is the curated wallpaper list for screens this rule claims.
	Paths []string `toml:"paths"`
}

var (
	// AllowedScreenOrientations enumerates the values accepted by
	// [[wallpapers.screens]] orientation. Kept here so validators and
	// docs share one source.
	AllowedScreenOrientations = []string{"vertical", "horizontal"}
	// AllowedScreenMatches enumerates the values accepted by
	// [[wallpapers.screens]] match. Currently only the catch-all "*";
	// future values (e.g. connector names) would land here.
	AllowedScreenMatches = []string{"*"}
)

// SubdirName returns the deterministic subdirectory name a per-screen
// rule materializes into under <slug>/wallpapers/. Both the generator
// (creates the dir) and the planner (emits the action with that path)
// call this so they stay in sync without sharing extra state.
func (s WallpapersScreen) SubdirName() string {
	switch {
	case s.Index != nil:
		return fmt.Sprintf("screen-%d", *s.Index)
	case s.Orientation != "":
		return s.Orientation // "vertical" | "horizontal"
	case s.Match == "*":
		return "default"
	}
	return "unknown" // unreachable when validated
}

// CriterionKind reports which match field is populated. Used by the
// planner to encode the rule into the apply Action's Args.
func (s WallpapersScreen) CriterionKind() (kind, value string) {
	switch {
	case s.Index != nil:
		return "idx", fmt.Sprintf("%d", *s.Index)
	case s.Orientation != "":
		return "orient", s.Orientation
	case s.Match != "":
		return "match", s.Match
	}
	return "", ""
}

type Panel struct {
	Position     string `toml:"position"` // "top" | "bottom" | "left" | "right"
	Floating     bool   `toml:"floating"`
	Height       int    `toml:"height"`
	LauncherIcon string `toml:"launcher_icon"` // relative to the theme directory
}

type Window struct {
	Decoration string `toml:"decoration"` // "klassy" | "breeze"
	Animations string `toml:"animations"` // "magic-lamp" | "scale" | "glide" | "fade" | "none"
	// AnimationSpeed maps to kwinrc [Compositing] AnimationSpeed (the
	// Plasma slider). Range 0-6, where 0=instant, 3=normal (KDE default),
	// 6=very slow. nil = not specified, renderer leaves the user's
	// existing setting alone.
	AnimationSpeed *int `toml:"animation_speed"`
	// AnimationDurationMS overrides the per-effect AnimationDuration in
	// milliseconds, bypassing the [Compositing] slider cap (~1s at speed 6).
	// Written to [Effect-<animations>] AnimationDuration in kwinrc when
	// Animations names a concrete effect. nil = leave it alone.
	AnimationDurationMS *int `toml:"animation_duration_ms"`
	Blur                bool `toml:"blur"`
	Wobbly              bool `toml:"wobbly"`
}

type Fonts struct {
	UI   string `toml:"ui"`
	Mono string `toml:"mono"`
}

type Terminal struct {
	Opacity float64 `toml:"opacity"` // 0.0 - 1.0
}

// Icons selects the system-wide icon theme. The value is the theme's
// directory name as installed under /usr/share/icons/ or
// ~/.local/share/icons/. Applied via `kwriteconfig6 kdeglobals Icons Theme`.
type Icons struct {
	Theme string `toml:"theme"`
}

// Cursors selects the system-wide cursor theme. The value is the theme's
// directory name (e.g. "capitaine-cursors"). Applied via
// `plasma-apply-cursortheme <name>`.
type Cursors struct {
	Theme string `toml:"theme"`
}

// Plasma carries Plasma-specific knobs that don't fit anywhere else.
// DesktopTheme is the "Plasma Style" name (panel widgets, popups,
// notifications). Applied via `plasma-apply-desktoptheme <name>`.
type Plasma struct {
	DesktopTheme string `toml:"desktop_theme"`
}

// LookAndFeel switches Plasma's "Global Theme" -- a meta-package that
// resets colorscheme, cursor, decoration, plasma theme and icons in one
// shot. Riced applies it BEFORE the individual overrides so the
// theme-specific fields can win over the look-and-feel defaults.
// Package is the LookAndFeel package id (e.g. "org.kde.breezedark.desktop"),
// as listed by `kpackagetool6 -t Plasma/LookAndFeel --list`.
type LookAndFeel struct {
	Package string `toml:"package"`
}

// Look describes a KDE theme asset Riced should install into the user's
// data dir before applying the rest of the manifest. Covers community
// packages from store.kde.org, github releases, or user-managed local
// clones under ~/.riced/looks/.
//
// Source: exactly one of URL or Local.
//
//	URL points to an HTTPS-hosted .tar.gz / .tar.xz / .zip archive.
//	SHA256 of that archive is mandatory (integrity check before install).
//
//	Local is a kebab-case name under ~/.riced/looks/<name>/ that the user
//	has already populated (typically `git clone`). No sha256 — trust is the
//	user's local checkout, same model as `riced repo add`.
//
// Type matches kpackagetool6's structure names ("Plasma/LookAndFeel",
// "Plasma/Theme", "KWin/Decoration", "KWin/Aurorae") plus "icons" and
// "cursors" which land directly under ~/.local/share/icons/.
//
// Install picks the install strategy. "auto" inspects the source and
// routes to one of the explicit strategies. Riced always *normalizes*
// the source first: if the archive (or local dir) has a single
// top-level subdirectory (github-source layout), Riced descends into
// it so paths in Script are relative to the actual theme root.
//
//	"kpackage" — kpackagetool6 -t <type> -i <normalized-source>
//	"extract"  — copy contents into ~/.local/share/icons/<basename>/
//	"script"   — exec /bin/bash <normalized-source>/<Script> <Args...>
//
// When Install = "script", Script is a relative path under the
// (normalized) source (no .. or absolute) and Args supports
// $HOME / $XDG_DATA_HOME / $XDG_CONFIG_HOME expansion (Go-side, no
// shell). The exact command lands in the apply plan for explicit user
// consent. "auto" will NEVER pick "script" silently — it asks the user
// to set Install = "script" explicitly.
type Look struct {
	Name    string   `toml:"name"`
	Type    string   `toml:"type"`
	URL     string   `toml:"url"`
	SHA256  string   `toml:"sha256"`
	Local   string   `toml:"local"`
	Install string   `toml:"install"`
	Script  string   `toml:"script"`
	Args    []string `toml:"args"`
}

// AllowedLookTypes enumerates the kpackagetool6 structure names plus the
// two special-cases handled by direct extract (icons, cursors).
var AllowedLookTypes = []string{
	"Plasma/LookAndFeel",
	"Plasma/Theme",
	"Plasma/Wallpaper",
	"KWin/Decoration",
	"KWin/Aurorae",
	"KWin/Effect",
	"KWin/Script",
	"icons",
	"cursors",
}

// AllowedLookInstallStrategies enumerates the install: values. "auto" is
// the default and lets Riced choose by inspecting the source.
var AllowedLookInstallStrategies = []string{
	"auto",
	"kpackage",
	"extract",
	"script",
}

// Allowed enum values, kept here so validators and docs share one source.
var (
	AllowedModes          = []string{"dark", "light"}
	AllowedWallpaperModes = []string{"single", "slideshow"}
	AllowedPanelPositions = []string{"top", "bottom", "left", "right"}
	AllowedDecorations    = []string{"klassy", "breeze"}
	AllowedAnimations     = []string{"magic-lamp", "scale", "glide", "fade", "none"}
)
