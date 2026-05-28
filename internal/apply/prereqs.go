package apply

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// toolRequirement describes one binary `riced apply` needs. When
// alternatives is non-empty, ANY of those names on PATH satisfies the
// requirement (e.g. qdbus6 OR qdbus -- different distros / Plasma
// versions ship different names).
type toolRequirement struct {
	primary      string
	alternatives []string
}

// requiredTools is intentionally short and stable: every entry is a
// real dependency of the apply pipeline.
var requiredTools = []toolRequirement{
	{primary: "plasma-apply-colorscheme"},                // ApplyColorScheme
	{primary: "plasma-apply-wallpaperimage"},             // SetWallpaperImage (single mode)
	{primary: "plasma-apply-cursortheme"},                // ApplyCursorTheme
	{primary: "plasma-apply-desktoptheme"},               // ApplyDesktopTheme
	{primary: "plasma-apply-lookandfeel"},                // ApplyLookAndFeel
	{primary: "kpackagetool6"},                           // KPackageInstall (looks: kpackage strategy)
	{primary: "kwriteconfig6"},                           // WriteINIKey, SetKonsoleDefaultProfile
	{primary: "kbuildsycoca6"},                           // RefreshSystemCache after icon theme change
	{primary: "qdbus6", alternatives: []string{"qdbus"}}, // ReconfigureKWin, SetLauncherIcon
	{primary: "tar"},                                     // extractArchive (.tar.gz / .tar.xz)
	{primary: "unzip"},                                   // extractArchive (.zip)
}

// QDBusBin resolves the qdbus binary name once at package init. Plasma 6
// systems usually ship `qdbus6`; some distros only have a bare `qdbus`
// that's actually the Qt6 version. We pick the first that exists on
// PATH so the rest of the code can call it by name. If neither is
// installed CheckPrerequisites surfaces the error before any exec.
var QDBusBin = func() string {
	for _, name := range []string{"qdbus6", "qdbus"} {
		if _, err := exec.LookPath(name); err == nil {
			return name
		}
	}
	return "qdbus6" // sensible default for error messages
}()

// CheckPrerequisites verifies that the binaries `riced apply` shells out
// to are present on PATH. Returns a single composite error listing the
// missing tools, or nil when everything is in place.
//
// Intended to run BEFORE the lock is taken and before any file write, so
// a user running Riced on the wrong desktop environment gets a clear,
// actionable message instead of "exec: plasma-apply-colorscheme: file
// not found" deep inside Execute.
func CheckPrerequisites() error {
	var missing []string
	for _, t := range requiredTools {
		if found := lookupAny(append([]string{t.primary}, t.alternatives...)); found == "" {
			label := t.primary
			if len(t.alternatives) > 0 {
				label = t.primary + " (or " + strings.Join(t.alternatives, " / ") + ")"
			}
			missing = append(missing, label)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf(
		"missing KDE Plasma 6 tools: %s -- riced apply requires Plasma 6 (install plasma-workspace / kf6-kconfig, or run with --dry-run to inspect the plan only)",
		strings.Join(missing, ", "))
}

// lookupAny returns the first name in names that exec.LookPath finds,
// or "" if none of them are on PATH.
func lookupAny(names []string) string {
	for _, n := range names {
		if _, err := exec.LookPath(n); err == nil {
			return n
		}
	}
	return ""
}

// CheckPlasmashellRunning probes the org.kde.plasmashell DBus name.
// Returns nil when plasmashell is responsive within plasmashellProbeTimeout,
// a wrapped error otherwise (typically "no such name on the bus").
//
// Used by `riced doctor` and `riced apply`'s prerequisite phase: if
// plasmashell is dead, our launcher-icon JS evaluateScript will fail
// with a confusing DBus error -- better to surface "plasmashell isn't
// running" up front.
func CheckPlasmashellRunning() error {
	const plasmashellProbeTimeout = 5 * time.Second
	if _, err := exec.LookPath(QDBusBin); err != nil {
		return fmt.Errorf("%s not found on PATH", QDBusBin)
	}
	ctx, cancel := context.WithTimeout(context.Background(), plasmashellProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, QDBusBin, "org.kde.plasmashell", "/MainApplication")
	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("plasmashell didn't respond within %s", plasmashellProbeTimeout)
		}
		return fmt.Errorf("plasmashell isn't running (%s probe failed): %w -- restart it with `kstart plasmashell`", QDBusBin, err)
	}
	return nil
}
