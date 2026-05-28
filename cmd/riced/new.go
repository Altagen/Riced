package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/Altagen/Riced/internal/manifest"
	"github.com/Altagen/Riced/internal/registry"
)

// runNew dispatches `riced new <subcommand>`.
//
// Today the only subcommand is `theme`. Repository scaffolding lives at
// `riced repo init` -- that command is idempotent and handles every case
// (brand-new directory, empty git clone, already-initialized repo) with
// one entry point.
func runNew(args []string) int {
	if len(args) < 1 {
		fmt.Fprint(os.Stderr, "Usage: riced new theme [args]\n")
		fmt.Fprint(os.Stderr, "(For repository scaffolding, use `riced repo init`.)\n")
		return exitUsage
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "theme":
		return runNewTheme(rest)
	case "repo":
		slog.Error("`riced new repo` has been replaced by `riced repo init`",
			"hint", "run `riced repo init [--register] <path>` instead")
		return exitUsage
	default:
		slog.Error("unknown 'new' subcommand", "sub", sub, "hint", "only 'theme' is supported")
		return exitUsage
	}
}

// runNewTheme scaffolds a new theme directory and minimal theme.toml.
//
// Target resolution, in order:
//  1. --repo NAME: look up in registry, write under <repo-path>/themes/<slug>/
//  2. CWD or any ancestor contains repository.toml: write under <repo>/themes/<slug>/
//  3. Else: ~/.riced/themes/<slug>/  (private theme)
func runNewTheme(args []string) int {
	fs := flag.NewFlagSet("new theme", flag.ContinueOnError)
	from := fs.String("from", "", "parent theme slug to inherit from")
	repoFlag := fs.String("repo", "", "name of registered repository to create the theme in")
	mode := fs.String("mode", "dark", "color mode (\"dark\" or \"light\")")
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, "Usage: riced new theme [--from PARENT] [--repo NAME] [--mode dark|light] <slug>\n")
		fmt.Fprint(os.Stderr, "(Flags must precede <slug> -- stdlib flag stops at the first positional.)\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() < 1 {
		fs.Usage()
		return exitUsage
	}
	slug := fs.Arg(0)
	if !isKebab(slug) {
		slog.Error("invalid slug", "slug", slug, "want", "kebab-case (a-z, 0-9, '-')")
		return exitUsage
	}
	if *mode != "" && *mode != "dark" && *mode != "light" {
		slog.Error("invalid mode", "mode", *mode, "want", "dark or light")
		return exitUsage
	}

	target, err := resolveNewThemeTarget(*repoFlag, slug)
	if err != nil {
		slog.Error("resolve target", "err", err)
		return exitErr
	}
	if _, err := os.Stat(target); err == nil {
		slog.Error("target already exists", "path", target)
		return exitErr
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		slog.Error("mkdir", "path", target, "err", err)
		return exitErr
	}

	manifestPath := filepath.Join(target, manifest.FileName)
	body := newThemeManifest(slug, *from, *mode)
	if err := os.WriteFile(manifestPath, []byte(body), 0o644); err != nil {
		slog.Error("write theme.toml", "err", err)
		return exitErr
	}

	fmt.Printf("Created theme at %s\n", manifestPath)
	if *from == "" {
		fmt.Println("Next: edit theme.toml to fill in [wallpapers].paths before validating.")
	} else {
		fmt.Printf("Next: edit theme.toml to override what differs from %q.\n", *from)
	}
	fmt.Printf("Then: riced validate %s\n", target)
	return exitOK
}

// resolveNewThemeTarget returns the absolute directory where a new theme
// of the given slug should live, based on the resolution rules documented
// on runNewTheme.
func resolveNewThemeTarget(repoFlag, slug string) (string, error) {
	if repoFlag != "" {
		reg, err := registry.Load()
		if err != nil {
			return "", err
		}
		r, ok := reg.Get(repoFlag)
		if !ok {
			return "", fmt.Errorf("repository %q is not registered", repoFlag)
		}
		return filepath.Join(r.Path, "themes", slug), nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	if repoRoot := registry.FindRepositoryRoot(cwd); repoRoot != "" {
		return filepath.Join(repoRoot, "themes", slug), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".riced", "themes", slug), nil
}

// newThemeManifest renders a minimal valid theme.toml as a string. For
// inheriting themes, only the override block is emitted -- everything else
// flows from the parent.
func newThemeManifest(slug, inherits, mode string) string {
	var b strings.Builder
	b.WriteString("schema_version = 1\n\n")
	b.WriteString("[meta]\n")
	fmt.Fprintf(&b, "name = %q\n", titleCase(slug))
	fmt.Fprintf(&b, "slug = %q\n", slug)
	if inherits != "" {
		fmt.Fprintf(&b, "inherits = %q\n", inherits)
	}
	if mode != "" {
		fmt.Fprintf(&b, "mode = %q\n", mode)
	}
	b.WriteString("\n")

	if inherits == "" {
		b.WriteString(`[palette]
bg     = "#0a0a14"
text   = "#e8e8f0"
accent = "#ff2a4b"

[wallpapers]
mode = "single"
# TODO: drop one or more PNGs into the theme dir (or symlink them from the
# repo's wallpapers/) and list them here. The path is relative to this
# theme.toml file.
paths = []
`)
	} else {
		b.WriteString(`# Override only the palette fields that differ from the parent. Anything
# you don't redeclare here is inherited from "` + inherits + `".
[palette]
accent = "#ff2a4b"
`)
	}
	return b.String()
}

// titleCase turns "s4-dark" into "S4 Dark". Naive on purpose -- the user
// is expected to refine the Name field themselves.
func titleCase(slug string) string {
	parts := strings.Split(slug, "-")
	for i, p := range parts {
		if p == "" {
			continue
		}
		r := []rune(p)
		r[0] = unicode.ToUpper(r[0])
		parts[i] = string(r)
	}
	return strings.Join(parts, " ")
}

// isKebab matches lowercase kebab-case identifiers (no leading/trailing
// dash, no consecutive dashes, at least one char). Shared between new.go
// (slug validation) and repo.go (derived repository name validation).
func isKebab(s string) bool {
	if s == "" || strings.HasPrefix(s, "-") || strings.HasSuffix(s, "-") {
		return false
	}
	prevDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			prevDash = false
		case r >= '0' && r <= '9':
			prevDash = false
		case r == '-':
			if prevDash {
				return false
			}
			prevDash = true
		default:
			return false
		}
	}
	return true
}
