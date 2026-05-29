package apply

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// kdeCallTimeout caps how long any single KDE-side exec is allowed to
// block. plasmashell can occasionally hang on a malformed JS script or
// while indexing; we'd rather fail the action than freeze `riced apply`.
const kdeCallTimeout = 30 * time.Second

// KDE is the side-effect surface for everything that talks to the live
// KDE Plasma session. Implementations exist for the real system (uses
// os/exec) and for tests (records calls, runs nothing).
//
// A non-nil error from any method must abort the apply -- we never
// continue past a half-applied state silently.
type KDE interface {
	// ApplyColorScheme installs the color scheme with the given slug name
	// and asks Plasma to switch to it. The .colors file must already exist
	// under ~/.local/share/color-schemes/<slug>.colors.
	ApplyColorScheme(slug string) error

	// SetWallpaperImage points Plasma's wallpaper at a single image (the
	// org.kde.image plugin). Slideshow mode is handled via
	// SetWallpaperSlideshow.
	SetWallpaperImage(path string) error

	// SetWallpaperSlideshow points the org.kde.slideshow plugin at a
	// list of rules, in priority order. The JS at apply time scans each
	// desktop and assigns it the first rule that matches (CSS cascade).
	// Desktops claimed by no rule keep their previous wallpaper.
	SetWallpaperSlideshow(rules []SlideshowRule, intervalSec int) error

	// SetKonsoleDefaultProfile rewrites the DefaultProfile key in
	// ~/.config/konsolerc so that new Konsole windows open with this
	// profile by default. The argument is the profile filename including
	// the .profile extension (Konsole stores the bare filename, not a
	// path).
	SetKonsoleDefaultProfile(profileFilename string) error

	// WriteINIKey is the generic kwriteconfig6 escape hatch: it writes
	// (or replaces) one key in a KDE-style INI file under ~/.config/. The
	// file argument is the *basename* (e.g. "kwinrc"), not a path --
	// kwriteconfig6 resolves XDG locations itself.
	WriteINIKey(file, group, key, value string) error

	// SetLauncherIcon replaces the icon of every launcher applet
	// (kickoff / kicker / kickerdash) in every Plasma panel.
	//
	// Implemented via Plasma's scripting API rather than by parsing
	// plasma-org.kde.plasma.desktop-appletsrc: the appletsrc has dynamic
	// containment/applet IDs that depend on the user's exact panel
	// layout, and the JS API is what Plasma itself uses to manage them.
	// Robust across panel reshuffles.
	SetLauncherIcon(iconPath string) error

	// ReconfigureKWin nudges KWin to re-read its config after we edit
	// files it cares about.
	ReconfigureKWin() error

	// ApplyCursorTheme switches the system-wide cursor theme. The argument
	// is the theme directory name (e.g. "capitaine-cursors"). The theme
	// must already be installed under /usr/share/icons/ or
	// ~/.local/share/icons/.
	ApplyCursorTheme(name string) error

	// ApplyDesktopTheme switches the Plasma "desktop theme" (panel widgets,
	// popups, notifications). The argument is the theme name as listed by
	// `kpackagetool6 -t Plasma/Theme --list`.
	ApplyDesktopTheme(name string) error

	// ApplyLookAndFeel switches Plasma's Global Theme via
	// `plasma-apply-lookandfeel -a <package>`. This resets colorscheme,
	// cursor, decoration, plasma theme, and icons in one shot, so callers
	// must invoke it BEFORE any individual override that should win.
	ApplyLookAndFeel(packageID string) error

	// KPackageInstall registers a KPackage source (.tar.gz / .tar.xz or a
	// directory containing metadata.json) via kpackagetool6. pkgType
	// matches manifest's looks.type for the kpackage strategies
	// (Plasma/*, KWin/*). Falls back to -u upgrade when -i errors on
	// already-installed.
	KPackageInstall(pkgType, sourcePath string) error

	// SetPanelGeometry updates location ("top"/"bottom"/"left"/"right"),
	// floating, and height (px) across every Plasma 6 panel via the
	// plasmashell scripting API. Fields with zero values are ignored, so
	// the manifest's `[panel].position` / `floating` / `height` can be
	// mixed-and-matched freely.
	SetPanelGeometry(location string, floating bool, height int) error

	// RefreshSystemCache rebuilds KDE's ksycoca service cache via
	// `kbuildsycoca6 --noincremental`. Called after a write that changes
	// what KDE thinks is installed (e.g. icon theme switch in kdeglobals);
	// without this newly-launched apps may keep serving the previous
	// theme until the next login.
	RefreshSystemCache() error
}

// RealKDE shells out to the canonical KDE helpers. Used by `riced apply`
// on a real system.
type RealKDE struct {
	// Stderr receives the helpers' own stderr. Useful so users see what
	// plasma-apply-* says.
	Stderr *os.File
}

func (k RealKDE) ApplyColorScheme(slug string) error {
	return k.run("plasma-apply-colorscheme", slug)
}

func (k RealKDE) SetWallpaperImage(path string) error {
	return k.run("plasma-apply-wallpaperimage", path)
}

// SlideshowRule is one match-criterion → wallpaper-dir binding inside
// a SetWallpaperSlideshow call. The dispatcher parses these from the
// kde Action's Args strings.
//
// Kind values: "idx" (Value = decimal screen number), "orient"
// (Value = "vertical"|"horizontal"), "match" (Value = "*").
type SlideshowRule struct {
	Kind  string
	Value string
	Path  string
}

// wallpaperSlideshowScript is the Plasma 6 JS sent to plasmashell. It
// walks every desktop and, for each, finds the first rule that matches
// (manifest order = CSS cascade). When a match is found, the desktop is
// switched to org.kde.slideshow with the rule's path. Unmatched
// desktops keep whatever wallpaper they had.
//
// Placeholders: "%s" = JS array literal of rules, "%d" = interval seconds.
const wallpaperSlideshowScript = `
var rules = %s;
var interval = %d;
var ds = desktops();
var changed = 0;
for (var i = 0; i < ds.length; i++) {
    var d = ds[i];
    var matched = null;
    for (var k = 0; k < rules.length; k++) {
        var r = rules[k];
        if (r.kind === "idx" && d.screen === parseInt(r.value, 10)) { matched = r; break; }
        if (r.kind === "orient") {
            var g = (typeof screenGeometry === "function") ? screenGeometry(d.screen) : null;
            var isVert = (g && g.height > g.width);
            if ((r.value === "vertical" && isVert) || (r.value === "horizontal" && g && !isVert)) {
                matched = r; break;
            }
        }
        if (r.kind === "match" && r.value === "*") { matched = r; break; }
    }
    if (!matched) continue;
    d.wallpaperPlugin = "org.kde.slideshow";
    d.currentConfigGroup = ["Wallpaper", "org.kde.slideshow", "General"];
    d.writeConfig("SlidePaths", [matched.path]);
    d.writeConfig("SlideInterval", interval);
    d.writeConfig("FillMode", 2);
    d.reloadConfig();
    changed++;
}
print("riced wallpaper-slideshow: " + changed + " desktop(s) updated");
`

func (k RealKDE) SetWallpaperSlideshow(rules []SlideshowRule, intervalSec int) error {
	// Build a JS array literal from the escaped rules.
	var b strings.Builder
	b.WriteString("[")
	for i, r := range rules {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, `{kind:"%s", value:"%s", path:"%s"}`,
			jsStringEscape(r.Kind), jsStringEscape(r.Value), jsStringEscape(r.Path))
	}
	b.WriteString("]")

	script := fmt.Sprintf(wallpaperSlideshowScript, b.String(), intervalSec)
	return k.runWithTimeout(kdeCallTimeout,
		QDBusBin, "org.kde.plasmashell", "/PlasmaShell",
		"evaluateScript", script)
}

func (k RealKDE) SetKonsoleDefaultProfile(profileFilename string) error {
	return k.WriteINIKey("konsolerc", "Desktop Entry", "DefaultProfile", profileFilename)
}

// WriteINIKey shells out to kwriteconfig6 (KDE Frameworks 6 / Plasma 6).
// Plasma 5's helper is kwriteconfig5; we don't target it.
//
// Nested groups are encoded with "/" separators in the `group` argument
// (e.g. "Greeter/Wallpaper/org.kde.image/General" expands to four
// `--group` flags). This is how Plasma's kscreenlockerrc nests its
// wallpaper config.
//
// Defensive: rejects group segments containing ".." -- kwriteconfig6
// itself accepts arbitrary group names, so a malicious manifest could
// otherwise navigate sideways in the config tree.
func (k RealKDE) WriteINIKey(file, group, key, value string) error {
	args := []string{"--file", file}
	for _, g := range strings.Split(group, "/") {
		if g == ".." || g == "." || strings.Contains(g, "/") {
			return fmt.Errorf("invalid group segment %q in %q", g, group)
		}
		args = append(args, "--group", g)
	}
	args = append(args, "--key", key, value)
	return k.run("kwriteconfig6", args...)
}

// launcherIconScript is the Plasma 6 JS sent to plasmashell. It walks
// every panel, finds launcher widgets, swaps their icon config key, and
// reloads. The "%s" placeholder is filled with a JS-escaped path.
//
// Plasma 6 API note: panel.applets (property) was removed -- panel.widgets()
// (method) is the replacement and returns the same set of Widget objects.
// We use the method and null-check the result; older Plasma 5 panels
// would simply yield zero matches rather than crashing.
const launcherIconScript = `
var changed = 0;
var ps = panels();
for (var i = 0; i < ps.length; i++) {
    var ws = typeof ps[i].widgets === "function" ? ps[i].widgets() : ps[i].applets;
    if (!ws) continue;
    for (var j = 0; j < ws.length; j++) {
        var t = ws[j].type;
        if (t === "org.kde.plasma.kickoff" ||
            t === "org.kde.plasma.kicker" ||
            t === "org.kde.plasma.kickerdash") {
            ws[j].currentConfigGroup = ["General"];
            ws[j].writeConfig("icon", "%s");
            ws[j].reloadConfig();
            changed++;
        }
    }
}
print("riced launcher-icon: " + changed + " widget(s) updated");
`

func (k RealKDE) SetLauncherIcon(iconPath string) error {
	script := fmt.Sprintf(launcherIconScript, jsStringEscape(iconPath))
	return k.runWithTimeout(kdeCallTimeout,
		QDBusBin, "org.kde.plasmashell", "/PlasmaShell",
		"evaluateScript", script)
}

// panelGeometryScript writes the optional [panel] geometry knobs to every
// Plasma 6 panel via the plasmashell scripting API. Empty / zero fields
// pass through as JS empty strings and the script skips them, so a user
// who only sets `position` does not see `floating` reset to false or
// `height` reset to the Plasma default.
//
// Order of substitutions: location, floating, height (height is a numeric
// string so a missing value reads as "0" -> skipped by the JS check).
const panelGeometryScript = `
var changed = 0;
var ps = panels();
for (var i = 0; i < ps.length; i++) {
    var p = ps[i];
    if (!p) continue;
    if ("%s".length > 0) { p.location = "%s"; }
    if ("%s".length > 0) { p.floating = (%s === "true"); }
    if (%d > 0)          { p.height   = %d; }
    changed++;
}
print("riced panel-geometry: " + changed + " panel(s) updated");
`

// SetPanelGeometry updates location / floating / height across every
// Plasma 6 panel. Each field is opt-in: pass "" / false / 0 to leave the
// corresponding property untouched.
func (k RealKDE) SetPanelGeometry(location string, floating bool, height int) error {
	loc := jsStringEscape(location)
	floatStr := ""
	if location != "" || floating || height > 0 {
		// floating is only applied when the manifest mentioned the panel
		// section at all; otherwise we cannot tell "user wants false" from
		// "user said nothing". The plan layer guarantees we only emit a
		// SetPanelGeometry action when the section is non-empty, so it is
		// safe to forward the bool here.
		if floating {
			floatStr = "true"
		} else {
			floatStr = "false"
		}
	}
	script := fmt.Sprintf(panelGeometryScript, loc, loc, floatStr, floatStr, height, height)
	return k.runWithTimeout(kdeCallTimeout,
		QDBusBin, "org.kde.plasmashell", "/PlasmaShell",
		"evaluateScript", script)
}

// jsStringEscape escapes a path so it can be safely embedded inside a
// JavaScript double-quoted string literal. Handles backslash, double
// quote, newline, and carriage return. Newlines + CR are technically
// legal in Linux file names (rare but possible) and would otherwise
// terminate the JS string literal mid-script -- a malformed JS would
// surface as a confusing plasmashell evaluateScript error rather than a
// clean Riced refusal.
func jsStringEscape(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\\', '"':
			out = append(out, '\\', c)
		case '\n':
			out = append(out, '\\', 'n')
		case '\r':
			out = append(out, '\\', 'r')
		default:
			out = append(out, c)
		}
	}
	return string(out)
}

func (k RealKDE) ReconfigureKWin() error {
	return k.run(QDBusBin, "org.kde.KWin", "/KWin", "reconfigure")
}

func (k RealKDE) ApplyCursorTheme(name string) error {
	return k.run("plasma-apply-cursortheme", name)
}

func (k RealKDE) ApplyDesktopTheme(name string) error {
	return k.run("plasma-apply-desktoptheme", name)
}

func (k RealKDE) ApplyLookAndFeel(packageID string) error {
	return k.run("plasma-apply-lookandfeel", "-a", packageID)
}

// KPackageInstall registers a KPackage (archive or extracted dir).
// kpackagetool6 -i is idempotent only when the package isn't already
// installed; we fall back to -u (upgrade) so re-apply doesn't fail when
// the package is unchanged.
//
// If install AND upgrade both fail, we surface both errors via
// errors.Join so the user can tell whether the package is genuinely
// broken vs a transient kpackagetool6 hiccup.
func (k RealKDE) KPackageInstall(pkgType, sourcePath string) error {
	installErr := k.run("kpackagetool6", "-t", pkgType, "-i", sourcePath)
	if installErr == nil {
		return nil
	}
	if upgradeErr := k.run("kpackagetool6", "-t", pkgType, "-u", sourcePath); upgradeErr != nil {
		return errors.Join(installErr, upgradeErr)
	}
	return nil
}

// RefreshSystemCache rebuilds KDE's ksycoca. `--noincremental` forces a
// full rebuild so newly-installed icon themes / look-and-feel packages
// are visible to newly-launched apps right away.
func (k RealKDE) RefreshSystemCache() error {
	return k.run("kbuildsycoca6", "--noincremental")
}

// extractArchive expands a .tar.gz / .tar.xz / .zip archive into dst.
// Shells out to tar / unzip (already-required system tools). Lives in
// kde.go; called from external.go's resolveLookSource.
//
// Captures stderr so a corrupted archive surfaces as a useful diagnostic
// (e.g. "gzip: stdin: invalid magic") rather than the bare "exit status 2".
func extractArchive(archivePath, dst string) error {
	var cmd *exec.Cmd
	lower := strings.ToLower(archivePath)
	switch {
	case strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz"):
		cmd = exec.Command("tar", "-xzf", archivePath, "-C", dst)
	case strings.HasSuffix(lower, ".tar.xz"):
		cmd = exec.Command("tar", "-xJf", archivePath, "-C", dst)
	case strings.HasSuffix(lower, ".zip"):
		cmd = exec.Command("unzip", "-qq", "-o", archivePath, "-d", dst)
	default:
		return fmt.Errorf("unsupported archive extension: %s", archivePath)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("extract %s: %w\n--- tool output ---\n%s",
			filepath.Base(archivePath), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (k RealKDE) run(name string, args ...string) error {
	return k.runWithTimeout(kdeCallTimeout, name, args...)
}

// runWithTimeout is the actual exec path. Every KDE helper goes through
// here with a bounded context -- a hung plasmashell can't freeze us.
//
// Both stdout and stderr are merged onto k.Stderr (which defaults to
// os.Stderr): the user wants to see what `plasma-apply-*` and `qdbus`
// say, and we never need to capture their output programmatically.
func (k RealKDE) runWithTimeout(timeout time.Duration, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	stderr := k.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	cmd.Stderr = stderr
	cmd.Stdout = stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("%s %s: timed out after %s", name, strings.Join(args, " "), timeout)
		}
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

// FakeKDE records every call instead of running it. Used by tests.
type FakeKDE struct {
	Calls []string
	// *Err fields let tests force failures to verify error handling.
	// nil = success.
	ColorSchemeErr    error
	WallpaperErr      error
	KonsoleProfileErr error
	WriteINIErr       error
	LauncherIconErr   error
	ReconfigureErr    error
	CursorThemeErr    error
	DesktopThemeErr   error
	LookAndFeelErr    error
	KPackageErr       error
	RefreshCacheErr   error
	PanelGeometryErr  error
}

func (k *FakeKDE) ApplyColorScheme(slug string) error {
	k.Calls = append(k.Calls, "ApplyColorScheme:"+slug)
	return k.ColorSchemeErr
}

func (k *FakeKDE) SetWallpaperImage(path string) error {
	k.Calls = append(k.Calls, "SetWallpaperImage:"+path)
	return k.WallpaperErr
}

func (k *FakeKDE) SetWallpaperSlideshow(rules []SlideshowRule, intervalSec int) error {
	parts := make([]string, 0, len(rules))
	for _, r := range rules {
		parts = append(parts, fmt.Sprintf("%s=%s=>%s", r.Kind, r.Value, r.Path))
	}
	k.Calls = append(k.Calls, fmt.Sprintf("SetWallpaperSlideshow:[%s]|%d", strings.Join(parts, ","), intervalSec))
	return k.WallpaperErr
}

func (k *FakeKDE) SetKonsoleDefaultProfile(profile string) error {
	k.Calls = append(k.Calls, "SetKonsoleDefaultProfile:"+profile)
	return k.KonsoleProfileErr
}

func (k *FakeKDE) WriteINIKey(file, group, key, value string) error {
	k.Calls = append(k.Calls, fmt.Sprintf("WriteINIKey:%s/[%s]/%s=%s", file, group, key, value))
	return k.WriteINIErr
}

func (k *FakeKDE) SetLauncherIcon(iconPath string) error {
	k.Calls = append(k.Calls, "SetLauncherIcon:"+iconPath)
	return k.LauncherIconErr
}

func (k *FakeKDE) ReconfigureKWin() error {
	k.Calls = append(k.Calls, "ReconfigureKWin")
	return k.ReconfigureErr
}

func (k *FakeKDE) ApplyCursorTheme(name string) error {
	k.Calls = append(k.Calls, "ApplyCursorTheme:"+name)
	return k.CursorThemeErr
}

func (k *FakeKDE) ApplyDesktopTheme(name string) error {
	k.Calls = append(k.Calls, "ApplyDesktopTheme:"+name)
	return k.DesktopThemeErr
}

func (k *FakeKDE) ApplyLookAndFeel(packageID string) error {
	k.Calls = append(k.Calls, "ApplyLookAndFeel:"+packageID)
	return k.LookAndFeelErr
}

func (k *FakeKDE) KPackageInstall(pkgType, sourcePath string) error {
	k.Calls = append(k.Calls, fmt.Sprintf("KPackageInstall:%s|%s", pkgType, sourcePath))
	return k.KPackageErr
}

func (k *FakeKDE) RefreshSystemCache() error {
	k.Calls = append(k.Calls, "RefreshSystemCache")
	return k.RefreshCacheErr
}

func (k *FakeKDE) SetPanelGeometry(location string, floating bool, height int) error {
	k.Calls = append(k.Calls, fmt.Sprintf("SetPanelGeometry:%s|%t|%d", location, floating, height))
	return k.PanelGeometryErr
}
