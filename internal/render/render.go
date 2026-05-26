// Package render generates KDE Plasma configuration artifacts from a parsed
// manifest. Each renderer returns bytes; callers (cmd/riced) decide where to
// write them. Renderers must never touch the user's live ~/.config or
// ~/.local/share -- that is the apply layer's job.
package render

import (
	"fmt"
	"strconv"
	"strings"
	"text/template"

	"github.com/Altagen/Riced/internal/manifest"
)

// ResolvedPalette is a Palette with every optional field filled in via
// deterministic fallbacks. Renderers always work against a ResolvedPalette so
// templates don't need to handle empty strings.
type ResolvedPalette struct {
	BG       string
	Surface  string
	Text     string
	Subtext  string
	Muted    string
	Accent   string
	Accent2  string
	Accent3  string
	Warning  string
	Error    string
	Positive string // not in manifest schema today; constant fallback
}

// defaults applied when an optional palette field is empty. Kept simple and
// neutral; renderers can refine later.
const (
	defaultMuted    = "#808080"
	defaultWarning  = "#ffcc00"
	defaultError    = "#ff5050"
	defaultPositive = "#27ae60"
)

// Resolve fills optional palette fields with sensible fallbacks. It assumes
// the input has already passed manifest.Validate (i.e. required fields are
// present and well-formed).
func Resolve(p manifest.Palette) ResolvedPalette {
	pick := func(v, fallback string) string {
		if v == "" {
			return fallback
		}
		return v
	}
	return ResolvedPalette{
		BG:       p.BG,
		Surface:  pick(p.Surface, p.BG),
		Text:     p.Text,
		Subtext:  pick(p.Subtext, p.Text),
		Muted:    pick(p.Muted, defaultMuted),
		Accent:   p.Accent,
		Accent2:  pick(p.Accent2, p.Accent),
		Accent3:  pick(p.Accent3, p.Accent),
		Warning:  pick(p.Warning, defaultWarning),
		Error:    pick(p.Error, defaultError),
		Positive: defaultPositive,
	}
}

// hexRGB parses "#RRGGBB" or "#RRGGBBAA" and returns the RGB components.
// Alpha is discarded (KDE color sections use RGB triplets, not RGBA).
func hexRGB(hex string) (r, g, b int, err error) {
	h := strings.TrimPrefix(hex, "#")
	if len(h) != 6 && len(h) != 8 {
		return 0, 0, 0, fmt.Errorf("invalid hex color %q", hex)
	}
	n, err := strconv.ParseUint(h[:6], 16, 32)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("invalid hex color %q: %w", hex, err)
	}
	return int(n>>16) & 0xff, int(n>>8) & 0xff, int(n) & 0xff, nil
}

// rgb formats a hex color as KDE's "r,g,b" triplet. Used as a template func.
// On bad input it returns an obviously-broken string so a failed render shows
// up in tests rather than silently passing.
func rgb(hex string) string {
	r, g, b, err := hexRGB(hex)
	if err != nil {
		return "INVALID(" + hex + ")"
	}
	return fmt.Sprintf("%d,%d,%d", r, g, b)
}

// FuncMap exposes the template functions every renderer uses.
func FuncMap() template.FuncMap {
	return template.FuncMap{
		"rgb": rgb,
	}
}
