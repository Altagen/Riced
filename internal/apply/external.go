package apply

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ExternalDownloader fetches `url` to `dst` and verifies its SHA-256
// matches `wantSHA` (lower-case hex). Tests inject a stub via
// ExecOptions.Download; production uses DefaultDownload.
type ExternalDownloader func(url, wantSHA, dst string) error

// DefaultDownload is the production implementation: HTTPS GET, stream to
// a temp file, sha256-verify, rename into place. Idempotent: when `dst`
// already exists with the right hash, returns nil without redownloading.
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
	// LimitReader caps total bytes copied. When the upstream sends more
	// we still read maxDownloadBytes+1 (the limit-exceed signal) and
	// detect that as an explicit error rather than running OOM.
	limited := io.LimitReader(resp.Body, maxDownloadBytes+1)
	written, copyErr := io.Copy(io.MultiWriter(out, hash), limited)
	if copyErr != nil {
		if closeErr := out.Close(); closeErr != nil {
			slog.Warn("download tmp close after copy error", "tmp", tmp, "close_err", closeErr)
		}
		_ = os.Remove(tmp)
		return fmt.Errorf("download %s: %w", url, copyErr)
	}
	if written > maxDownloadBytes {
		_ = out.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("download %s: exceeds size cap of %d bytes", url, maxDownloadBytes)
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

// archiveSHAPrefixLen is the length of the sha-256 prefix kept on the
// cached archive filename. 12 hex chars give 2^48 namespace collisions
// resistance, more than enough for a per-user theme cache.
const archiveSHAPrefixLen = 12

// maxDownloadBytes caps a single look archive download. A malicious URL
// could otherwise stream gigabytes into the cache; theme archives are
// typically <50 MB so 200 MB gives generous headroom while preventing
// disk exhaustion. Surfaces as a clear error rather than running OOM.
const maxDownloadBytes = 200 << 20 // 200 MiB

// ExternalCacheDir is the persistent download cache root.
func ExternalCacheDir(homeDir string) string {
	return filepath.Join(homeDir, ".riced", "cache", "external")
}

// LooksDir is the convention for user-managed local theme sources
// (typically populated via `git clone` by the user).
func LooksDir(homeDir string) string {
	return filepath.Join(homeDir, ".riced", "looks")
}

// lookSpec is the decoded shape of an "external" action's Args, parsed
// out of Plan.Build's positional encoding by parseLookArgs. Unexported
// because parseLookArgs is the only entry point -- external callers
// build looks via the manifest, not via this struct.
type lookSpec struct {
	Type       string   // Plasma/LookAndFeel, icons, ...
	SourceKind string   // "url" or "local"
	Source     string   // URL or local subdir name
	SHA256     string   // empty for local
	Install    string   // auto | kpackage | extract | script
	Script     string   // relative path for install=script
	Args       []string // script args (env-expanded at exec time)
}

func parseLookArgs(args []string) (lookSpec, error) {
	if len(args) < 5 {
		return lookSpec{}, fmt.Errorf("malformed look args: want >= 5, got %d", len(args))
	}
	src := args[1]
	kind, val, ok := strings.Cut(src, ":")
	if !ok || (kind != "url" && kind != "local") {
		return lookSpec{}, fmt.Errorf("malformed look source %q (want url:<...> or local:<name>)", src)
	}
	return lookSpec{
		Type:       args[0],
		SourceKind: kind,
		Source:     val,
		SHA256:     args[2],
		Install:    args[3],
		Script:     args[4],
		Args:       args[5:],
	}, nil
}

// executeLook orchestrates the full pipeline for a single Kind="external"
// action: resolve source → (download + extract) | (use local dir) →
// normalize → (auto-detect strategy if needed) → dispatch.
//
// The staging dir lives at <ExternalCacheDir>/<sha-prefix or local-name>-staging/
// for URL sources and at <LooksDir>/<name> for local sources. URL
// extractions are kept across applies (cheap re-apply) but never used as
// the install target -- the extracted contents are *normalized* into a
// "root" pointer the strategies operate on.
func executeLook(spec lookSpec, name string, opts ExecOptions) error {
	home := opts.HomeDir
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("resolve HOME: %w", err)
		}
		home = h
	}

	root, archivePath, err := resolveLookSource(spec, name, home, opts)
	if err != nil {
		return fmt.Errorf("look %q: %w", name, err)
	}
	root = normalizeRoot(root)

	install := spec.Install
	if install == "" || install == "auto" {
		detected, derr := detectStrategy(root)
		if derr != nil {
			return fmt.Errorf("look %q: auto-detect: %w", name, derr)
		}
		install = detected
		slog.Info("look auto-detected", "name", name, "strategy", install, "root", root)
	}

	switch install {
	case "kpackage":
		// kpackagetool6 -i accepts both archives and directories. We
		// prefer the original archive when we have it (preserves
		// metadata) and fall back to the normalized dir otherwise.
		target := archivePath
		if target == "" {
			target = root
		}
		if err := opts.KDE.KPackageInstall(spec.Type, target); err != nil {
			return fmt.Errorf("look %q: kpackage install: %w", name, err)
		}

	case "extract":
		dst := iconsDataDir(home)
		if err := os.MkdirAll(dst, 0o755); err != nil {
			return fmt.Errorf("look %q: mkdir %s: %w", name, dst, err)
		}
		entries, err := os.ReadDir(root)
		if err != nil {
			return fmt.Errorf("look %q: read %s: %w", name, root, err)
		}
		for _, e := range entries {
			if err := copyTreeOver(filepath.Join(root, e.Name()), filepath.Join(dst, e.Name())); err != nil {
				return fmt.Errorf("look %q: copy %s: %w", name, e.Name(), err)
			}
		}

	case "script":
		if spec.Script == "" {
			return fmt.Errorf("look %q: install=script but no script path set", name)
		}
		scriptPath := filepath.Join(root, spec.Script)
		if !isUnder(scriptPath, root) {
			return fmt.Errorf("look %q: script %q escapes the source root", name, spec.Script)
		}
		if _, err := os.Stat(scriptPath); err != nil {
			return fmt.Errorf("look %q: script %s not found after normalize", name, spec.Script)
		}
		bash, err := exec.LookPath("bash")
		if err != nil {
			return fmt.Errorf("look %q: bash not found on PATH (required by install=script): %w", name, err)
		}
		expanded := make([]string, len(spec.Args))
		for i, a := range spec.Args {
			expanded[i] = expandEnv(a)
		}
		slog.Info("look running script", "name", name, "cmd", scriptPath, "args", expanded)
		cmd := exec.Command(bash, append([]string{scriptPath}, expanded...)...)
		cmd.Dir = root
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("look %q: script failed: %w", name, err)
		}

	default:
		return fmt.Errorf("look %q: unknown install strategy %q", name, install)
	}

	slog.Info("look installed", "name", name, "strategy", install)
	return nil
}

// resolveLookSource turns the spec's source into a local directory
// (root) ready for strategy dispatch. Returns the directory plus the
// original archive path when applicable (kpackage prefers the archive
// over the extracted tree to preserve metadata.json signatures).
func resolveLookSource(spec lookSpec, _ string, home string, opts ExecOptions) (root, archivePath string, err error) {
	switch spec.SourceKind {
	case "local":
		dir := filepath.Join(LooksDir(home), spec.Source)
		if _, err := os.Stat(dir); err != nil {
			return "", "", fmt.Errorf("local source %s not found (clone there first or check the manifest): %w", dir, err)
		}
		return dir, "", nil

	case "url":
		dl := opts.Download
		if dl == nil {
			dl = DefaultDownload
		}
		// Manifest validation guarantees SHA256 is 64 hex chars here, but
		// keep a runtime guard so a malformed call (e.g. tests that bypass
		// validation) errors cleanly instead of panicking on the slice.
		if len(spec.SHA256) < archiveSHAPrefixLen {
			return "", "", fmt.Errorf("sha256 too short (%d chars) -- url sources require a 64-hex sha256", len(spec.SHA256))
		}
		archive := filepath.Join(ExternalCacheDir(home), spec.SHA256[:archiveSHAPrefixLen]+"-"+filepath.Base(spec.Source))
		if err := dl(spec.Source, spec.SHA256, archive); err != nil {
			return "", "", fmt.Errorf("download: %w", err)
		}
		// .riced.tmp.unpack matches the project-wide ".riced.tmp.<...>" tmp
		// convention so `riced doctor` and orphan cleanup see this directory
		// uniformly with the rest of the apply package.
		staging := archive + ".riced.tmp.unpack"
		if err := os.RemoveAll(staging); err != nil {
			return "", "", fmt.Errorf("clean staging %s: %w", staging, err)
		}
		if err := os.MkdirAll(staging, 0o755); err != nil {
			return "", "", fmt.Errorf("mkdir staging %s: %w", staging, err)
		}
		if err := extractArchive(archive, staging); err != nil {
			return "", "", fmt.Errorf("extract %s: %w", archive, err)
		}
		return staging, archive, nil
	}
	return "", "", fmt.Errorf("unknown source kind %q", spec.SourceKind)
}

// normalizeRoot returns the directory where the actual theme content
// lives. If `root` contains a single subdirectory and nothing else (the
// github source-tarball shape: <repo>-<tag>/...), descend into it.
func normalizeRoot(root string) string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return root
	}
	if len(entries) != 1 || !entries[0].IsDir() {
		return root
	}
	return filepath.Join(root, entries[0].Name())
}

// detectStrategy picks an install strategy by inspecting the normalized
// source root. Refuses to silently pick "script" -- if an install.sh is
// the only thing present, returns an error asking the user to opt in
// explicitly via install = "script" in the manifest.
func detectStrategy(root string) (string, error) {
	if _, err := os.Stat(filepath.Join(root, "metadata.json")); err == nil {
		return "kpackage", nil
	}
	if _, err := os.Stat(filepath.Join(root, "index.theme")); err == nil {
		return "extract", nil
	}
	// install.sh present without explicit opt-in -- refuse.
	if _, err := os.Stat(filepath.Join(root, "install.sh")); err == nil {
		return "", fmt.Errorf(`found install.sh but install strategy was "auto" -- set install = "script" and script = "install.sh" in the manifest to opt in`)
	}
	return "", fmt.Errorf("no recognizable theme layout in %s (need metadata.json or index.theme at root, or set install = \"script\" / \"kpackage\")", root)
}

// iconsDataDir is the XDG-aware ~/.local/share/icons path for icon and
// cursor themes.
func iconsDataDir(home string) string {
	if x := os.Getenv("XDG_DATA_HOME"); x != "" {
		return filepath.Join(x, "icons")
	}
	return filepath.Join(home, ".local", "share", "icons")
}

// copyTreeOver copies src to dst, recursing into directories. Existing
// files at dst are overwritten. Symlinks are absolutized (see body).
// Used by the "extract" strategy to land theme dirs into ~/.local/share/icons/.
//
// Failure semantics: best-effort. A failure mid-copy leaves whatever
// was already written at dst; the caller (executeLook's "extract") can
// safely re-run because copyTreeOver overwrites existing entries.
func copyTreeOver(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		// Relative symlinks resolve against their parent dir. After copy,
		// dst's parent is different from src's parent, so a relative target
		// would point at the wrong place. Convert to absolute before
		// re-symlinking; absolute targets pass through unchanged.
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(src), target)
		}
		_ = os.Remove(dst)
		return os.Symlink(target, dst)
	case info.IsDir():
		if err := os.MkdirAll(dst, info.Mode().Perm()); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := copyTreeOver(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
				return err
			}
		}
		return nil
	default:
		in, err := os.Open(src)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			_ = out.Close()
			return err
		}
		// Close() is part of the write path on Linux (filesystem flushes
		// happen here); surfacing its error is what CodeQL flags as missing.
		return out.Close()
	}
}

// isUnder reports whether `path` lives somewhere inside `root` (after
// cleaning both). Defense against script paths that try to break out
// via .. or absolute paths.
func isUnder(path, root string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil {
		return false
	}
	return !strings.HasPrefix(rel, "..")
}

// expandEnv replaces $HOME / $XDG_DATA_HOME / $XDG_CONFIG_HOME in a
// script argument. Only those three are honored -- no shell-style
// $(...) command substitution, no arbitrary env variable read.
func expandEnv(s string) string {
	for _, v := range []string{"XDG_DATA_HOME", "XDG_CONFIG_HOME", "HOME"} {
		val := os.Getenv(v)
		if val == "" && v == "XDG_DATA_HOME" {
			val = filepath.Join(os.Getenv("HOME"), ".local", "share")
		}
		if val == "" && v == "XDG_CONFIG_HOME" {
			val = filepath.Join(os.Getenv("HOME"), ".config")
		}
		s = strings.ReplaceAll(s, "$"+v, val)
		s = strings.ReplaceAll(s, "${"+v+"}", val)
	}
	return s
}
