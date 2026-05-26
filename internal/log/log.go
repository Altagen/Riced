// Package log centralizes structured logging configuration for the Riced
// CLI. Built on log/slog (Go 1.21+) -- zero external dependencies.
//
// Conventions across the codebase:
//
//   - Diagnostics (errors, progress, warnings) go through slog and land on
//     stderr.
//   - User-facing data (tables, summaries, "Wrote: …" lists) stays on
//     stdout via fmt.Println. That stream must remain machine-parseable;
//     don't pollute it with log records.
//   - Levels: Debug for internals worth seeing under -v, Info for "normal
//     happened", Warn for "you may want to know", Error for "failed".
package log

import (
	"log/slog"
	"os"
	"strings"
)

// Format selects the slog handler.
type Format int

const (
	FormatText Format = iota
	FormatJSON
)

// Options control how Setup configures the default logger.
type Options struct {
	Level  slog.Level
	Format Format
}

// Setup installs the configured handler on slog.Default and returns the
// logger for callers that prefer explicit injection. Safe to call more than
// once; the last call wins.
//
// The text handler drops the timestamp: every record is "now" for a sync
// CLI and the noise outweighs the value. JSON keeps it because log
// aggregators and CI tooling expect a time field.
func Setup(opts Options) *slog.Logger {
	var handler slog.Handler
	switch opts.Format {
	case FormatJSON:
		handler = slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: opts.Level})
	default:
		handler = slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: opts.Level,
			ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
				if a.Key == slog.TimeKey {
					return slog.Attr{}
				}
				return a
			},
		})
	}
	logger := slog.New(handler)
	slog.SetDefault(logger)
	return logger
}

// ParseLevel maps common spellings to slog.Level. Unknown values fall back
// to Info so a typo in a flag never silences the logger entirely.
func ParseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "info", "":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error", "err":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// ParseFormat maps "json" (any case) to FormatJSON and everything else to
// FormatText.
func ParseFormat(s string) Format {
	if strings.EqualFold(strings.TrimSpace(s), "json") {
		return FormatJSON
	}
	return FormatText
}
