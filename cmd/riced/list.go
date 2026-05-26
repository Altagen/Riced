package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"text/tabwriter"

	"github.com/Altagen/Riced/internal/manifest"
)

// runList implements `riced list [search-path]`.
//
// It walks immediate subdirectories of the search path, attempts to load
// theme.toml from each, and prints a one-line summary per theme found.
//
// Exit codes: 0 on success, 1 if the search path can't be read.
func runList(args []string) int {
	searchPath := "./themes"
	if len(args) >= 1 {
		searchPath = args[0]
	}

	entries, err := os.ReadDir(searchPath)
	if err != nil {
		slog.Error("read search path", "path", searchPath, "err", err)
		return 1
	}

	type row struct {
		slug, name, mode, status string
	}
	var rows []row

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		themeDir := filepath.Join(searchPath, e.Name())
		if _, err := os.Stat(filepath.Join(themeDir, manifest.FileName)); err != nil {
			continue // not a theme dir, skip silently
		}
		m, loadErr := manifest.ResolveDir(themeDir, nil)
		if loadErr != nil {
			rows = append(rows, row{slug: e.Name(), status: "parse error: " + loadErr.Error()})
			continue
		}
		status := "ok"
		if vErr := m.Validate(); vErr != nil {
			var ve *manifest.ValidationError
			if errors.As(vErr, &ve) {
				status = fmt.Sprintf("invalid (%d issue(s))", len(ve.Issues))
			} else {
				status = "invalid"
			}
		}
		rows = append(rows, row{
			slug:   m.Meta.Slug,
			name:   m.Meta.Name,
			mode:   m.Meta.Mode,
			status: status,
		})
	}

	if len(rows) == 0 {
		slog.Warn("no themes found", "path", searchPath)
		return 0
	}

	sort.Slice(rows, func(i, j int) bool { return rows[i].slug < rows[j].slug })

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "SLUG\tNAME\tMODE\tSTATUS")
	for _, r := range rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.slug, r.name, r.mode, r.status)
	}
	if err := tw.Flush(); err != nil {
		slog.Error("flush table", "err", err)
		return 1
	}
	return 0
}
