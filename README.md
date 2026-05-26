# Riced

**Theme-as-code engine for KDE Plasma 6.**

Riced reads a `theme.toml` manifest and generates / applies the matching KDE Plasma configuration: color scheme, wallpapers (per-screen aware), launcher icon, window decoration, Konsole profile, Kvantum theme, GTK mirror — everything you'd otherwise tweak by hand in System Settings. One TOML file, one fully themed desktop, reversible in one command.

## Why

KDE Plasma is fully customizable but the customization isn't declarative. There's no equivalent of [Stylix](https://github.com/danth/stylix) for non-NixOS systems. Riced fills that gap: keep your theme in git, apply it on a fresh install, switch between themes without lingering files.

## Design principles

- **One binary, no runtime deps.** A static Go binary. No Python, no Node, no scripts to chase down.
- **Templates embedded.** Everything Riced needs is inside the binary (`embed.FS`). The repo doesn't need to be on disk after install.
- **Explicit consent.** `apply` prints the full plan and waits for a `y/N` prompt before touching `~/.config`. Pass `--yes` for automation.
- **Reversible.** Every overwritten file is backed up. `riced revert` restores the previous state from the backup index.
- **Idempotent.** Re-running `apply` on the same theme does not stack backups, does not double-process files, does not change the recorded state root.
- **Host-clean dev.** All Go work runs in Podman. The host needs only `make` and `podman`.

## Install

Grab a release binary from the [Releases page](https://github.com/Altagen/Riced/releases) (multi-arch Linux), or build from source:

```bash
make build       # → ./bin/riced  (static, ~5 MB)
./bin/riced version
```

`make help` lists every target (`test`, `lint`, `tidy`, `shell`, `clean`).

## Quickstart

```bash
# 1. Register a theme repository (a directory containing themes/*/theme.toml)
riced repo add ~/themes/my-themes

# 2. List discovered themes
riced themes

# 3. Inspect the apply plan WITHOUT writing anything
riced apply --dry-run my-themes/dark

# 4. Apply (interactive y/N confirmation; pass --yes to skip)
riced apply my-themes/dark

# 5. Undo
riced revert
```

The `apply` command prints every action it will take — file copies (with backup), KDE adapter calls (`plasma-apply-colorscheme`, `kwriteconfig6 …`, `qdbus6 org.kde.KWin /KWin reconfigure`), and the slideshow rule cascade — before asking for consent. Nothing happens until you say yes.

## Commands at a glance

| Command | What it does |
|---|---|
| `riced apply <slug>` | Render + apply a theme. Use `--dry-run` for preview, `--yes` to skip the prompt. |
| `riced revert` | Undo the most recent apply: remove Riced-written files, restore originals from backup. |
| `riced doctor` | Audit live state: KDE tools available, lockfile, current applied theme, backups. |
| `riced status` | Show the currently applied theme + when it was applied. |
| `riced generate <dir>` | Render to a directory without touching the system. |
| `riced validate <dir>` | Parse + validate a `theme.toml` (with `--hints` for taplo tips). |
| `riced themes` | List themes across all registered repositories. |
| `riced repo init/add/list/remove` | Manage theme repositories. |
| `riced new theme <name>` | Scaffold a new theme directory. |
| `riced clean-backups` | Garbage-collect old backups (--keep N, --older-than 30d). |
| `riced completion bash/fish` | Print shell completion script. |

## The contract: `theme.toml`

A minimal manifest:

```toml
schema_version = 1

[meta]
name = "My Theme"
slug = "my-theme"
mode = "dark"

[palette]
bg      = "#0a0a12"
text    = "#e8e8f0"
accent  = "#ff2a4b"

[wallpapers]
paths = ["wallpapers/main.png"]
```

A multi-screen slideshow with portrait support:

```toml
[wallpapers]
mode     = "slideshow"
interval = 600

[[wallpapers.screens]]
orientation = "vertical"             # detected at runtime via screenGeometry()
paths = ["wallpapers/portrait.png"]

[[wallpapers.screens]]
orientation = "horizontal"
paths = ["wallpapers/landscape-1.png", "wallpapers/landscape-2.png"]

[[wallpapers.screens]]
match = "*"                          # fallback catch-all
paths = ["wallpapers/default.png"]
```

Full schema reference: [`docs/MANIFEST.md`](docs/MANIFEST.md).

## Documentation

| Doc | What it covers |
|---|---|
| [`docs/MANIFEST.md`](docs/MANIFEST.md) | Complete `theme.toml` schema reference. Every key, every enum, every default. |
| [`docs/APPLY.md`](docs/APPLY.md) | The apply pipeline end-to-end: plan computation, backup, KDE adapter, state, revert. |
| [`docs/REPOSITORIES.md`](docs/REPOSITORIES.md) | How theme repositories are discovered, the inheritance chain, qualified vs bare slugs. |

## Shell completion

Riced ships its own completion scripts embedded in the binary. Install once:

```bash
# fish
riced completion fish > ~/.config/fish/completions/riced.fish

# bash
riced completion bash > ~/.local/share/bash-completion/completions/riced
```

What gets completed:

- top-level subcommands (`apply`, `themes`, `repo`, …)
- sub-subcommands (`repo init|add|list|remove`, `new theme`, `completion fish|bash`)
- per-subcommand flags (`--dry-run`, `--yes`, `--hints`, `--from`, `--repo`, …)
- **dynamic data**: theme slugs after `riced apply <TAB>` (from `riced themes --slugs`)
  and repository names after `riced repo remove <TAB>` (from `riced repo list --names`).

## Releases

Riced ships as a static multi-arch Linux binary via goreleaser, with an SBOM (CycloneDX) and a git-cliff changelog attached to each GitHub release.

Tags drive releases — semver, **no `v` prefix**:

```bash
git tag -a 0.1.0 -m "release 0.1.0"
git push origin 0.1.0
```

For a local dry-run that doesn't publish anything:

```bash
make release-snapshot   # → ./dist/
```

## Contributing

This repository follows a strict GitFlow:

- All changes (including Dependabot PRs) target `develop`.
- `main` is the release branch — PRs to `main` come only from `develop`, `release/*`, or `hotfix/*` (enforced by CI).
- Commits follow [Conventional Commits](https://www.conventionalcommits.org/) — enforced by commitlint on every PR.

## License

TBD.
