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
	SchemaVersion int        `toml:"schema_version"`
	Meta          Meta       `toml:"meta"`
	Palette       Palette    `toml:"palette"`
	Wallpapers    Wallpapers `toml:"wallpapers"`
	Panel         Panel      `toml:"panel"`
	Window        Window     `toml:"window"`
	Fonts         Fonts      `toml:"fonts"`
	Terminal      Terminal   `toml:"terminal"`

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

// Allowed enum values, kept here so validators and docs share one source.
var (
	AllowedModes          = []string{"dark", "light"}
	AllowedWallpaperModes = []string{"single", "slideshow"}
	AllowedPanelPositions = []string{"top", "bottom", "left", "right"}
	AllowedDecorations    = []string{"klassy", "breeze"}
	AllowedAnimations     = []string{"magic-lamp", "scale", "glide", "fade", "none"}
)
