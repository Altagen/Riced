package main

import (
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"

	ricedlog "github.com/Altagen/Riced/internal/log"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

const (
	envLogLevel  = "RICED_LOG_LEVEL"
	envLogFormat = "RICED_LOG_FORMAT"
)

// Exit codes returned by run* subcommand handlers and surfaced via
// os.Exit. Keep this list authoritative -- any subcommand that returns
// a value outside this set is a bug.
const (
	exitOK    = 0 // success
	exitErr   = 1 // runtime error (validation, IO, exec, etc.)
	exitUsage = 2 // bad usage / user refused interactive prompt
)

func main() {
	args := os.Args[1:]
	args = setupLogging(args)

	if len(args) == 0 {
		usage()
		os.Exit(exitUsage)
	}

	cmd, rest := args[0], args[1:]
	switch cmd {
	case "version", "-v", "--version":
		// -v also serves as a "verbose" toggle when it appears before a
		// subcommand; here it has already been consumed by setupLogging.
		fmt.Printf("riced %s (commit %s, built %s)\n", version, commit, date)
	case "list":
		os.Exit(runList(rest))
	case "themes":
		os.Exit(runThemes(rest))
	case "validate":
		os.Exit(runValidate(rest))
	case "generate":
		os.Exit(runGenerate(rest))
	case "repo":
		os.Exit(runRepo(rest))
	case "apply":
		os.Exit(runApply(rest))
	case "status":
		os.Exit(runStatus(rest))
	case "revert":
		os.Exit(runRevert(rest))
	case "switch":
		os.Exit(runSwitch(rest))
	case "schedule":
		os.Exit(runSchedule(rest))
	case "clean-backups":
		os.Exit(runCleanBackups(rest))
	case "doctor":
		os.Exit(runDoctor(rest))
	case "new":
		os.Exit(runNew(rest))
	case "completion":
		os.Exit(runCompletion(rest))
	case "help", "-h", "--help":
		usage()
	default:
		slog.Error("unknown command", "cmd", cmd)
		usage()
		os.Exit(exitUsage)
	}
}

// setupLogging consumes the leading global flags from args, initializes the
// logger, and returns the remaining arguments (subcommand + its args).
// Globals must precede the subcommand -- stdlib flag stops at the first
// non-flag positional, which is exactly that boundary.
//
// Recognized: --log-level, --log-format, -v / --verbose.
// Env fallbacks: RICED_LOG_LEVEL, RICED_LOG_FORMAT.
func setupLogging(args []string) []string {
	fs := flag.NewFlagSet("riced", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // we render usage ourselves

	var (
		levelStr  = os.Getenv(envLogLevel)
		formatStr = os.Getenv(envLogFormat)
		verbose   bool
	)
	if levelStr == "" {
		levelStr = "info"
	}
	if formatStr == "" {
		formatStr = "text"
	}

	fs.StringVar(&levelStr, "log-level", levelStr, "")
	fs.StringVar(&formatStr, "log-format", formatStr, "")
	fs.BoolVar(&verbose, "verbose", false, "")
	fs.BoolVar(&verbose, "v", false, "")

	// flag.Parse errors on unknown flags; that's fine -- we only care about
	// the leading ones, and unknown flags surface later in the subcommand's
	// own flagset. Suppress the noisy "flag provided but not defined".
	_ = fs.Parse(args)
	if verbose {
		levelStr = "debug"
	}
	ricedlog.Setup(ricedlog.Options{
		Level:  ricedlog.ParseLevel(levelStr),
		Format: ricedlog.ParseFormat(formatStr),
	})
	return fs.Args()
}

func usage() {
	fmt.Fprint(os.Stderr, `riced -- theme-as-code engine for KDE Plasma

Usage:
  riced [global flags] <command> [args]

Commands:
  themes    [--repo N] [--theme G]  List themes across all registered repositories
  list      [search-path]           List themes in a local directory (low-level)
  validate  [--hints] <theme-dir>   Load + validate a theme.toml (no rendering;
                                    --hints adds a passive reminder about taplo)
  generate  <theme-dir>             Render a theme to ./build/<slug>/
  repo init [--register] <path>     Make <path> a Riced repository (idempotent;
                                    creates only the pieces that are missing)
  repo add  <path>                  Register an already-initialized repository
  repo list                         Show registered repositories
  repo remove <name>                Unregister (files on disk are kept)
  apply     [--dry-run|--yes] [--mode=dark|light] <slug>
                                    Render + apply to the live KDE session
                                    (--mode required for family themes with
                                    [meta.modes]; flags must precede slug)
  status                            Show the currently-applied theme
  revert                            Undo the last apply (restore backups)
  switch    [--dry-run|--yes]       Toggle the currently-applied family theme
                                    between its dark and light variants
  schedule  (install|list|uninstall)  Install systemd user timers that flip
                                    a family theme on a day/night schedule
  clean-backups [--keep N|--older-than DUR]
                                    Garbage-collect ~/.riced/state/backup/
  doctor                            Audit the live state for inconsistencies
  new theme [--from PARENT|--repo NAME|--mode MODE] <slug>
                                    Scaffold a new theme
  completion <fish|bash>            Print shell completion script to stdout
  version                           Print version
  help                              Show this help

Global flags:
  --log-level=debug|info|warn|error  default: info
  --log-format=text|json             default: text
  -v, --verbose                      shortcut for --log-level=debug

Env fallback: RICED_LOG_LEVEL, RICED_LOG_FORMAT.

The contract between Riced and a theme is theme.toml in <theme-dir>.
See docs/MANIFEST.md for the schema.
`)
}
