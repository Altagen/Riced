// Package schedule installs systemd user timers that call
// `riced apply <family> --mode=...` at the configured day / night
// boundaries. No system-wide changes, no sudo: everything lives under
// $XDG_CONFIG_HOME/systemd/user/.
//
// The unit pair per family is:
//
//	riced-mode-day-<slug>.timer    -> .service which applies --mode=light
//	riced-mode-night-<slug>.timer  -> .service which applies --mode=dark
//
// Times use systemd's OnCalendar format so DST + clock drift are handled
// by the system, not by Riced.
package schedule

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// UnitPair is the four files a single family schedule materializes:
// a day timer + service, and a night timer + service. Paths are
// absolute under $XDG_CONFIG_HOME/systemd/user/.
type UnitPair struct {
	Family       string
	DayTimer     string
	DayService   string
	NightTimer   string
	NightService string
}

var timeRange = regexp.MustCompile(`^(\d{2}):(\d{2})-(\d{2}):(\d{2})$`)

// ParseDayRange splits a "HH:MM-HH:MM" range into its day boundary (the
// first time, when light kicks in) and its night boundary (the second
// time, when dark kicks in). Validates the format and the hour/minute
// ranges; does NOT enforce that day < night (overnight ranges are valid).
func ParseDayRange(s string) (dayHHMM, nightHHMM string, err error) {
	m := timeRange.FindStringSubmatch(s)
	if m == nil {
		return "", "", fmt.Errorf(`%q is not in HH:MM-HH:MM form (e.g. "07:00-19:00")`, s)
	}
	for _, hh := range []string{m[1], m[3]} {
		if hh < "00" || hh > "23" {
			return "", "", fmt.Errorf("hour %q out of range (00-23)", hh)
		}
	}
	for _, mm := range []string{m[2], m[4]} {
		if mm < "00" || mm > "59" {
			return "", "", fmt.Errorf("minute %q out of range (00-59)", mm)
		}
	}
	return m[1] + ":" + m[2], m[3] + ":" + m[4], nil
}

// UnitDir returns the user-level systemd directory under $XDG_CONFIG_HOME
// (falling back to ~/.config). Created on demand by Install.
func UnitDir(home string) string {
	xdg := os.Getenv("XDG_CONFIG_HOME")
	if xdg == "" {
		xdg = filepath.Join(home, ".config")
	}
	return filepath.Join(xdg, "systemd", "user")
}

// PathsFor reports the four file paths a Install / Uninstall touches for a
// given family slug.
func PathsFor(home, family string) UnitPair {
	dir := UnitDir(home)
	return UnitPair{
		Family:       family,
		DayTimer:     filepath.Join(dir, "riced-mode-day-"+family+".timer"),
		DayService:   filepath.Join(dir, "riced-mode-day-"+family+".service"),
		NightTimer:   filepath.Join(dir, "riced-mode-night-"+family+".timer"),
		NightService: filepath.Join(dir, "riced-mode-night-"+family+".service"),
	}
}

// Install writes the four unit files for a family + day range, runs
// `systemctl --user daemon-reload`, and enables both timers. ricedBin is
// the absolute path the service should invoke (default: "riced" off PATH).
func Install(home, family, dayHHMM, nightHHMM, ricedBin string) (UnitPair, error) {
	dir := UnitDir(home)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return UnitPair{}, fmt.Errorf("mkdir %s: %w", dir, err)
	}
	if ricedBin == "" {
		ricedBin = "riced"
	}
	paths := PathsFor(home, family)
	writes := map[string]string{
		paths.DayTimer:     renderTimer(family, "day", dayHHMM),
		paths.DayService:   renderService(family, "light", ricedBin),
		paths.NightTimer:   renderTimer(family, "night", nightHHMM),
		paths.NightService: renderService(family, "dark", ricedBin),
	}
	for p, body := range writes {
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			return paths, fmt.Errorf("write %s: %w", p, err)
		}
	}
	if err := runSystemctl("daemon-reload"); err != nil {
		return paths, err
	}
	for _, t := range []string{
		"riced-mode-day-" + family + ".timer",
		"riced-mode-night-" + family + ".timer",
	} {
		if err := runSystemctl("enable", "--now", t); err != nil {
			return paths, err
		}
	}
	return paths, nil
}

// Uninstall disables the timers and removes the four files. Idempotent:
// missing files and "Unit not loaded" failures are not surfaced as errors.
func Uninstall(home, family string) (UnitPair, error) {
	paths := PathsFor(home, family)
	for _, t := range []string{
		"riced-mode-day-" + family + ".timer",
		"riced-mode-night-" + family + ".timer",
	} {
		// Best-effort: don't fail the whole uninstall when the unit was
		// already missing. systemctl returns non-zero for "not loaded",
		// which is exactly the "already gone" case we want to silence.
		_ = runSystemctl("disable", "--now", t)
	}
	for _, p := range []string{paths.DayTimer, paths.DayService, paths.NightTimer, paths.NightService} {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return paths, fmt.Errorf("remove %s: %w", p, err)
		}
	}
	if err := runSystemctl("daemon-reload"); err != nil {
		return paths, err
	}
	return paths, nil
}

// List returns the family slugs currently scheduled, by walking the unit
// directory for riced-mode-day-*.timer files.
func List(home string) ([]string, error) {
	dir := UnitDir(home)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var slugs []string
	for _, e := range entries {
		name := e.Name()
		const prefix = "riced-mode-day-"
		const suffix = ".timer"
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
			continue
		}
		slugs = append(slugs, name[len(prefix):len(name)-len(suffix)])
	}
	return slugs, nil
}

func renderTimer(family, phase, hhmm string) string {
	return fmt.Sprintf(`[Unit]
Description=Riced -- %s mode trigger for family %q
Documentation=https://github.com/Altagen/Riced

[Timer]
OnCalendar=*-*-* %s:00
Persistent=true
Unit=riced-mode-%s-%s.service

[Install]
WantedBy=timers.target
`, phase, family, hhmm, phase, family)
}

func renderService(family, mode, ricedBin string) string {
	return fmt.Sprintf(`[Unit]
Description=Riced -- apply %s variant of family %q
Documentation=https://github.com/Altagen/Riced

[Service]
Type=oneshot
ExecStart=%s apply --yes --mode=%s %s
`, mode, family, ricedBin, mode, family)
}

func runSystemctl(args ...string) error {
	cmd := exec.Command("systemctl", append([]string{"--user"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl --user %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}
