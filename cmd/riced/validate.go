package main

import (
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/Altagen/Riced/internal/manifest"
)

// runValidate implements `riced validate [--hints] <theme-dir>`.
//
// It parses the manifest (including any inherits chain), runs Validate,
// and reports every issue. Nothing is written. --hints appends a passive
// informational block reminding the user about taplo for formatting /
// schema checks -- Riced deliberately does not shell out to taplo itself,
// to keep the binary's exec surface narrow.
//
// Exit codes: 0 valid, 1 invalid, 2 bad usage.
func runValidate(args []string) int {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	hints := fs.Bool("hints", false, "after validating, print a reminder about taplo for TOML formatting / schema checks")
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, "Usage: riced validate [--hints] <theme-dir>\n")
		fmt.Fprint(os.Stderr, "(Flags must precede <theme-dir>.)\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() < 1 {
		fs.Usage()
		return 2
	}
	themeDir := fs.Arg(0)

	exit := 0
	m, err := manifest.ResolveDir(themeDir, nil)
	if err != nil {
		slog.Error("load manifest", "err", err)
		exit = 1
	} else if err := m.Validate(); err != nil {
		emitValidationIssues(err)
		exit = 1
	} else {
		slog.Info("manifest valid", "slug", m.Meta.Slug, "dir", m.Dir)
	}

	if *hints {
		printValidationHints(themeDir)
	}
	return exit
}

// printValidationHints writes a passive informational block to stderr
// explaining the validation split. Never invokes any external command --
// it just tells the user what they could run themselves.
func printValidationHints(themeDir string) {
	manifestPath := filepath.Join(themeDir, "theme.toml")
	fmt.Fprint(os.Stderr, `
Hints:
  Riced checks the manifest's SEMANTICS (palette, enums, paths, slug shape).
  TOML formatting / style is a separate concern handled by taplo:

    taplo format --check `+manifestPath+`
    taplo lint   --schema <path-to-riced>/schemas/theme.schema.json `+manifestPath+`

  Schema:  https://github.com/Altagen/Riced/blob/main/schemas/theme.schema.json
  Install: paru -S taplo-cli   (or https://taplo.tamasfe.dev/)
`)
}

// emitValidationIssues unwraps a *manifest.ValidationError and logs every
// issue with structured fields. Falls back to a single Error for any other
// error type.
func emitValidationIssues(err error) {
	var verr *manifest.ValidationError
	if errors.As(err, &verr) {
		for _, iss := range verr.Issues {
			slog.Error("manifest issue", "field", iss.Field, "msg", iss.Msg)
		}
		return
	}
	slog.Error("validate", "err", err)
}
