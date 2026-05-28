package apply

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ExecOptions configures Execute. Most fields are optional -- sensible
// defaults take over when they're zero values.
type ExecOptions struct {
	// KDE is the side-effect adapter. Required for kind="kde" actions to
	// actually do anything; pass a *FakeKDE in tests.
	KDE KDE

	// Now is the wallclock used to stamp the backup directory and the
	// state file. Default time.Now.UTC().
	Now func() time.Time

	// Download fetches [[looks]] archives (url source) + verifies their
	// SHA-256. Default DefaultDownload (HTTPS + io.Copy). Tests set a
	// stub to avoid network calls.
	Download ExternalDownloader

	// HomeDir is the user's home dir used to compute the look download
	// cache path and the ~/.riced/looks/ local-source directory.
	// Default os.UserHomeDir().
	HomeDir string
}

// Execute walks the Plan, runs every Action, backs up overwritten files,
// and persists a fresh State.toml.
//
// Failure semantics: the first error aborts the loop and is returned.
// Files already copied stay on disk -- Riced does not attempt a partial
// rollback. BUT the state file IS still written (via defer) with the
// list of paths actually written before the error, so `riced revert` can
// clean them up afterwards. Without this, a half-failed apply would
// strand the user with orphan files no command knew about.
func Execute(plan *Plan, opts ExecOptions) (err error) {
	now := opts.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}

	var written []string
	var backedUp bool

	defer func() {
		// Persist whatever was actually written, even on failure. The
		// state file becomes "what's on disk because of Riced", not
		// "what a successful apply looks like". Revert reads this exact
		// list when it walks WrittenAt.
		if len(written) == 0 {
			return
		}
		// BackupRoot tracking:
		//   - We backed up new files this run -> point at plan.BackupDir.
		//   - We backed up nothing this run, but plan.BackupDir already
		//     exists on disk -> the caller carried over a previous run's
		//     BackupRoot (sticky reuse). Preserve the reference so revert
		//     can still find originals.
		//   - Nothing backed up and no dir exists -> empty, doctor stops
		//     flagging a phantom "missing backup root".
		backupRoot := ""
		if backedUp {
			backupRoot = plan.BackupDir
		} else if _, err := os.Stat(plan.BackupDir); err == nil {
			backupRoot = plan.BackupDir
		}
		state := &State{
			Slug:       plan.Slug,
			Repo:       plan.Repo,
			AppliedAt:  now(),
			WrittenAt:  append([]string(nil), written...),
			BackupRoot: backupRoot,
		}
		sort.Strings(state.WrittenAt)
		if saveErr := state.Save(plan.StatePath); saveErr != nil {
			// Don't shadow a real Execute error. If the loop already
			// reported one, keep that and just log the secondary
			// failure -- the user can recover the files via the names
			// in the slog stream even if state.toml didn't persist.
			if err == nil {
				err = fmt.Errorf("save state: %w", saveErr)
			} else {
				slog.Warn("failed to persist partial state after error", "save_err", saveErr, "primary_err", err)
			}
		}
	}()

	for _, a := range plan.Actions {
		switch a.Kind {
		case "mkdir":
			if err := os.MkdirAll(a.Dst, 0o755); err != nil {
				return fmt.Errorf("mkdir %s: %w", a.Dst, err)
			}
		case "copy":
			if a.PreexistingBackupable {
				if _, err := backupFile(a.Dst, plan.BackupDir); err != nil {
					return fmt.Errorf("backup %s: %w", a.Dst, err)
				}
				backedUp = true
				slog.Debug("backed up", "path", a.Dst)
			}
			if err := copyRegular(a.Src, a.Dst); err != nil {
				return err
			}
			written = append(written, a.Dst)
			slog.Debug("copied", "dst", a.Dst)
		case "symlink":
			// Symlinks always replace -- backing them up isn't useful (the
			// target may not even exist post-revert), and the curated
			// wallpapers dir is fully Riced-managed.
			//
			// Atomic dance: create the link under a tmp name first, then
			// rename. That guarantees the destination is *always* either
			// the old link or the new one, never absent.
			//
			// FALLBACK: when a.Src is a regular file (no symlink to read),
			// we synthesize the target by absolutizing the path. This is
			// only meaningful in tests that hand the executor a regular
			// file in place of a real symlink -- production code always
			// goes through materialize* helpers which produce symlinks.
			// Surface a Warn so a real-prod fallback reveals a Build() bug
			// instead of staying hidden behind the test-only fallback.
			target, err := os.Readlink(a.Src)
			if err != nil {
				abs, aerr := filepath.Abs(a.Src)
				if aerr != nil {
					return fmt.Errorf("read symlink %s: %w", a.Src, err)
				}
				slog.Warn("symlink action source is not a symlink, falling back to absolute path -- bug in Build() if seen in production",
					"src", a.Src, "abs", abs)
				target = abs
			}
			tmp := a.Dst + tmpSuffix()
			_ = os.Remove(tmp) // clean any leftover from a previous crash
			if err := os.Symlink(target, tmp); err != nil {
				return fmt.Errorf("symlink %s -> %s: %w", tmp, target, err)
			}
			if err := os.Rename(tmp, a.Dst); err != nil {
				_ = os.Remove(tmp)
				return fmt.Errorf("rename %s -> %s: %w", tmp, a.Dst, err)
			}
			written = append(written, a.Dst)
			slog.Debug("symlinked", "dst", a.Dst, "target", target)
		case "kde":
			if opts.KDE == nil {
				return fmt.Errorf("kde action %q requires opts.KDE; pass RealKDE in production or FakeKDE in tests", a.Src)
			}
			if err := dispatchKDE(opts.KDE, a); err != nil {
				return err
			}
			slog.Info("KDE", "call", a.Src, "args", a.Args)
		case "external":
			if opts.KDE == nil {
				return fmt.Errorf("external action %q requires opts.KDE; pass RealKDE in production or FakeKDE in tests", a.Src)
			}
			if err := executeExternal(a, opts); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unknown action kind %q", a.Kind)
		}
	}

	// On success the defer above persists state. Nothing more to do.
	return nil
}

// executeExternal handles Kind="external" actions. It parses the
// positional Args produced by plan.Build, resolves the source (URL
// download or local clone), normalizes the root, picks a strategy
// (auto or explicit), and dispatches to executeLook.
//
// Action shape:
//
//	Src     = look name (free-form label for plan display)
//	Args[0] = type (Plasma/LookAndFeel | icons | ...)
//	Args[1] = source ("url:<url>" or "local:<name>")
//	Args[2] = sha256 ("" for local)
//	Args[3] = install strategy
//	Args[4] = script relative path ("" unless install=script)
//	Args[5:] = script args
func executeExternal(a Action, opts ExecOptions) error {
	spec, err := parseLookArgs(a.Args)
	if err != nil {
		return fmt.Errorf("external %q: %w", a.Src, err)
	}
	return executeLook(spec, a.Src, opts)
}

// dispatchKDE routes a kde Action to the right KDE adapter method.
//
// The dispatcher reads Action.Src as the verb and Action.Args (if set) as
// the parameters. Single-argument actions keep the arg packed into Src
// after a space -- backward-compat with earlier code paths.
//
// CASES ARE ORDER-SENSITIVE: if you add a new verb whose name is a
// prefix of an existing one, list yours FIRST (strings.HasPrefix would
// otherwise shadow it). The current set has no collisions; the
// kwriteconfig6-konsole-default case sits before the kwriteconfig6
// exact-match case and works because the exact-match check guards
// against the prefix overlap. Keep this structure when adding new
// actions. A future refactor to enum-based verbs (0.2.0) would remove
// the order requirement; for 0.1.x we stick with the prefix pattern.
func dispatchKDE(kde KDE, a Action) error {
	switch {
	case strings.HasPrefix(a.Src, "plasma-apply-colorscheme "):
		return kde.ApplyColorScheme(a.Src[len("plasma-apply-colorscheme "):])
	case strings.HasPrefix(a.Src, "plasma-apply-wallpaperimage "):
		return kde.SetWallpaperImage(a.Src[len("plasma-apply-wallpaperimage "):])
	case strings.HasPrefix(a.Src, "kwriteconfig6-konsole-default "):
		return kde.SetKonsoleDefaultProfile(a.Src[len("kwriteconfig6-konsole-default "):])
	case a.Src == "kwriteconfig6" && len(a.Args) == 4:
		return kde.WriteINIKey(a.Args[0], a.Args[1], a.Args[2], a.Args[3])
	case strings.HasPrefix(a.Src, "set-launcher-icon "):
		return kde.SetLauncherIcon(a.Src[len("set-launcher-icon "):])
	case strings.HasPrefix(a.Src, "apply-cursor-theme "):
		return kde.ApplyCursorTheme(a.Src[len("apply-cursor-theme "):])
	case strings.HasPrefix(a.Src, "apply-desktop-theme "):
		return kde.ApplyDesktopTheme(a.Src[len("apply-desktop-theme "):])
	case strings.HasPrefix(a.Src, "apply-lookandfeel "):
		return kde.ApplyLookAndFeel(a.Src[len("apply-lookandfeel "):])
	case strings.HasPrefix(a.Src, "set-wallpaper-slideshow "):
		// Src format: "set-wallpaper-slideshow <interval>". Each Args
		// entry is one rule encoded as "<kind>:<value>:<path>", in
		// priority order (manifest order).
		intervalStr := strings.TrimPrefix(a.Src, "set-wallpaper-slideshow ")
		interval, err := strconv.Atoi(intervalStr)
		if err != nil {
			return fmt.Errorf("invalid slideshow interval %q: %w", intervalStr, err)
		}
		rules := make([]SlideshowRule, 0, len(a.Args))
		for _, raw := range a.Args {
			parts := strings.SplitN(raw, ":", 3)
			if len(parts) != 3 {
				return fmt.Errorf("malformed slideshow rule %q (want kind:value:path)", raw)
			}
			rules = append(rules, SlideshowRule{Kind: parts[0], Value: parts[1], Path: parts[2]})
		}
		return kde.SetWallpaperSlideshow(rules, interval)
	case strings.HasSuffix(a.Src, " org.kde.KWin /KWin reconfigure"):
		// Match the trailing path so the resolved qdbus binary (qdbus6
		// or qdbus, see prereqs.QDBusBin) routes to ReconfigureKWin.
		return kde.ReconfigureKWin()
	}
	return fmt.Errorf("unrecognized KDE action %q (args=%v)", a.Src, a.Args)
}

// Revert undoes the last apply. It does so in two passes:
//
//  1. For each file in State.WrittenAt, remove it. If the file does not
//     exist or has been replaced by another tool, the entry is skipped
//     with a warning.
//  2. For each file in BackupRoot, restore it to its original location
//     (computed by mirroring the path under BackupRoot back to /).
//
// The state file itself is removed on success.
//
// SCOPE: Revert is *file-level only*. It does NOT replay plasma-apply-*
// to restore the previous color scheme, cursor, decoration, etc. Plasma
// keeps whatever was applied last (so the visual state remains the just-
// undone theme) until the user applies another theme or restarts the
// session. Adding a "remember previous Plasma state" layer is on the
// roadmap; until then, document this expectation to callers.
func Revert(homeDir string) error {
	statePath := StatePath(homeDir)
	state, err := LoadState(statePath)
	if err != nil {
		return err
	}
	if state == nil {
		return fmt.Errorf("nothing to revert: no state file at %s", statePath)
	}

	// Pass 1: remove Riced-written files, then prune empty Riced-managed
	// dirs in one batched walk at the end. We collect any real (non-not-
	// exist) failures rather than swallowing them -- the user must hear
	// that revert didn't fully succeed instead of seeing a reassuring
	// "reverted" log.
	var removeFailures []string
	parentsToPrune := map[string]struct{}{}
	for _, p := range state.WrittenAt {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			slog.Warn("remove failed during revert", "path", p, "err", err)
			removeFailures = append(removeFailures, p)
			continue
		}
		parentsToPrune[filepath.Dir(p)] = struct{}{}
	}
	// Sort by depth (deepest first) so child empty-dirs disappear before
	// we try their parents. pruneEmptyRicedDirs stops at non-empty, so
	// processing leaves-first lets a chain of empty parents collapse.
	dirs := make([]string, 0, len(parentsToPrune))
	for d := range parentsToPrune {
		dirs = append(dirs, d)
	}
	sort.Slice(dirs, func(i, j int) bool {
		return strings.Count(dirs[i], string(filepath.Separator)) > strings.Count(dirs[j], string(filepath.Separator))
	})
	for _, d := range dirs {
		pruneEmptyRicedDirs(d + string(filepath.Separator)) // pruneEmptyRicedDirs takes a file path; pass dir/<sentinel>
	}

	// Pass 2: restore from backup, if any.
	if state.BackupRoot != "" {
		err := filepath.WalkDir(state.BackupRoot, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			restored, err := restoreFile(p, state.BackupRoot)
			if err != nil {
				return err
			}
			slog.Debug("restored", "path", restored)
			return nil
		})
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("restore from %s: %w", state.BackupRoot, err)
		}
	}

	// Only after the restore pass do we surface accumulated remove
	// failures: restoration may have repaired some of them (overwriting
	// the file we failed to remove with the backed-up content).
	if len(removeFailures) > 0 {
		return fmt.Errorf("revert finished with %d unremovable file(s): %v", len(removeFailures), removeFailures)
	}

	if err := os.Remove(statePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove state file: %w", err)
	}
	return nil
}
