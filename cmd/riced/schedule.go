package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/Altagen/Riced/internal/schedule"
)

// runSchedule implements `riced schedule (install|list|uninstall)`.
//
// Backed by systemd user timers under $XDG_CONFIG_HOME/systemd/user/.
// No root or sudo; the timers are scoped to the user's session.
func runSchedule(args []string) int {
	if len(args) == 0 {
		scheduleUsage()
		return exitUsage
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "install":
		return runScheduleInstall(rest)
	case "list":
		return runScheduleList(rest)
	case "uninstall":
		return runScheduleUninstall(rest)
	case "-h", "--help", "help":
		scheduleUsage()
		return exitOK
	default:
		slog.Error("unknown schedule subcommand", "got", sub)
		scheduleUsage()
		return exitUsage
	}
}

func scheduleUsage() {
	fmt.Fprint(os.Stderr, `riced schedule -- install systemd user timers that flip a family theme day/night

Usage:
  riced schedule install <family-slug> --day HH:MM-HH:MM
  riced schedule list
  riced schedule uninstall <family-slug>

The "install" form writes four systemd user units under
$XDG_CONFIG_HOME/systemd/user/, runs `+"`"+`systemctl --user daemon-reload`+"`"+`,
and enables both timers. At the first time the service runs
`+"`"+`riced apply --yes --mode=light <family-slug>`+"`"+`; at the second,
`+"`"+`--mode=dark`+"`"+`.
`)
}

func runScheduleInstall(args []string) int {
	fs := flag.NewFlagSet("schedule install", flag.ContinueOnError)
	day := fs.String("day", "", "HH:MM-HH:MM range -- first time = light kicks in, second = dark kicks in")
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() < 1 || *day == "" {
		slog.Error("usage: riced schedule install <family-slug> --day HH:MM-HH:MM")
		return exitUsage
	}
	family := fs.Arg(0)
	dayHHMM, nightHHMM, err := schedule.ParseDayRange(*day)
	if err != nil {
		slog.Error("parse --day", "err", err)
		return exitUsage
	}

	home, err := os.UserHomeDir()
	if err != nil {
		slog.Error("resolve HOME", "err", err)
		return exitErr
	}

	// Resolve the absolute path of the running riced binary so the service
	// keeps working after a $PATH change. Fall back to bare "riced" when
	// the executable path lookup fails -- systemd will still find it on
	// the user's $PATH at run time.
	exe, _ := os.Executable()
	if exe != "" {
		exe, _ = filepath.EvalSymlinks(exe)
	}

	paths, err := schedule.Install(home, family, dayHHMM, nightHHMM, exe)
	if err != nil {
		slog.Error("install schedule", "err", err)
		return exitErr
	}
	slog.Info("schedule installed",
		"family", family,
		"day", dayHHMM,
		"night", nightHHMM,
		"timer_day", paths.DayTimer,
		"timer_night", paths.NightTimer,
	)
	fmt.Fprintln(os.Stderr, "Run `systemctl --user list-timers riced-mode-*` to inspect.")
	return exitOK
}

func runScheduleList(_ []string) int {
	home, err := os.UserHomeDir()
	if err != nil {
		slog.Error("resolve HOME", "err", err)
		return exitErr
	}
	slugs, err := schedule.List(home)
	if err != nil {
		slog.Error("list schedules", "err", err)
		return exitErr
	}
	if len(slugs) == 0 {
		fmt.Println("No schedules installed.")
		return exitOK
	}
	fmt.Println("Scheduled families:")
	for _, s := range slugs {
		fmt.Println("  " + s)
	}
	fmt.Fprintln(os.Stderr, "Run `systemctl --user list-timers riced-mode-*` for the actual fire times.")
	return exitOK
}

func runScheduleUninstall(args []string) int {
	if len(args) < 1 {
		slog.Error("usage: riced schedule uninstall <family-slug>")
		return exitUsage
	}
	family := strings.TrimSpace(args[0])
	if family == "" {
		slog.Error("empty family slug")
		return exitUsage
	}
	home, err := os.UserHomeDir()
	if err != nil {
		slog.Error("resolve HOME", "err", err)
		return exitErr
	}
	paths, err := schedule.Uninstall(home, family)
	if err != nil {
		slog.Error("uninstall schedule", "err", err)
		return exitErr
	}
	slog.Info("schedule uninstalled",
		"family", family,
		"timer_day", paths.DayTimer,
		"timer_night", paths.NightTimer,
	)
	return exitOK
}
