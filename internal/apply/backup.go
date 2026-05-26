package apply

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// backupFile copies src into backupRoot, preserving its absolute path under
// the backup root. The original file is left untouched. Returns the path of
// the backup copy.
//
// We mirror the absolute path (with the leading slash stripped) so
// /home/me/.config/gtk-3.0/gtk.css lands at
// <backupRoot>/home/me/.config/gtk-3.0/gtk.css. That makes revert a pure
// reverse-walk: every file under backupRoot has a deterministic original
// location.
func backupFile(src, backupRoot string) (string, error) {
	abs, err := filepath.Abs(src)
	if err != nil {
		return "", err
	}
	rel := strings.TrimPrefix(abs, string(filepath.Separator))
	dst := filepath.Join(backupRoot, rel)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", filepath.Dir(dst), err)
	}
	return dst, copyRegular(src, dst)
}

// copyRegular copies a regular file byte-for-byte, preserving its mode.
// Used both by backupFile and by the apply executor when an Action is
// "copy". Symlinks are handled separately via os.Symlink.
//
// The write is atomic from the destination's point of view: we first
// create <dst>.riced.tmp.<pid> next to dst, copy into it, then rename
// over dst. A crash mid-copy leaves dst untouched (the tmp file is
// orphaned and cleaned up by the executor if found).
func copyRegular(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(dst), err)
	}
	info, err := os.Stat(src)
	if err != nil {
		return err
	}

	// info.Mode().Perm() keeps the 9 rwxrwxrwx bits and strips setuid /
	// setgid / sticky. Riced-managed files (palette CSS, wallpaper
	// symlinks, etc.) should never carry those, regardless of what the
	// source file happens to have.
	tmp := dst + tmpSuffix()
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return fmt.Errorf("create %s: %w", tmp, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("copy %s -> %s: %w", src, tmp, err)
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename %s -> %s: %w", tmp, dst, err)
	}
	return nil
}

// restoreFile is the inverse of backupFile. Given a path under backupRoot,
// it computes the original destination and copies the backed-up content
// back into place. Returns the destination path that was restored.
func restoreFile(backupPath, backupRoot string) (string, error) {
	rel, err := filepath.Rel(backupRoot, backupPath)
	if err != nil {
		return "", fmt.Errorf("rel %s -> %s: %w", backupRoot, backupPath, err)
	}
	dst := string(filepath.Separator) + rel
	if err := copyRegular(backupPath, dst); err != nil {
		return "", err
	}
	return dst, nil
}
