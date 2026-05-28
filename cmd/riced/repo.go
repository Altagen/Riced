package main

import (
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/Altagen/Riced/internal/registry"
)

// runRepo dispatches `riced repo <subcommand>`.
//
// Subcommands:
//
//	init    [--register] <path>   Make <path> a Riced repository (idempotent;
//	                              only creates the pieces that are missing)
//	add     <path>                Register an already-initialized repository
//	list                          Print every registered repository
//	remove  <name>                Unregister (files on disk are kept)
//
// Notably absent: any subcommand that shells out to git. Cloning, pulling
// and pushing are the user's job -- Riced only manages the on-disk paths
// it knows about. This keeps the binary's exec surface limited to the
// KDE helpers actually required by `riced apply`.
func runRepo(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: riced repo <init|add|list|remove> ...")
		return exitUsage
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "init":
		return runRepoInit(rest)
	case "add":
		return runRepoAdd(rest)
	case "list", "ls":
		return runRepoList(rest)
	case "remove", "rm":
		return runRepoRemove(rest)
	default:
		slog.Error("unknown repo subcommand", "sub", sub, "hint", "use init|add|list|remove")
		return exitUsage
	}
}

// runRepoInit creates / completes the Riced layout in <path>.
//
// Semantics (idempotent):
//
//   - <path> does not exist           → mkdir it, then scaffold everything
//   - <path> exists, no marker        → scaffold only the pieces that are missing
//   - <path> exists, marker present   → no-op (every scaffold step is "create if absent")
//
// Existing files are never overwritten. This makes the command safe to run
// on a freshly cloned external repository: the marker gets added if absent,
// otherwise the command is a quiet no-op.
//
// --register also adds the repository to ~/.config/riced/repositories.toml.
// The registered name defaults to filepath.Base(path).
func runRepoInit(args []string) int {
	fs := flag.NewFlagSet("repo init", flag.ContinueOnError)
	register := fs.Bool("register", false, "also add the initialized repository to the user registry")
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, "Usage: riced repo init [--register] <path>\n")
		fmt.Fprint(os.Stderr, "(Flags must precede <path>.)\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() < 1 {
		fs.Usage()
		return exitUsage
	}
	rawPath := fs.Arg(0)
	root, err := filepath.Abs(rawPath)
	if err != nil {
		slog.Error("resolve path", "err", err)
		return exitErr
	}
	name := filepath.Base(root)
	if err := registry.ValidateName(name); err != nil {
		slog.Error("invalid repository name (derived from path's last segment)",
			"name", name, "err", err)
		return exitUsage
	}

	created, err := scaffoldIfMissing(root, name)
	if err != nil {
		slog.Error("scaffold", "err", err)
		return exitErr
	}
	if len(created) == 0 {
		slog.Info("repository already initialized; nothing to do", "path", root)
	} else {
		slog.Info("repository initialized", "path", root, "created", len(created))
		for _, p := range created {
			slog.Debug("created", "path", p)
		}
	}

	if *register {
		reg, err := registry.Load()
		if err != nil {
			slog.Error("load registry", "err", err)
			return exitErr
		}
		if _, already := reg.Get(name); already {
			slog.Info("already registered", "name", name)
		} else if err := reg.Add(registry.Repository{Name: name, Path: root}); err != nil {
			slog.Error("add to registry", "err", err)
			return exitErr
		} else if err := reg.Save(); err != nil {
			slog.Error("save registry", "err", err)
			return exitErr
		} else {
			slog.Info("registered", "name", name, "path", root)
		}
	}
	return exitOK
}

// runRepoAdd registers an existing Riced repository in the user registry.
// Pure path operation -- never clones, never pulls. The path MUST already
// contain a repository.toml marker (run `riced repo init` first if not).
func runRepoAdd(args []string) int {
	fs := flag.NewFlagSet("repo add", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, "Usage: riced repo add <path>\n")
		fmt.Fprint(os.Stderr, "(The path must already contain repository.toml -- run 'riced repo init <path>' first if missing.)\n")
	}
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return exitUsage
	}
	rawPath := fs.Arg(0)
	root, err := filepath.Abs(rawPath)
	if err != nil {
		slog.Error("resolve path", "err", err)
		return exitErr
	}
	info, err := os.Stat(root)
	if err != nil {
		slog.Error("path does not exist", "path", root, "err", err)
		return exitErr
	}
	if !info.IsDir() {
		slog.Error("path is not a directory", "path", root)
		return exitErr
	}
	if _, err := os.Stat(filepath.Join(root, registry.RepositoryManifestFile)); err != nil {
		slog.Error("no repository.toml at path",
			"path", root,
			"hint", "run 'riced repo init "+rawPath+"' to create the missing scaffold")
		return exitErr
	}

	name := filepath.Base(root)
	reg, err := registry.Load()
	if err != nil {
		slog.Error("load registry", "err", err)
		return exitErr
	}
	if err := reg.Add(registry.Repository{Name: name, Path: root}); err != nil {
		slog.Error("add repository", "err", err)
		return exitErr
	}
	if err := reg.Save(); err != nil {
		slog.Error("save registry", "err", err)
		return exitErr
	}
	slog.Info("repository registered", "name", name, "path", root)
	return exitOK
}

func runRepoList(args []string) int {
	fs := flag.NewFlagSet("repo list", flag.ContinueOnError)
	namesOnly := fs.Bool("names", false, "print repository names only, one per line (for shell completion)")
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}

	reg, err := registry.Load()
	if err != nil {
		slog.Error("load registry", "err", err)
		return exitErr
	}
	repos := reg.List()
	if len(repos) == 0 {
		if !*namesOnly {
			slog.Warn("no repositories registered")
		}
		return exitOK
	}

	if *namesOnly {
		for _, r := range repos {
			fmt.Println(r.Name)
		}
		return exitOK
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tPATH")
	for _, r := range repos {
		fmt.Fprintf(tw, "%s\t%s\n", r.Name, r.Path)
	}
	if err := tw.Flush(); err != nil {
		slog.Error("flush table", "err", err)
		return exitErr
	}
	return exitOK
}

func runRepoRemove(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "Usage: riced repo remove <name>")
		return exitUsage
	}
	name := args[0]
	reg, err := registry.Load()
	if err != nil {
		slog.Error("load registry", "err", err)
		return exitErr
	}
	if ok := reg.Remove(name); !ok {
		slog.Error("repository not found", "name", name)
		return exitErr
	}
	if err := reg.Save(); err != nil {
		slog.Error("save registry", "err", err)
		return exitErr
	}
	slog.Info("repository unregistered", "name", name, "note", "files on disk were not deleted")
	return exitOK
}

// scaffoldIfMissing creates each piece of the standard Riced repository
// layout under root, skipping anything that already exists. Returns the
// list of paths it actually created so the caller can report the diff.
//
// Idempotency rule: NEVER overwrite. Every file is created only if absent,
// every directory is created with MkdirAll which is itself a no-op when
// the dir already exists.
func scaffoldIfMissing(root, name string) ([]string, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", root, err)
	}

	var created []string

	// Standard subdirectories with a .gitkeep so git keeps them.
	for _, sub := range []string{"themes", "wallpapers", "icons"} {
		d := filepath.Join(root, sub)
		if err := os.MkdirAll(d, 0o755); err != nil {
			return created, fmt.Errorf("mkdir %s: %w", d, err)
		}
		keep := filepath.Join(d, ".gitkeep")
		c, err := writeIfMissing(keep, nil)
		if err != nil {
			return created, err
		}
		if c {
			created = append(created, keep)
		}
	}

	// Repository manifest. We don't modify an existing one so any user
	// metadata (description, author, homepage) survives.
	manifestPath := filepath.Join(root, registry.RepositoryManifestFile)
	if _, err := os.Stat(manifestPath); errors.Is(err, os.ErrNotExist) {
		if err := registry.SaveRepositoryManifest(root, &registry.RepositoryManifest{
			Repository: registry.RepoHeader{Name: name},
		}); err != nil {
			return created, fmt.Errorf("write %s: %w", manifestPath, err)
		}
		created = append(created, manifestPath)
	}

	readme := filepath.Join(root, "README.md")
	c, err := writeIfMissing(readme, []byte(readmeBoilerplate(name)))
	if err != nil {
		return created, err
	}
	if c {
		created = append(created, readme)
	}

	gitignore := filepath.Join(root, ".gitignore")
	c, err = writeIfMissing(gitignore, []byte("# Riced\n/build/\n*.riced.tmp.*\n"))
	if err != nil {
		return created, err
	}
	if c {
		created = append(created, gitignore)
	}

	return created, nil
}

// writeIfMissing writes body to path only if path does not exist. Returns
// (true, nil) when the file was created, (false, nil) when it already
// existed, and (_, err) on any I/O error.
func writeIfMissing(path string, body []byte) (bool, error) {
	if _, err := os.Stat(path); err == nil {
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("stat %s: %w", path, err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return false, fmt.Errorf("write %s: %w", path, err)
	}
	return true, nil
}

// readmeBoilerplate produces a minimal README for a freshly scaffolded
// repository.
func readmeBoilerplate(name string) string {
	return fmt.Sprintf(`# %[1]s

A Riced theme repository for KDE Plasma.

## Layout

- `+"`themes/<slug>/theme.toml`"+` -- one directory per theme
- `+"`wallpapers/*.png`"+` -- shared wallpaper pool
- `+"`icons/*.png`"+` -- icon assets (e.g. launcher overrides)

## Usage

`+"```bash"+`
# Register this repo with Riced (once)
riced repo add ./

# Apply a theme from this repo
riced apply %[1]s/<slug>
`+"```"+`

See [Riced](https://github.com/Altagen/Riced) for the engine and
`+"`theme.toml`"+` schema reference.
`, name)
}
