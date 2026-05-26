package render

import (
	"bytes"
	"fmt"
	"text/template"

	"github.com/Altagen/Riced/internal/manifest"
)

// WallpaperOpts feeds the wallpaper renderer with paths only known at write
// time. WallpapersDir must be an absolute path to the directory where the
// caller has (or will) materialize symlinks named NN-<basename>.<ext>.
type WallpaperOpts struct {
	WallpapersDir string
}

// wallpaperData is the view-model handed to the wallpaper template. It
// abstracts the two Plasma plugins (image / slideshow) behind a single set
// of fields so the template stays small.
type wallpaperData struct {
	Mode        string // "single" or "slideshow"
	Plugin      string // "org.kde.image" or "org.kde.slideshow"
	Path        string // absolute path: a file in single mode, a dir in slideshow
	Interval    int
	SortingMode int
	FillMode    int
}

// Plasma FillMode constants. 6 (Scaled & Cropped) reads well on most
// wallpapers and is Plasma 6's default.
const plasmaFillScaledCropped = 6

// Plasma SortingMode for slideshow. 1 = alphabetical ascending, which under
// our NN-<basename> naming convention reproduces manifest order.
const plasmaSortingAlphaAsc = 1

// Wallpapers renders the Plasma wallpaper descriptor INI fragment for the
// given manifest. It never writes to disk and never reads from
// opts.WallpapersDir -- the directory only needs to exist at apply time.
//
// In slideshow mode, .Path is opts.WallpapersDir (the directory containing
// the curated symlinks). In single mode, it is opts.WallpapersDir joined
// with the first manifest path's NN-basename -- the caller is responsible
// for materializing that symlink.
func Wallpapers(m *manifest.Manifest, opts WallpaperOpts) ([]byte, error) {
	if opts.WallpapersDir == "" {
		return nil, fmt.Errorf("WallpapersDir must be set")
	}
	if len(m.Wallpapers.Paths) == 0 {
		return nil, fmt.Errorf("manifest has no wallpapers")
	}

	mode := m.Wallpapers.Mode
	if mode == "" {
		mode = "single"
	}

	data := wallpaperData{
		Mode:        mode,
		FillMode:    plasmaFillScaledCropped,
		SortingMode: plasmaSortingAlphaAsc,
	}

	switch mode {
	case "slideshow":
		data.Plugin = "org.kde.slideshow"
		data.Path = opts.WallpapersDir
		data.Interval = m.Wallpapers.Interval
		if data.Interval <= 0 {
			data.Interval = 600
		}
	case "single":
		data.Plugin = "org.kde.image"
		data.Path = SymlinkName(opts.WallpapersDir, 0, m.Wallpapers.Paths[0])
	default:
		return nil, fmt.Errorf("unknown wallpaper mode %q", mode)
	}

	tmpl, err := template.
		New("wallpaper.ini.tmpl").
		Funcs(FuncMap()).
		ParseFS(templatesFS, "templates/wallpaper.ini.tmpl")
	if err != nil {
		return nil, fmt.Errorf("parse wallpaper template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("execute wallpaper template: %w", err)
	}
	return buf.Bytes(), nil
}
