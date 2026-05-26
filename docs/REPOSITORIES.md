# Repositories — manage theme sources

Riced doesn't ship themes itself. It applies themes from **repositories** — directories on disk (often Git repos) that contain a `themes/` subfolder with one or more `theme.toml` manifests.

This document covers the **registry** that tracks which repositories Riced knows about, the layout of `~/.riced/`, and the user-facing `riced repo` and `riced themes` commands.

## The three nesting levels

```
Repository       Theme family       Mode
─────────        ─────────────      ─────
my-themes        red                dark / light
  ↓                ↓                  ↓
git repo on    slugs that share   value of Meta.Mode
disk           Meta.Theme         on each manifest
```

- A **repository** is a directory at the root of a Git clone or a hand-curated local folder. Identified by a unique name in the registry.
- A **theme** is a coherent design within a repository (e.g. "red" — the design family). Themes are a UI grouping, not a separate file: every manifest whose `Meta.Theme` field is `"red"` belongs to the "red" theme family. Optional — themes without `Meta.Theme` show up under `(none)`.
- A **mode** is `dark` or `light`. Carried by each manifest via `Meta.Mode`. Optional. A theme family doesn't have to ship both modes; whichever manifests exist define which modes are available.

Mental model: a manifest is the atomic unit Riced applies. `Repository / Theme / Mode` is just three layers of metadata to help users find the manifest they want.

## On-disk layout

```
~/.riced/
├── repositories/             # Git clones live here (auto-created)
│   ├── my-themes/
│   └── catppuccin-kde/
├── themes/                   # User's private custom themes (auto-created)
│   └── my-red/
│       └── theme.toml
└── state/                    # currently applied theme + backups (see APPLY.md)
    └── current.toml

~/.config/riced/
└── repositories.toml         # The registry — list of registered repos
```

`~/.config/riced/repositories.toml` looks like:

```toml
schema_version = 1

[[repository]]
name = "my-themes"
path = "/home/me/dev/my-themes"

[[repository]]
name = "catppuccin-kde"
path = "/home/me/dev/catppuccin-kde"
```

The registry file is edited by `riced repo` commands, not by hand. It only stores a name and an absolute on-disk path — Riced **deliberately does not store remote URLs** because it never clones or pulls anything itself. If you want to track the upstream of a repo, put it in that repo's own `repository.toml` under `[repository] homepage = "..."`.

### Why no `git clone` / `git pull` inside Riced?

Two reasons:

1. **Trust boundary.** Cloning and pulling should use *your* git config — your SSH keys, GPG signing, credential helpers, hooks. Spawning `git` from Riced bypasses or duplicates that. The user is the right owner of "fetch source code from the network".
2. **Narrow exec surface.** Riced shells out only to KDE helpers strictly needed by `riced apply`. Adding `git` to that list grows the attack surface for no functional gain — `cd <repo>/ && git pull` is one command.

## Commands

### `riced repo init [--register] <path>`

Makes `<path>` a Riced repository. **Idempotent**: every step is "create if missing, leave alone if present". Existing files are never overwritten.

Behavior, branchless:
- if `<path>` doesn't exist → mkdir it, then create the standard scaffold (`themes/`, `wallpapers/`, `icons/` with `.gitkeep`s, `repository.toml`, `README.md`, `.gitignore`)
- if `<path>` exists without `repository.toml` → write the missing pieces only
- if `<path>` is already a complete Riced repo → no-op

The repository name registered (when `--register` is set) is `filepath.Base(<path>)`. That base name must be kebab-case (`a-z`, `0-9`, `-`).

Covers the four typical workflows:

```bash
# A) From scratch, brand new directory
riced repo init --register my-rice

# B) You cloned an existing Riced repository (already has repository.toml)
git clone https://github.com/me/my-themes ~/dev/my-themes
riced repo init --register ~/dev/my-themes     # no-op on scaffold, just registers

# C) You cloned an external git repo without a Riced marker
git clone https://github.com/someone/themes ~/dev/themes
riced repo init --register ~/dev/themes        # adds only repository.toml + the boilerplate dirs

# D) Empty git repo just cloned, you want to start authoring
git clone git@github.com:you/my-rice ~/dev/my-rice
riced repo init --register ~/dev/my-rice       # scaffolds inside the empty clone
```

After A or D you typically follow up with `riced new theme <slug>` inside the repo.

### `riced repo add <path>`

Registers an **already-initialized** repository (one that has `repository.toml`). Fails with a helpful hint if the marker is absent — running `riced repo init <path>` first is the fix.

```bash
riced repo add ~/dev/my-themes
```

The registered name is `filepath.Base(<path>)`. Duplicates fail without modifying anything.

### `riced repo list`

Prints the registered repositories as a table (NAME, PATH).

### `riced repo remove <name>`

Unregisters the named repository. **Files on disk are never deleted** — `rm -rf <path>` is your call if you want them gone.

### Update? Just use git.

Riced has no `repo update` subcommand. To pull upstream changes:

```bash
cd ~/dev/some-repo && git pull
```

That uses your git config, your credentials, your hooks. Riced doesn't need to know.

## Listing themes

### `riced themes [--repo NAME] [--theme GROUP]`

Walks every registered repository plus `~/.riced/themes/`, groups manifests by `(repository, Meta.Theme)`, and prints a compact table:

```
REPO        THEME    MODES        SLUGS
my-themes   red      dark, light  my-themes/my-red, my-themes/my-red-light
my-themes   blue     dark         my-themes/my-blue
my-themes   mixed    dark         my-themes/my-dark
my-themes   (none)   —            my-themes/my-base
catppuccin  mocha    dark         catppuccin/ctp-mocha
(user)      custom   dark         my-personal-red
```

`(none)` rows are themes without a `Meta.Theme` value — typically base themes meant for inheritance, not direct application.

`(user)` rows come from `~/.riced/themes/`. Those are referenced by bare slug (no `repo/` prefix).

Filters compose: `riced themes --repo my-themes --theme red` returns only that repository's red variants.

### `riced list <path>`

Lower-level. Walks a single directory you point at, ignoring the registry. Useful while developing a theme locally before adding the repo.

```bash
riced list ~/Dev/projects/my-themes/themes
```

## Slug resolution

When Riced needs to resolve a slug (typically in `inherits = "..."` or `riced apply`), it tries in this order:

1. **Qualified form** `<repo>/<slug>` (e.g. `my-themes/my-red`) — exact lookup in one repository.
2. **Sibling lookup** when resolving `inherits`: the parent is searched first in the current theme's parent directory (sibling themes in the same repo).
3. **User overrides** `~/.riced/themes/<slug>/`.
4. **All registered repositories** in alphabetical order.

The first match wins. Qualifying with `<repo>/` is recommended when a slug exists in more than one place.

## Scaffolding themes

Repository scaffolding is `riced repo init` (documented above). The single-theme scaffold is here.

### `riced new theme <slug>`

Creates a new theme directory with a minimal valid `theme.toml`. The target location is resolved in this order:

1. `--repo NAME` flag → `<registered-repo-path>/themes/<slug>/`
2. `repository.toml` found by walking up from CWD → `<repo>/themes/<slug>/`
3. Otherwise → `~/.riced/themes/<slug>/` (private theme)

Flags (must precede `<slug>`):
- `--from PARENT_SLUG` — emit a manifest with `inherits = "PARENT_SLUG"` and only the override fields
- `--mode dark|light` — sets `Meta.Mode` (default `dark`)
- `--repo NAME` — force the target to a specific registered repo

```bash
# Inside a repo, no inheritance
riced new theme my-first
# → themes/my-first/theme.toml  with a TODO for [wallpapers].paths

# Inheriting from a sibling base
riced new theme --from my-first my-red
# → themes/my-red/theme.toml  with only the palette override block

# Private theme, no repo
cd ~
riced new theme my-personal
# → ~/.riced/themes/my-personal/theme.toml
```

## User overrides

Drop a `theme.toml` under `~/.riced/themes/<your-slug>/`. It behaves exactly like a theme in a registered repository: it shows up in `riced themes`, can be `inherits` from another theme, can itself be referenced as a parent.

A typical use case: take an existing theme, change one thing.

```toml
# ~/.riced/themes/my-red/theme.toml
[meta]
name     = "My Red"
slug     = "my-red"
theme    = "red"
mode     = "dark"
inherits = "my-themes/my-red"

[palette]
accent = "#ff00aa"  # the only override
```

`riced themes` will list it under `(user)`. `riced apply my-red` will produce a build identical to `my-themes/my-red` except for the accent.

## Tests

The registry uses `os.UserConfigDir()` to find its file. Tests override this through `registry.SetConfigPath(...)` so they never touch the real `~/.config/`. Same approach is used in `riced` smoke tests: a temporary `HOME` + `XDG_CONFIG_HOME` makes the binary believe it's running on a fresh machine.
