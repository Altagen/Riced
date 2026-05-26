package render

import (
	"fmt"
	"path/filepath"
)

// SymlinkName returns the canonical NN-<basename> name Riced uses for the
// i-th curated wallpaper symlink (0-indexed). The numeric prefix preserves
// manifest order under Plasma's alphabetical SortingMode.
//
// Pure helper: takes no filesystem side effects. Used by both the renderer
// (to embed paths in the descriptor) and by the apply/generate wiring (to
// create the symlinks).
func SymlinkName(dir string, i int, srcPath string) string {
	return filepath.Join(dir, fmt.Sprintf("%02d-%s", i+1, filepath.Base(srcPath)))
}
