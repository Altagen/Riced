// Package apply implements the side of Riced that actually touches the
// user's KDE Plasma installation. It deals with file copies into
// ~/.config / ~/.local/share, with backup of replaced files, with
// invoking plasma-apply-* and qdbus helpers, and with persisting state
// under ~/.riced/state/.
//
// Every operation is gated by an explicit caller intent. The package
// never decides on its own to overwrite a live config file -- the CLI
// layer is responsible for confirming with the user.
package apply

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Altagen/Riced/internal/manifest"
	"github.com/Altagen/Riced/internal/render"
)

// Action is one atomic step in an apply Plan. The CLI prints these to the
// user before asking for confirmation, the executor walks them in order.
type Action struct {
	// Kind is one of: "copy", "symlink", "mkdir", "kde".
	Kind string

	// Src is the source path on disk for copy/symlink actions, or the
	// command name + arg string for KDE actions ("plasma-apply-colorscheme
	// s4-dark"). For multi-argument KDE actions, Src holds the verb and
	// Args holds the parameters separately (INI keys/values often contain
	// spaces, which forbids re-packing them into Src).
	Src string

	// Dst is the destination path that will be written/created. Empty for
	// KDE actions.
	Dst string

	// Args carries parameters for KDE actions whose verb takes more than
	// one argument (e.g. kwriteconfig6 wants file/group/key/value). Empty
	// for non-kde actions and single-argument KDE actions.
	Args []string

	// PreexistingBackupable is true when Dst already exists as a regular
	// file before apply runs. The executor will copy it to the backup dir.
	PreexistingBackupable bool
}

// Plan is the full sequence of Actions a single `riced apply` will execute.
// It's computed up front so a) the user can review it before consenting
// and b) `--dry-run` can print it without side effects.
type Plan struct {
	Slug      string
	Repo      string // optional repository name, empty when from ~/.riced/themes
	BuildDir  string // where generate dropped its output
	Actions   []Action
	StatePath string // ~/.riced/state/current.toml
	BackupDir string // ~/.riced/state/backup/<timestamp>/
}

// Targets is the mapping from a generated artifact (relative to BuildDir)
// to its live KDE location. Kept here rather than inside the renderers so
// the renderers stay pure: they don't need to know where files end up.
//
// DataHome and ConfigHome honor the XDG Base Directory spec
// ($XDG_DATA_HOME, $XDG_CONFIG_HOME). Use NewTargets to construct one
// with the env-derived defaults; tests that want a fully sandboxed tree
// can set the fields explicitly.
type Targets struct {
	HomeDir    string // typically os.UserHomeDir; used only when DataHome/ConfigHome are empty
	DataHome   string // $XDG_DATA_HOME or $HOME/.local/share
	ConfigHome string // $XDG_CONFIG_HOME or $HOME/.config
}

// NewTargets returns a Targets with DataHome and ConfigHome resolved from
// the XDG env vars (falling back to $HOME/.local/share and $HOME/.config).
// This is the production constructor; pass the user's home dir.
func NewTargets(homeDir string) Targets {
	return Targets{
		HomeDir:    homeDir,
		DataHome:   xdgDataHome(homeDir),
		ConfigHome: xdgConfigHome(homeDir),
	}
}

// xdgDataHome returns $XDG_DATA_HOME if set, else $HOME/.local/share.
// Spec: https://specifications.freedesktop.org/basedir-spec/.
func xdgDataHome(homeDir string) string {
	if x := os.Getenv("XDG_DATA_HOME"); x != "" {
		return x
	}
	return filepath.Join(homeDir, ".local", "share")
}

// xdgConfigHome returns $XDG_CONFIG_HOME if set, else $HOME/.config.
func xdgConfigHome(homeDir string) string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return x
	}
	return filepath.Join(homeDir, ".config")
}

// dataHome returns the resolved data-home, falling back to HomeDir-based
// default if neither DataHome nor an env var set it. Defensive: tests
// often construct Targets{HomeDir: ...} only.
func (t Targets) dataHome() string {
	if t.DataHome != "" {
		return t.DataHome
	}
	return xdgDataHome(t.HomeDir)
}

func (t Targets) configHome() string {
	if t.ConfigHome != "" {
		return t.ConfigHome
	}
	return xdgConfigHome(t.HomeDir)
}

// Map returns the absolute destination path for a build-relative source.
// Returns false when the artifact is not yet integrated into the apply
// layer (e.g. the Plasma wallpaper.ini descriptor -- see comments).
func (t Targets) Map(slug, relSrc string) (string, bool) {
	dataHome := t.dataHome()
	configHome := t.configHome()
	switch {
	case relSrc == slug+".colors":
		return filepath.Join(dataHome, "color-schemes", slug+".colors"), true

	case relSrc == filepath.Join("konsole", slug+".colorscheme"):
		return filepath.Join(dataHome, "konsole", slug+".colorscheme"), true

	case relSrc == filepath.Join("konsole", slug+".profile"):
		return filepath.Join(dataHome, "konsole", slug+".profile"), true

	case relSrc == filepath.Join("gtk-3.0", "gtk.css"):
		return filepath.Join(configHome, "gtk-3.0", "gtk.css"), true

	case relSrc == filepath.Join("gtk-4.0", "gtk.css"):
		return filepath.Join(configHome, "gtk-4.0", "gtk.css"), true

	case relSrc == "wallpaper.ini":
		// Not directly applied in Phase 8 v1 -- the apply layer integrates
		// wallpapers via plasma-apply-wallpaperimage (single mode) or via a
		// user-facing instruction (slideshow mode). The descriptor stays in
		// the cache for inspection.
		return "", false

	case strings.HasPrefix(relSrc, "wallpapers"+string(filepath.Separator)):
		// Wallpaper symlinks land in a Riced-managed dir so apply/revert
		// can scrub them cleanly. Preserve any subdirectory structure
		// under wallpapers/ (per-screen layout uses wallpapers/screen-N/).
		subpath := strings.TrimPrefix(relSrc, "wallpapers"+string(filepath.Separator))
		return filepath.Join(dataHome, "riced", "wallpapers", slug, subpath), true

	case strings.HasPrefix(relSrc, "icons"+string(filepath.Separator)):
		// Launcher icon (and future per-slug icons) land in a Riced-
		// managed dir so the Plasma JS API can point at a stable path.
		// We prefix with the slug to avoid collisions when several themes
		// are kept side-by-side under DataHome/riced/.
		base := filepath.Base(relSrc)
		return filepath.Join(dataHome, "riced", "icons", slug+"-"+base), true

	case strings.HasPrefix(relSrc, "lock"+string(filepath.Separator)):
		// Lock screen wallpaper. Single file ("lock.<ext>") prefixed with
		// the slug at the destination so multiple themes coexist cleanly.
		base := filepath.Base(relSrc)
		return filepath.Join(dataHome, "riced", "lock", slug+"-"+base), true
	}
	return "", false
}

// Build walks the generated files in buildDir and produces a Plan whose
// Actions cover: directory creation, file copies (with backup detection),
// and KDE side-effects (plasma-apply-colorscheme + qdbus reconfigure).
//
// buildDir must contain the output of `riced generate` for slug; targets
// resolves home-relative paths.
//
// prev (optional) is the State from a previous apply. When provided, files
// already in prev.WrittenAt are NOT marked PreexistingBackupable: they
// are Riced's own output from the previous run, and re-backing them up
// would silently overwrite the genuine user originals captured in prev's
// BackupRoot. Pass nil on a fresh install.
func Build(m *manifest.Manifest, buildDir string, targets Targets, statePath, backupDir string, prev *State) (*Plan, error) {
	plan := &Plan{
		Slug:      m.Meta.Slug,
		BuildDir:  buildDir,
		StatePath: statePath,
		BackupDir: backupDir,
	}

	// Index of paths already owned by Riced (written by a previous apply).
	// We must not back these up: their on-disk content is our own output,
	// and the real user originals live in prev.BackupRoot from that run.
	ricedOwned := map[string]struct{}{}
	if prev != nil {
		for _, p := range prev.WrittenAt {
			ricedOwned[p] = struct{}{}
		}
	}

	// Walk the build output and pair each file with its live destination.
	type entry struct{ src, dst string }
	var entries []entry
	var launcherIconDst string   // remembered for kde set-launcher-icon
	var lockWallpaperDst string  // remembered for kde lock-screen wallpaper action
	var firstWallpaperDst string // remembered for plasma-apply-wallpaperimage (single mode)

	walkErr := filepath.WalkDir(buildDir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(buildDir, p)
		dst, ok := targets.Map(m.Meta.Slug, rel)
		if !ok {
			return nil // skip artifacts not yet integrated
		}
		// Capture the launcher icon destination so we can later append a
		// kde action telling Plasma which file to point at.
		if filepath.Dir(rel) == "icons" && strings.HasPrefix(filepath.Base(rel), "launcher") {
			launcherIconDst = dst
		}
		// Capture the lock-screen wallpaper destination for kscreenlockerrc.
		if filepath.Dir(rel) == "lock" && strings.HasPrefix(filepath.Base(rel), "lock") {
			lockWallpaperDst = dst
		}
		// Capture the first wallpaper's destination. WalkDir is lexical,
		// and materializeWallpapers names symlinks "01-…", "02-…", so the
		// first hit lines up with Manifest.Wallpapers.Paths[0] -- which is
		// what plasma-apply-wallpaperimage wants in single mode.
		if firstWallpaperDst == "" && filepath.Dir(rel) == "wallpapers" {
			firstWallpaperDst = dst
		}
		entries = append(entries, entry{src: p, dst: dst})
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("walk build dir %s: %w", buildDir, walkErr)
	}

	// Stable ordering: directories first (deduped + sorted), then files.
	// Avoids "mkdir A/B before A" and keeps Plan.Format deterministic so
	// the user always sees the same plan for the same inputs.
	dirSet := map[string]struct{}{}
	for _, e := range entries {
		dirSet[filepath.Dir(e.dst)] = struct{}{}
	}
	dirs := make([]string, 0, len(dirSet))
	for d := range dirSet {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	for _, dir := range dirs {
		plan.Actions = append(plan.Actions, Action{Kind: "mkdir", Dst: dir})
	}

	for _, e := range entries {
		info, err := os.Lstat(e.src)
		if err != nil {
			return nil, fmt.Errorf("lstat %s: %w", e.src, err)
		}
		kind := "copy"
		if info.Mode()&os.ModeSymlink != 0 {
			kind = "symlink"
		}
		pre := isRegularFile(e.dst)
		if _, owned := ricedOwned[e.dst]; owned {
			// Riced wrote this on a previous apply -- not a user original.
			pre = false
		}
		plan.Actions = append(plan.Actions, Action{
			Kind:                  kind,
			Src:                   e.src,
			Dst:                   e.dst,
			PreexistingBackupable: pre,
		})
	}

	// Looks (external theme assets) first: subsequent actions
	// (lookandfeel, apply-desktop-theme, [icons].theme) may reference
	// names this Looks loop installs.
	//
	// Action.Args encoding (positional):
	//   Args[0]   = type (Plasma/LookAndFeel | icons | ...)
	//   Args[1]   = source ("url:<url>" or "local:<name>")
	//   Args[2]   = sha256 ("" for local)
	//   Args[3]   = install strategy (auto | kpackage | extract | script)
	//   Args[4]   = script relative path ("" unless install=script)
	//   Args[5:]  = script args (env-expanded at apply time)
	for _, p := range m.Looks {
		install := p.Install
		if install == "" {
			install = "auto"
		}
		source := "url:" + p.URL
		if p.Local != "" {
			source = "local:" + p.Local
		}
		args := []string{p.Type, source, p.SHA256, install, p.Script}
		args = append(args, p.Args...)
		plan.Actions = append(plan.Actions, Action{
			Kind: "external",
			Src:  p.Name,
			Args: args,
		})
	}

	// Global Look & Feel BEFORE individual overrides. plasma-apply-lookandfeel
	// resets colorscheme / cursor / decoration / plasma theme / icons in one
	// shot, so the per-field actions that follow must be able to override
	// whatever look-and-feel defaults to.
	if m.LookAndFeel.Package != "" {
		plan.Actions = append(plan.Actions, Action{
			Kind: "kde",
			Src:  "apply-lookandfeel " + m.LookAndFeel.Package,
		})
	}

	// KDE side-effects. Wallpaper handling is conditional on mode.
	plan.Actions = append(plan.Actions, Action{
		Kind: "kde",
		Src:  "plasma-apply-colorscheme " + m.Meta.Slug,
	})
	plan.Actions = append(plan.Actions, Action{
		Kind: "kde",
		Src:  "kwriteconfig6-konsole-default " + m.Meta.Slug + ".profile",
	})

	// KWin + Klassy settings -- one kwriteconfig6 action per INI entry.
	// Keeping them as separate actions makes the plan auditable: the user
	// sees every key we touch before consenting.
	for _, e := range render.KWinSettings(m) {
		plan.Actions = append(plan.Actions, Action{
			Kind: "kde",
			Src:  "kwriteconfig6",
			Args: []string{e.File, e.Group, e.Key, e.Value},
		})
	}
	for _, e := range render.KlassySettings(m) {
		plan.Actions = append(plan.Actions, Action{
			Kind: "kde",
			Src:  "kwriteconfig6",
			Args: []string{e.File, e.Group, e.Key, e.Value},
		})
	}
	for _, e := range render.KvantumSettings(m) {
		plan.Actions = append(plan.Actions, Action{
			Kind: "kde",
			Src:  "kwriteconfig6",
			Args: []string{e.File, e.Group, e.Key, e.Value},
		})
	}

	if launcherIconDst != "" {
		plan.Actions = append(plan.Actions, Action{
			Kind: "kde",
			Src:  "set-launcher-icon " + launcherIconDst,
		})
	}

	// Lock screen wallpaper: kscreenlockerrc with deeply nested groups.
	// WriteINIKey splits Group on "/" into multiple --group flags.
	if lockWallpaperDst != "" {
		plan.Actions = append(plan.Actions, Action{
			Kind: "kde",
			Src:  "kwriteconfig6",
			Args: []string{
				"kscreenlockerrc",
				"Greeter/Wallpaper/org.kde.image/General",
				"Image",
				lockWallpaperDst,
			},
		})
	}

	// System-wide icon theme. Single kwriteconfig6 on kdeglobals.
	if m.Icons.Theme != "" {
		plan.Actions = append(plan.Actions, Action{
			Kind: "kde",
			Src:  "kwriteconfig6",
			Args: []string{"kdeglobals", "Icons", "Theme", m.Icons.Theme},
		})
	}

	// Cursor theme. plasma-apply-cursortheme switches it live and writes
	// the appropriate config files for new sessions.
	if m.Cursors.Theme != "" {
		plan.Actions = append(plan.Actions, Action{
			Kind: "kde",
			Src:  "apply-cursor-theme " + m.Cursors.Theme,
		})
	}

	// Plasma desktop "style" (panel widgets, popups). plasma-apply-desktoptheme
	// resolves the theme name against installed Plasma/Theme packages.
	if m.Plasma.DesktopTheme != "" {
		plan.Actions = append(plan.Actions, Action{
			Kind: "kde",
			Src:  "apply-desktop-theme " + m.Plasma.DesktopTheme,
		})
	}

	if m.Wallpapers.Mode == "single" && firstWallpaperDst != "" {
		// Use the materialized symlink path, NOT the raw manifest path.
		// The raw path is relative to the theme dir (or absolute when
		// inherited); Plasma needs an absolute path it can resolve
		// post-apply, which is the install location we just decided on.
		plan.Actions = append(plan.Actions, Action{
			Kind: "kde",
			Src:  "plasma-apply-wallpaperimage " + firstWallpaperDst,
		})
	} else if m.Wallpapers.Mode == "slideshow" {
		interval := m.Wallpapers.Interval
		if interval == 0 {
			interval = 600 // 10 min, matches Plasma's own default
		}
		if len(m.Wallpapers.Screens) > 0 {
			// Per-screen: one slideshow action carrying every rule in
			// manifest order. Each Args entry encodes a rule as
			// "<kind>:<value>:<path>", parsed by dispatchKDE. The JS at
			// apply time evaluates rules per desktop (first match wins).
			wallpaperRoot := filepath.Join(targets.dataHome(), "riced", "wallpapers", m.Meta.Slug)
			args := make([]string, 0, len(m.Wallpapers.Screens))
			for _, s := range m.Wallpapers.Screens {
				kind, value := s.CriterionKind()
				path := filepath.Join(wallpaperRoot, s.SubdirName())
				args = append(args, fmt.Sprintf("%s:%s:%s", kind, value, path))
			}
			plan.Actions = append(plan.Actions, Action{
				Kind: "kde",
				Src:  fmt.Sprintf("set-wallpaper-slideshow %d", interval),
				Args: args,
			})
		} else if firstWallpaperDst != "" {
			// Flat layout (legacy or explicit mirror): one rule with the
			// catch-all "*" criterion, pointing at the shared dir.
			wallpapersDir := filepath.Dir(firstWallpaperDst)
			plan.Actions = append(plan.Actions, Action{
				Kind: "kde",
				Src:  fmt.Sprintf("set-wallpaper-slideshow %d", interval),
				Args: []string{"match:*:" + wallpapersDir},
			})
		}
	}
	plan.Actions = append(plan.Actions, Action{
		Kind: "kde",
		// QDBusBin is resolved at package init (qdbus6 or qdbus). The
		// dispatcher matches on the trailing path so either form works;
		// using the resolved name here keeps the displayed plan honest
		// about what will actually be exec'd.
		Src: QDBusBin + " org.kde.KWin /KWin reconfigure",
	})
	return plan, nil
}

func isRegularFile(p string) bool {
	info, err := os.Lstat(p)
	if err != nil {
		return false
	}
	return info.Mode().IsRegular()
}

// Format renders the Plan as a human-readable string for display before
// confirmation.
func (p *Plan) Format() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Apply plan for %q:\n", p.Slug)
	fmt.Fprintf(&b, "  build:  %s\n", p.BuildDir)
	fmt.Fprintf(&b, "  backup: %s\n\n", p.BackupDir)

	var copies, symlinks, kdeCalls int
	for _, a := range p.Actions {
		switch a.Kind {
		case "mkdir":
			continue
		case "copy", "symlink":
			tag := ""
			if a.PreexistingBackupable {
				tag = " (overwrite, backup made)"
			}
			fmt.Fprintf(&b, "  %s  %s%s\n", a.Kind, a.Dst, tag)
			if a.Kind == "copy" {
				copies++
			} else {
				symlinks++
			}
		case "kde":
			if len(a.Args) > 0 {
				fmt.Fprintf(&b, "  kde     %s %s\n", a.Src, strings.Join(a.Args, " "))
			} else {
				fmt.Fprintf(&b, "  kde     %s\n", a.Src)
			}
			kdeCalls++
		}
	}
	fmt.Fprintf(&b, "\n%d file(s), %d symlink(s), %d KDE call(s).\n", copies, symlinks, kdeCalls)
	return b.String()
}
