package apply

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// ExternalDownloader fetches `url` to `dst` and verifies its SHA-256
// matches `wantSHA` (lower-case hex). Tests inject a stub via
// ExecOptions.Download; production uses DefaultDownload.
type ExternalDownloader func(url, wantSHA, dst string) error

// DefaultDownload is the production implementation: HTTPS GET, stream to
// a temp file, sha256-verify, rename into place. Idempotent: when `dst`
// already exists with the right hash, returns nil without redownloading.
//
// Mirrors the atomic-rename pattern used elsewhere in the apply package
// so a crash mid-download never leaves a half-file at `dst`.
func DefaultDownload(url, wantSHA, dst string) error {
	if existingMatches(dst, wantSHA) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(dst), err)
	}

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("get %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("get %s: HTTP %s", url, resp.Status)
	}

	tmp := dst + ".riced.tmp"
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("create %s: %w", tmp, err)
	}
	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(out, hash), resp.Body); err != nil {
		out.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("download %s: %w", url, err)
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close %s: %w", tmp, err)
	}
	got := hex.EncodeToString(hash.Sum(nil))
	if got != wantSHA {
		_ = os.Remove(tmp)
		return fmt.Errorf("sha256 mismatch for %s: got %s, want %s", url, got, wantSHA)
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename %s -> %s: %w", tmp, dst, err)
	}
	return nil
}

// existingMatches returns true if `path` exists and its sha256 already
// matches `wantSHA`. Used so re-applying a theme doesn't re-download
// archives it already has.
func existingMatches(path, wantSHA string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false
	}
	return hex.EncodeToString(h.Sum(nil)) == wantSHA
}

// ExternalCacheDir is where downloads land before being handed to
// kpackagetool6 / tar / unzip. Per-user, persisted across runs.
func ExternalCacheDir(homeDir string) string {
	return filepath.Join(homeDir, ".riced", "cache", "external")
}
