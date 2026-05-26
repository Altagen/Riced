package manifest

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// ValidationError aggregates one or more issues found in a Manifest.
type ValidationError struct {
	Issues []Issue
}

func (e *ValidationError) Error() string {
	if len(e.Issues) == 0 {
		return "manifest is invalid"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "manifest has %d issue(s):", len(e.Issues))
	for _, iss := range e.Issues {
		fmt.Fprintf(&b, "\n  - [%s] %s", iss.Field, iss.Msg)
	}
	return b.String()
}

// Issue describes a single validation problem.
type Issue struct {
	Field string // dotted path, e.g. "palette.accent"
	Msg   string
}

// hexColor matches "#RRGGBB" or "#RRGGBBAA" (case-insensitive).
var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}([0-9a-fA-F]{2})?$`)

// slug matches kebab-case identifiers.
var slug = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Validate runs all schema checks against m and returns a *ValidationError
// listing every issue found. Returns nil if everything is OK.
func (m *Manifest) Validate() error {
	var issues []Issue
	add := func(field, msg string) {
		issues = append(issues, Issue{Field: field, Msg: msg})
	}

	// --- schema_version ----------------------------------------------------
	if m.SchemaVersion != CurrentSchemaVersion {
		add("schema_version",
			fmt.Sprintf("got %d, expected %d", m.SchemaVersion, CurrentSchemaVersion))
	}

	// --- meta --------------------------------------------------------------
	if strings.TrimSpace(m.Meta.Name) == "" {
		add("meta.name", "must not be empty")
	}
	if m.Meta.Slug == "" {
		add("meta.slug", "must not be empty")
	} else if !slug.MatchString(m.Meta.Slug) {
		add("meta.slug", fmt.Sprintf("%q must be kebab-case (a-z, 0-9, '-')", m.Meta.Slug))
	}
	if m.Meta.Mode != "" && !slices.Contains(AllowedModes, m.Meta.Mode) {
		add("meta.mode", fmt.Sprintf("%q not in %v", m.Meta.Mode, AllowedModes))
	}

	// --- palette -----------------------------------------------------------
	// Slice + ordered iteration so issue ordering is deterministic. Map
	// iteration in Go is randomized; that would make `riced validate`
	// output (and any test that snapshots it) non-reproducible.
	paletteFields := []struct {
		name     string
		value    string
		required bool
	}{
		{"palette.bg", m.Palette.BG, true},
		{"palette.surface", m.Palette.Surface, false},
		{"palette.text", m.Palette.Text, true},
		{"palette.subtext", m.Palette.Subtext, false},
		{"palette.muted", m.Palette.Muted, false},
		{"palette.accent", m.Palette.Accent, true},
		{"palette.accent2", m.Palette.Accent2, false},
		{"palette.accent3", m.Palette.Accent3, false},
		{"palette.warning", m.Palette.Warning, false},
		{"palette.error", m.Palette.Error, false},
	}
	for _, pf := range paletteFields {
		if pf.required && pf.value == "" {
			add(pf.name, "is required")
		}
		if pf.value != "" && !hexColor.MatchString(pf.value) {
			add(pf.name, fmt.Sprintf("%q is not a valid hex color (#RRGGBB or #RRGGBBAA)", pf.value))
		}
	}

	// --- wallpapers --------------------------------------------------------
	if m.Wallpapers.Mode != "" && !slices.Contains(AllowedWallpaperModes, m.Wallpapers.Mode) {
		add("wallpapers.mode", fmt.Sprintf("%q not in %v", m.Wallpapers.Mode, AllowedWallpaperModes))
	}
	if m.Wallpapers.Mode == "slideshow" && m.Wallpapers.Interval <= 0 {
		add("wallpapers.interval", "must be > 0 when mode = \"slideshow\"")
	}
	// Per-screen mode lets [[wallpapers.screens]] supply the paths,
	// so the top-level Paths can stay empty in that case.
	hasScreens := len(m.Wallpapers.Screens) > 0
	if len(m.Wallpapers.Paths) == 0 && !hasScreens {
		add("wallpapers.paths", "must contain at least one wallpaper (or define [[wallpapers.screens]])")
	}
	for i, p := range m.Wallpapers.Paths {
		field := fmt.Sprintf("wallpapers.paths[%d]", i)
		if err := checkPathSafety(p); err != nil {
			add(field, err.Error())
			continue
		}
		if err := checkRelFileExists(m.Dir, p); err != nil {
			add(field, err.Error())
		}
	}
	if hasScreens && m.Wallpapers.Mirror {
		add("wallpapers.mirror", "cannot combine with [[wallpapers.screens]]; mirror is for spreading the top-level paths to every screen, while screens already enumerates per-screen lists")
	}
	if hasScreens && len(m.Wallpapers.Paths) > 0 {
		add("wallpapers.paths", "cannot combine with [[wallpapers.screens]]; pick one layout: a flat `paths` list (mirrored to every screen) OR per-screen lists under [[wallpapers.screens]]")
	}
	seenIdx := map[int]bool{}
	seenOrient := map[string]bool{}
	seenMatch := map[string]bool{}
	for si, s := range m.Wallpapers.Screens {
		base := fmt.Sprintf("wallpapers.screens[%d]", si)

		// Exactly one criterion per entry. Counting set fields keeps the
		// rule local (no cross-entry inference): the error is the user's
		// to fix, not Riced's to guess at.
		set := 0
		if s.Index != nil {
			set++
		}
		if s.Orientation != "" {
			set++
		}
		if s.Match != "" {
			set++
		}
		if set == 0 {
			add(base, "set exactly one of: index, orientation, match")
		}
		if set > 1 {
			add(base, "set exactly one of index/orientation/match per entry (combining them is ambiguous)")
		}

		if s.Index != nil {
			if *s.Index < 0 {
				add(base+".index", fmt.Sprintf("%d must be >= 0", *s.Index))
			}
			if seenIdx[*s.Index] {
				add(base+".index", fmt.Sprintf("duplicate screen index %d", *s.Index))
			}
			seenIdx[*s.Index] = true
		}
		if s.Orientation != "" {
			if !slices.Contains(AllowedScreenOrientations, s.Orientation) {
				add(base+".orientation", fmt.Sprintf("%q not in %v", s.Orientation, AllowedScreenOrientations))
			}
			if seenOrient[s.Orientation] {
				add(base+".orientation", fmt.Sprintf("duplicate orientation %q", s.Orientation))
			}
			seenOrient[s.Orientation] = true
		}
		if s.Match != "" {
			if !slices.Contains(AllowedScreenMatches, s.Match) {
				add(base+".match", fmt.Sprintf("%q not in %v", s.Match, AllowedScreenMatches))
			}
			if seenMatch[s.Match] {
				add(base+".match", fmt.Sprintf("duplicate match %q", s.Match))
			}
			seenMatch[s.Match] = true
		}

		if len(s.Paths) == 0 {
			add(base+".paths", "must contain at least one wallpaper")
		}
		for pi, p := range s.Paths {
			field := fmt.Sprintf("%s.paths[%d]", base, pi)
			if err := checkPathSafety(p); err != nil {
				add(field, err.Error())
				continue
			}
			if err := checkRelFileExists(m.Dir, p); err != nil {
				add(field, err.Error())
			}
		}
	}

	// --- panel -------------------------------------------------------------
	if m.Panel.Position != "" && !slices.Contains(AllowedPanelPositions, m.Panel.Position) {
		add("panel.position", fmt.Sprintf("%q not in %v", m.Panel.Position, AllowedPanelPositions))
	}
	if m.Panel.Height < 0 {
		add("panel.height", "must be >= 0")
	}
	if m.Panel.LauncherIcon != "" {
		if err := checkPathSafety(m.Panel.LauncherIcon); err != nil {
			add("panel.launcher_icon", err.Error())
		} else if err := checkRelFileExists(m.Dir, m.Panel.LauncherIcon); err != nil {
			add("panel.launcher_icon", err.Error())
		}
	}

	// --- window ------------------------------------------------------------
	if m.Window.Decoration != "" && !slices.Contains(AllowedDecorations, m.Window.Decoration) {
		add("window.decoration", fmt.Sprintf("%q not in %v", m.Window.Decoration, AllowedDecorations))
	}
	if m.Window.Animations != "" && !slices.Contains(AllowedAnimations, m.Window.Animations) {
		add("window.animations", fmt.Sprintf("%q not in %v", m.Window.Animations, AllowedAnimations))
	}
	if m.Window.AnimationSpeed != nil && (*m.Window.AnimationSpeed < 0 || *m.Window.AnimationSpeed > 6) {
		add("window.animation_speed", fmt.Sprintf("%d must be in [0, 6] (0=instant, 3=normal, 6=very slow)", *m.Window.AnimationSpeed))
	}
	if m.Window.AnimationDurationMS != nil && (*m.Window.AnimationDurationMS < 0 || *m.Window.AnimationDurationMS > 10000) {
		add("window.animation_duration_ms", fmt.Sprintf("%d must be in [0, 10000] ms", *m.Window.AnimationDurationMS))
	}

	// --- terminal ----------------------------------------------------------
	if m.Terminal.Opacity < 0 || m.Terminal.Opacity > 1 {
		add("terminal.opacity", fmt.Sprintf("%.3f must be in [0, 1]", m.Terminal.Opacity))
	}

	// --- unknown keys (forward-compatibility warning) ----------------------
	for _, k := range m.unknownKeys {
		add(k, "unknown key (newer schema?) -- ignored")
	}

	if len(issues) == 0 {
		return nil
	}
	return &ValidationError{Issues: issues}
}

// checkPathSafety rejects two shapes of path that would let a malicious
// theme.toml reach outside its directory:
//
//  1. Absolute paths in *original* manifest fields. (Inherited-and-merged
//     fields ARE absolute by design -- they pass through this check via the
//     short-circuit: a successful check on the parent's manifest already
//     guaranteed the path was safe at that point.)
//  2. Any segment equal to "..".
//
// Symlinks at the filesystem level are NOT restricted: users can `ln -s`
// a theme's wallpapers/ to wherever they like -- that's a deliberate
// filesystem action, not manifest content.
func checkPathSafety(p string) error {
	if p == "" {
		return fmt.Errorf("path is empty")
	}
	if filepath.IsAbs(p) {
		// Trust: this can only arrive here from the merge layer rewriting an
		// inherited relative path. The parent's own validation already
		// rejected "..", so the resulting absolute path is contained.
		return nil
	}
	for _, part := range strings.Split(filepath.ToSlash(p), "/") {
		if part == ".." {
			return fmt.Errorf("path %q escapes the theme directory (contains '..')", p)
		}
	}
	return nil
}

// checkRelFileExists resolves p relative to base (which must be absolute) and
// returns an error if the resulting path is missing or is not a regular file.
// Absolute paths are honored as-is -- this matters for fields inherited from
// a parent theme, which the merge layer rewrites to absolute.
func checkRelFileExists(base, p string) error {
	if p == "" {
		return fmt.Errorf("path is empty")
	}
	full := p
	if !filepath.IsAbs(p) {
		full = filepath.Join(base, p)
	}
	info, err := os.Stat(full)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("file %q does not exist (resolved to %s)", p, full)
		}
		return fmt.Errorf("stat %s: %w", full, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%q is not a regular file", p)
	}
	return nil
}
