package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Altagen/Riced/internal/manifest"
	"github.com/Altagen/Riced/internal/render"
)

// materializeWallpapers builds the curated wallpaper directory under
// build/<slug>/wallpapers/: one absolute symlink per manifest entry, named
// NN-<basename>.<ext> so Plasma's alphabetical SortingMode reproduces the
// manifest order. Returns the root directory plus the list of every
// symlink created (for the renderAll progress report).
//
// Layout:
//   - Legacy / mirror: a flat dir wallpapers/01-name.png, 02-name.png ...
//   - Per-screen: subdirs wallpapers/screen-0/, wallpapers/screen-1/ ...
//     each with its own NN-name.png list. Plasma's slideshow plugin
//     scans the directory it points at, so each screen gets its own
//     scope.
//
// Wipes any stale symlinks from a previous run before recreating them so
// repeated `riced generate` calls converge instead of accumulating.
func materializeWallpapers(m *manifest.Manifest, themeOut string) (string, []string, error) {
	dir := filepath.Join(themeOut, "wallpapers")
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", nil, fmt.Errorf("resolve wallpapers dir: %w", err)
	}

	if err := wipeSymlinks(absDir); err != nil {
		return "", nil, err
	}
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		return "", nil, fmt.Errorf("mkdir %s: %w", absDir, err)
	}

	var written []string

	// Per-screen layout (takes precedence over flat Paths when set).
	// The subdir name is derived from the rule's match criterion via
	// WallpapersScreen.SubdirName() -- both materialize (here) and the
	// planner pick the same name from the same input, so the action's
	// SlidePaths argument lines up with what we just wrote.
	if len(m.Wallpapers.Screens) > 0 {
		for _, screen := range m.Wallpapers.Screens {
			subDir := filepath.Join(absDir, screen.SubdirName())
			if err := wipeSymlinks(subDir); err != nil {
				return "", nil, err
			}
			if err := os.MkdirAll(subDir, 0o755); err != nil {
				return "", nil, fmt.Errorf("mkdir %s: %w", subDir, err)
			}
			for i, rel := range screen.Paths {
				src, err := resolveThemeAsset(m.Dir, rel)
				if err != nil {
					return "", nil, err
				}
				dst := render.SymlinkName(subDir, i, rel)
				if err := os.Symlink(src, dst); err != nil {
					return "", nil, fmt.Errorf("symlink %s -> %s: %w", dst, src, err)
				}
				written = append(written, dst)
			}
		}
		return absDir, written, nil
	}

	// Flat layout (legacy / mirror).
	for i, rel := range m.Wallpapers.Paths {
		src, err := resolveThemeAsset(m.Dir, rel)
		if err != nil {
			return "", nil, err
		}
		dst := render.SymlinkName(absDir, i, rel)
		if err := os.Symlink(src, dst); err != nil {
			return "", nil, fmt.Errorf("symlink %s -> %s: %w", dst, src, err)
		}
		written = append(written, dst)
	}
	return absDir, written, nil
}

// materializeLauncherIcon places the manifest's panel.launcher_icon as a
// symlink under build/<slug>/icons/launcher<ext>. The same idempotency
// trick as wallpapers: stale symlinks are wiped first. Returns the
// absolute path of the materialized symlink, or "" when the manifest
// has no launcher icon configured.
func materializeLauncherIcon(m *manifest.Manifest, themeOut string) (string, error) {
	if m.Panel.LauncherIcon == "" {
		return "", nil
	}
	dir := filepath.Join(themeOut, "icons")
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve icons dir: %w", err)
	}
	if err := wipeSymlinks(absDir); err != nil {
		return "", err
	}
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", absDir, err)
	}

	src, err := resolveThemeAsset(m.Dir, m.Panel.LauncherIcon)
	if err != nil {
		return "", fmt.Errorf("launcher icon: %w", err)
	}
	// Preserve the file's extension so KDE / Plasma can identify the
	// image type. Name the file "launcher.<ext>" -- consistent regardless
	// of the source name, which makes the target path predictable for
	// Targets.Map.
	ext := filepath.Ext(src)
	dst := filepath.Join(absDir, "launcher"+ext)
	if err := os.Symlink(src, dst); err != nil {
		return "", fmt.Errorf("symlink %s -> %s: %w", dst, src, err)
	}
	return dst, nil
}

// resolveThemeAsset turns a manifest path (which may be relative to the
// theme dir OR already absolute -- the merge layer rewrites inherited
// paths to absolute) into a fully resolved absolute path with all
// symlinks followed.
//
// Two separate failure modes get distinct error messages so callers can
// surface "you referenced a missing file" vs "your symlink is broken"
// to the user.
func resolveThemeAsset(themeDir, p string) (string, error) {
	abs := p
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(themeDir, abs)
	}
	resolved, err := filepath.Abs(abs)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", p, err)
	}
	resolved, err = filepath.EvalSymlinks(resolved)
	if err != nil {
		return "", fmt.Errorf("eval symlink %s: %w", p, err)
	}
	return resolved, nil
}

// wipeSymlinks removes every entry in dir that is itself a symlink. It does
// not touch regular files or subdirectories -- defensive, in case the user
// drops something custom into the build output.
func wipeSymlinks(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read %s: %w", dir, err)
	}
	for _, e := range entries {
		if e.Type()&os.ModeSymlink == 0 {
			continue
		}
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
			return fmt.Errorf("remove stale symlink %s: %w", e.Name(), err)
		}
	}
	return nil
}
