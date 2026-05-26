package render

import "github.com/Altagen/Riced/internal/manifest"

// TerminalColors maps the manifest palette onto the 16 ANSI color slots a
// terminal emulator expects, plus the regular Background / Foreground. It is
// renderer-agnostic -- Konsole, Kitty, Alacritty all consume essentially the
// same 16-slot model.
type TerminalColors struct {
	Background string
	Foreground string

	// ANSI 0..7
	Black   string
	Red     string
	Green   string
	Yellow  string
	Blue    string
	Magenta string
	Cyan    string
	White   string

	// ANSI 8..15 (intense / bright)
	BrightBlack   string
	BrightRed     string
	BrightGreen   string
	BrightYellow  string
	BrightBlue    string
	BrightMagenta string
	BrightCyan    string
	BrightWhite   string
}

// Hardcoded defaults for ANSI slots the manifest palette does not name. Kept
// neutral so they read well against most backgrounds.
const (
	defaultGreen     = "#27ae60"
	defaultCyan      = "#1abc9c"
	defaultPureWhite = "#ffffff"
)

// ResolveTerminal projects a manifest palette onto the 16 ANSI slots. The
// mapping deliberately keeps the user's accents prominent (Red = Accent,
// Blue = Accent2, Magenta = Accent3) so anything color-coded in the
// terminal -- diffs, ls, prompts -- reads as "their theme".
func ResolveTerminal(p manifest.Palette) TerminalColors {
	r := Resolve(p)
	return TerminalColors{
		Background: r.BG,
		Foreground: r.Text,

		Black:   r.BG,
		Red:     r.Accent,
		Green:   defaultGreen,
		Yellow:  r.Warning,
		Blue:    r.Accent2,
		Magenta: r.Accent3,
		Cyan:    defaultCyan,
		White:   r.Text,

		BrightBlack:   r.Muted,
		BrightRed:     r.Accent,
		BrightGreen:   defaultGreen,
		BrightYellow:  r.Warning,
		BrightBlue:    r.Accent2,
		BrightMagenta: r.Accent3,
		BrightCyan:    defaultCyan,
		BrightWhite:   defaultPureWhite,
	}
}
