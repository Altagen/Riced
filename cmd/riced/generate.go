package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/Altagen/Riced/internal/manifest"
	"github.com/Altagen/Riced/internal/render"
)

// runGenerate implements `riced generate <theme-dir> [--out <dir>]`.
//
// It loads + validates the manifest, runs every available renderer, and
// writes the results under <out>/<slug>/. It NEVER touches the user's
// ~/.config or ~/.local/share -- that is the apply layer's job.
//
// Exit codes: 0 success, 1 invalid manifest or write failure, 2 bad usage.
func runGenerate(args []string) int {
	fs := flag.NewFlagSet("generate", flag.ContinueOnError)
	outDir := fs.String("out", "build", "output directory (per-theme subdir created underneath)")
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, "Usage: riced generate <theme-dir> [--out <dir>]\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() < 1 {
		fs.Usage()
		return exitUsage
	}

	slog.Debug("resolving manifest", "dir", fs.Arg(0))
	m, err := manifest.ResolveDir(fs.Arg(0), nil)
	if err != nil {
		slog.Error("load manifest", "err", err)
		return exitErr
	}
	if m.Meta.Inherits != "" {
		slog.Debug("merged parent", "inherits", m.Meta.Inherits)
	}

	if err := m.Validate(); err != nil {
		emitValidationIssues(err)
		return exitErr
	}

	summarize(m)

	themeOut := filepath.Join(*outDir, m.Meta.Slug)
	if err := os.MkdirAll(themeOut, 0o755); err != nil {
		slog.Error("create output dir", "dir", themeOut, "err", err)
		return exitErr
	}

	written, err := renderAll(m, themeOut)
	if err != nil {
		slog.Error("render", "err", err)
		return exitErr
	}

	fmt.Println("\nWrote:")
	for _, p := range written {
		fmt.Printf("  %s\n", p)
	}
	return exitOK
}

// renderAll runs every Phase 3+ renderer against m and writes its output
// under outDir. Returns the list of files written (in order) for reporting.
//
// Adding a new renderer is one line: produce the bytes, call writeFile.
func renderAll(m *manifest.Manifest, outDir string) ([]string, error) {
	var written []string

	// Wallpapers come first because the wallpaper.ini descriptor needs the
	// absolute path of the symlink directory we are about to create.
	wallpapersDir, wallpaperSymlinks, err := materializeWallpapers(m, outDir)
	if err != nil {
		return written, fmt.Errorf("wallpapers: %w", err)
	}
	slog.Debug("materialized wallpapers", "count", len(wallpaperSymlinks), "dir", wallpapersDir)
	written = append(written, wallpaperSymlinks...)

	// Launcher icon: same materialize pattern, but only one file.
	iconPath, err := materializeLauncherIcon(m, outDir)
	if err != nil {
		return written, fmt.Errorf("launcher icon: %w", err)
	}
	if iconPath != "" {
		slog.Debug("materialized launcher icon", "path", iconPath)
		written = append(written, iconPath)
	}

	// Lock screen wallpaper: same single-symlink pattern as the launcher icon.
	lockPath, err := materializeLockWallpaper(m, outDir)
	if err != nil {
		return written, fmt.Errorf("lock wallpaper: %w", err)
	}
	if lockPath != "" {
		slog.Debug("materialized lock wallpaper", "path", lockPath)
		written = append(written, lockPath)
	}

	wallpaperRender := func(mm *manifest.Manifest) ([]byte, error) {
		return render.Wallpapers(mm, render.WallpaperOpts{WallpapersDir: wallpapersDir})
	}

	type job struct {
		path string
		fn   func(*manifest.Manifest) ([]byte, error)
	}
	jobs := []job{
		{filepath.Join(outDir, m.Meta.Slug+".colors"), render.Colorscheme},
		{filepath.Join(outDir, "konsole", m.Meta.Slug+".colorscheme"), render.KonsoleColorscheme},
		{filepath.Join(outDir, "konsole", m.Meta.Slug+".profile"), render.KonsoleProfile},
		{filepath.Join(outDir, "gtk-3.0", "gtk.css"), render.GTKCSS},
		{filepath.Join(outDir, "gtk-4.0", "gtk.css"), render.GTKCSS},
	}
	// wallpaper.ini is a single-set descriptor; it doesn't represent the
	// per-screen layout cleanly, and the apply pipeline drives Plasma
	// directly via plasmashell JS anyway. Only emit it when the manifest
	// uses the flat Paths list (single image OR mirror slideshow).
	if len(m.Wallpapers.Paths) > 0 {
		jobs = append(jobs, job{filepath.Join(outDir, "wallpaper.ini"), wallpaperRender})
	}

	for _, j := range jobs {
		body, err := j.fn(m)
		if err != nil {
			return written, fmt.Errorf("render %s: %w", filepath.Base(j.path), err)
		}
		if err := os.MkdirAll(filepath.Dir(j.path), 0o755); err != nil {
			return written, fmt.Errorf("mkdir for %s: %w", j.path, err)
		}
		if err := os.WriteFile(j.path, body, 0o644); err != nil {
			return written, fmt.Errorf("write %s: %w", j.path, err)
		}
		slog.Debug("rendered", "path", j.path)
		written = append(written, j.path)
	}
	return written, nil
}

func summarize(m *manifest.Manifest) {
	fmt.Printf("Theme:        %s (%s)\n", m.Meta.Name, m.Meta.Slug)
	fmt.Printf("Mode:         %s\n", m.Meta.Mode)
	if m.Meta.Theme != "" {
		fmt.Printf("Theme:        %s\n", m.Meta.Theme)
	}
	fmt.Printf("Directory:    %s\n", m.Dir)
	fmt.Printf("Schema:       v%d\n", m.SchemaVersion)
	if m.Meta.Inherits != "" {
		fmt.Printf("Inherits:     %s\n", m.Meta.Inherits)
	}
	fmt.Println()

	fmt.Println("Palette:")
	fmt.Printf("  bg       %s\n", m.Palette.BG)
	fmt.Printf("  surface  %s\n", m.Palette.Surface)
	fmt.Printf("  text     %s\n", m.Palette.Text)
	fmt.Printf("  accent   %s\n", m.Palette.Accent)
	if m.Palette.Accent2 != "" {
		fmt.Printf("  accent2  %s\n", m.Palette.Accent2)
	}
	if m.Palette.Accent3 != "" {
		fmt.Printf("  accent3  %s\n", m.Palette.Accent3)
	}
	fmt.Println()

	mode := m.Wallpapers.Mode
	if mode == "" {
		mode = "single"
	}
	fmt.Printf("Wallpapers:   %d file(s), mode=%s", len(m.Wallpapers.Paths), mode)
	if mode == "slideshow" {
		fmt.Printf(", interval=%ds", m.Wallpapers.Interval)
	}
	fmt.Println()
	for _, p := range m.Wallpapers.Paths {
		fmt.Printf("  - %s\n", p)
	}
	fmt.Println()

	fmt.Printf("Panel:        position=%s floating=%v height=%d\n",
		m.Panel.Position, m.Panel.Floating, m.Panel.Height)
	if m.Panel.LauncherIcon != "" {
		fmt.Printf("              launcher_icon=%s\n", m.Panel.LauncherIcon)
	}
	fmt.Printf("Window:       decoration=%s animations=%s blur=%v wobbly=%v\n",
		m.Window.Decoration, m.Window.Animations, m.Window.Blur, m.Window.Wobbly)
	fmt.Printf("Fonts:        ui=%q mono=%q\n", m.Fonts.UI, m.Fonts.Mono)
	fmt.Printf("Terminal:     opacity=%.2f\n", m.Terminal.Opacity)
}
