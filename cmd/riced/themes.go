package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/Altagen/Riced/internal/manifest"
	"github.com/Altagen/Riced/internal/registry"
)

// discoveredTheme is one row produced by scanning a single themes/ dir.
// Lifted to file-level (rather than nested inside runThemes) so the helper
// functions below can take []discoveredTheme without type acrobatics.
type discoveredTheme struct {
	repo     string // empty for ~/.riced/themes/ entries
	slug     string
	name     string
	mode     string
	theme    string // Meta.Theme -- the design family
	status   string
	fullSlug string // "<repo>/<slug>" or just slug for user-overrides
}

// runThemes implements `riced themes [--repo NAME] [--theme GROUP]`.
//
// It enumerates every theme reachable through the registry and the
// ~/.riced/themes/ user override directory, groups them by Meta.Theme, and
// prints a compact table.
//
// `riced list` is the lower-level cousin that walks a single search-path
// passed on the command line. `themes` is the user-facing default.
func runThemes(args []string) int {
	fs := flag.NewFlagSet("themes", flag.ContinueOnError)
	repoFilter := fs.String("repo", "", "show only themes from this repository")
	themeFilter := fs.String("theme", "", "show only themes in this design family (Meta.Theme)")
	slugsOnly := fs.Bool("slugs", false, "print qualified slugs only, one per line (for shell completion)")
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, "Usage: riced themes [--repo NAME] [--theme GROUP] [--slugs]\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	reg, err := registry.Load()
	if err != nil {
		slog.Error("load registry", "err", err)
		return 1
	}

	var found []discoveredTheme

	// Registered repositories.
	for _, r := range reg.List() {
		if *repoFilter != "" && *repoFilter != r.Name {
			continue
		}
		found = append(found, scanThemesDir(r.Name, filepath.Join(r.Path, "themes"))...)
	}
	// User overrides (~/.riced/themes/) -- hidden when a --repo filter is set.
	if *repoFilter == "" {
		if userDir, err := userThemesDir(); err == nil {
			found = append(found, scanThemesDir("", userDir)...)
		}
	}

	if *themeFilter != "" {
		filtered := found[:0]
		for _, d := range found {
			if d.theme == *themeFilter {
				filtered = append(filtered, d)
			}
		}
		found = filtered
	}

	if len(found) == 0 {
		if !*slugsOnly {
			slog.Warn("no themes found")
		}
		return 0
	}

	if *slugsOnly {
		// Sorted, unique slugs -- completion frontends append directly
		// after `riced apply `, so the output must be parse-free AND
		// stable across runs (otherwise re-pressing Tab could reshuffle
		// the menu order).
		seen := map[string]struct{}{}
		var slugs []string
		for _, d := range found {
			if _, dup := seen[d.fullSlug]; dup {
				continue
			}
			seen[d.fullSlug] = struct{}{}
			slugs = append(slugs, d.fullSlug)
		}
		sort.Strings(slugs)
		for _, s := range slugs {
			fmt.Println(s)
		}
		return 0
	}

	printGroupedThemes(found)
	return 0
}

// scanThemesDir walks immediate subdirs of base, loading each theme.toml
// and producing a discoveredTheme row. Returns nil if base doesn't exist
// (so callers can probe optional directories without pre-stat'ing).
func scanThemesDir(repoName, base string) []discoveredTheme {
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	var out []discoveredTheme
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		themeDir := filepath.Join(base, e.Name())
		if _, err := os.Stat(filepath.Join(themeDir, manifest.FileName)); err != nil {
			continue
		}
		m, lerr := manifest.ResolveDir(themeDir, nil)
		if lerr != nil {
			out = append(out, discoveredTheme{repo: repoName, slug: e.Name(), status: "load error"})
			continue
		}
		status := "ok"
		if verr := m.Validate(); verr != nil {
			status = "invalid"
		}
		d := discoveredTheme{
			repo:   repoName,
			slug:   m.Meta.Slug,
			name:   m.Meta.Name,
			mode:   m.Meta.Mode,
			theme:  m.Meta.Theme,
			status: status,
		}
		if repoName == "" {
			d.fullSlug = d.slug
		} else {
			d.fullSlug = repoName + "/" + d.slug
		}
		out = append(out, d)
	}
	return out
}

// printGroupedThemes renders the table grouped by (repo, design-family).
// One row per group; MODES lists the modes present in that group; SLUGS
// lists every applicable slug. Themes without an explicit Meta.Theme land
// in a synthetic "(none)" group at the bottom of each repo block.
func printGroupedThemes(found []discoveredTheme) {
	type key struct{ repo, theme string }
	groups := map[key]*themeGroupRow{}

	for _, d := range found {
		k := key{repo: d.repo, theme: d.theme}
		g, ok := groups[k]
		if !ok {
			g = &themeGroupRow{repo: d.repo, theme: d.theme}
			groups[k] = g
		}
		g.slugs = append(g.slugs, d.fullSlug)
		if d.mode != "" && !contains(g.modes, d.mode) {
			g.modes = append(g.modes, d.mode)
		}
		if d.status != "ok" {
			g.anyInvalid = true
		}
	}

	rows := make([]*themeGroupRow, 0, len(groups))
	for _, g := range groups {
		sort.Strings(g.modes)
		sort.Strings(g.slugs)
		rows = append(rows, g)
	}
	sort.Slice(rows, func(i, j int) bool {
		// User overrides ("") first, then repos alphabetical. Within a repo,
		// themes with a group name come before "(none)".
		if rows[i].repo != rows[j].repo {
			return rows[i].repo < rows[j].repo
		}
		iEmpty, jEmpty := rows[i].theme == "", rows[j].theme == ""
		if iEmpty != jEmpty {
			return !iEmpty
		}
		return rows[i].theme < rows[j].theme
	})

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "REPO\tTHEME\tMODES\tSLUGS\tSTATUS")
	for _, g := range rows {
		repoCell := g.repo
		if repoCell == "" {
			repoCell = "(user)"
		}
		themeCell := g.theme
		if themeCell == "" {
			themeCell = "(none)"
		}
		modesCell := "--"
		if len(g.modes) > 0 {
			modesCell = strings.Join(g.modes, ", ")
		}
		status := "ok"
		if g.anyInvalid {
			status = "has invalid"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			repoCell, themeCell, modesCell, strings.Join(g.slugs, ", "), status)
	}
	_ = tw.Flush()
}

type themeGroupRow struct {
	repo, theme string
	modes       []string
	slugs       []string
	anyInvalid  bool
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// userThemesDir returns ~/.riced/themes/ if HOME is resolvable.
func userThemesDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".riced", "themes"), nil
}
