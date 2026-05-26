package main

import (
	"embed"
	"fmt"
	"log/slog"
	"os"
)

// completionsFS embeds the per-shell completion scripts at build time so
// the binary can `riced completion <shell>` itself without depending on a
// sibling files-on-disk layout. Editing the scripts and rebuilding the
// binary is the only path to update them.
//
//go:embed completions/riced.fish completions/riced.bash
var completionsFS embed.FS

// runCompletion implements `riced completion <shell>`.
//
// Output goes to stdout so users can pipe directly into their per-shell
// completion file (typically ~/.config/fish/completions/riced.fish or
// /usr/share/bash-completion/completions/riced).
//
// Exit codes: 0 ok, 2 unknown shell / bad usage.
func runCompletion(args []string) int {
	if len(args) != 1 {
		fmt.Fprint(os.Stderr, "Usage: riced completion <fish|bash>\n")
		return 2
	}
	shell := args[0]
	var path string
	switch shell {
	case "fish":
		path = "completions/riced.fish"
	case "bash":
		path = "completions/riced.bash"
	default:
		slog.Error("unsupported shell", "shell", shell, "supported", "fish, bash")
		return 2
	}

	data, err := completionsFS.ReadFile(path)
	if err != nil {
		slog.Error("read embedded completion", "shell", shell, "err", err)
		return 1
	}
	if _, err := os.Stdout.Write(data); err != nil {
		slog.Error("write completion", "err", err)
		return 1
	}
	return 0
}
