# `theme.toml` — manifest schema

This document is the **contract** between Riced and any theme repository. It is versioned via the `schema_version` field; breaking changes bump that number, additive fields stay on the same version.

**Current schema version: `1`**

## Anatomy

A theme is a directory containing a `theme.toml` file. All file paths referenced from the manifest are resolved **relative to the theme directory**.

```
my-theme/
├── theme.toml        ← the manifest
├── wallpapers/       ← arbitrary layout — manifest decides what's read
│   └── …
└── icons/
    └── launcher.png
```

You may also keep your wallpapers and icons elsewhere in the repository and reference them via symlinks inside the theme directory (recommended when one wallpaper set serves several theme variants).

## Top-level fields

| Key | Type | Required | Notes |
|---|---|---|---|
| `schema_version` | int | yes | Must equal `1`. |

Plus the sections below.

---

## `[meta]`

| Key | Type | Required | Notes |
|---|---|---|---|
| `name` | string | yes | Human-readable name. Shown in `riced themes` / `riced list`. |
| `slug` | string | yes | Kebab-case identifier (`^[a-z0-9]+(-[a-z0-9]+)*$`). Unique within a repository. Used as filename stem in generated files. |
| `theme` | string | no | Optional design-family identifier (e.g. `"red"`, `"blue"`, `"mixed"`). Themes sharing a value appear grouped in `riced themes`. Purely informational. |
| `mode` | string | no | `"dark"` or `"light"`. Indicates the intended color mode of this manifest. Dark/light is not enforced — ship the variants you have. |
| `inherits` | string | no | Slug of a parent theme whose fields are merged underneath this one. See [Inheritance](#inheritance). |
| `author` | string | no | Free-form. |

```toml
[meta]
name    = "My Theme"
slug    = "my-theme"
theme   = "mixed"
mode    = "dark"
author  = "Jane"
```

### Inheritance

When `inherits = "<slug>"` is set, Riced resolves the parent (searched in this order: siblings of the current theme dir → registered repositories → `~/.riced/themes/`) and deep-merges the parent's fields underneath this manifest. Scalars from the child override the parent; arrays from the child replace the parent's arrays (no append). Path fields inherited from a parent are rewritten to absolute paths so they resolve correctly. Multi-level inheritance chains are supported; cycles error out cleanly.

To explicitly **clear** a parent's array (e.g. switching from a flat `paths` list to per-screen rules — see [Wallpapers](#wallpapers)), set the field to an empty array in the child:

```toml
[wallpapers]
paths = []           # clears the inherited flat list
[[wallpapers.screens]]
…
```

---

## `[palette]`

Every color is a hex string, either `#RRGGBB` or `#RRGGBBAA` (case-insensitive).

| Key | Required | Role |
|---|---|---|
| `bg`      | yes | Window background, panel background. |
| `text`    | yes | Primary foreground text. |
| `accent`  | yes | Primary accent: selection, highlight, focus. |
| `surface` | no  | Elevated surfaces (cards, popups). |
| `subtext` | no  | Secondary text. |
| `muted`   | no  | Disabled / placeholder text. |
| `accent2` | no  | Secondary accent. |
| `accent3` | no  | Tertiary accent. |
| `warning` | no  | Warning / caution states. |
| `error`   | no  | Errors / destructive actions. |

```toml
[palette]
bg     = "#0a0a14"
text   = "#e8e8f0"
accent = "#ff2a4b"
```

---

## `[wallpapers]`

Riced supports three layouts: a single image, a flat slideshow mirrored across every screen, or a per-screen rule cascade for multi-monitor setups (including mixed orientations).

### Common fields

| Key | Type | Default | Notes |
|---|---|---|---|
| `mode` | string | `"single"` | `"single"` or `"slideshow"`. |
| `interval` | int (seconds) | `600` for slideshow | Required when `mode = "slideshow"`. |
| `paths` | array of string | — | Flat list, mirrored across every screen. Mutually exclusive with `[[wallpapers.screens]]`. |
| `lock_image` | string | — | Wallpaper shown on the lock screen. Optional; when empty the lock screen keeps Plasma's existing wallpaper. Written to `kscreenlockerrc` Greeter/Wallpaper/org.kde.image/General/Image. |
| `mirror` | bool | `false` | Explicit alias for the flat-mirror semantics. Mutually exclusive with `[[wallpapers.screens]]`. |
| `screens` | array of tables | — | Per-screen rule cascade. See below. Requires `mode = "slideshow"`; combining with `mode = "single"` is rejected by `riced validate`. |

### Single mode

```toml
[wallpapers]
mode  = "single"
paths = ["wallpapers/main.png"]
```

Riced reads `paths[0]` and applies it via `plasma-apply-wallpaperimage`. Extra entries are ignored.

### Slideshow mirror (flat list, applied to every screen)

```toml
[wallpapers]
mode     = "slideshow"
interval = 600
paths    = ["wallpapers/a.png", "wallpapers/b.png", "wallpapers/c.png"]
```

Every screen receives the same image pool and cycles independently (Plasma's slideshow plugin is per-screen, even with identical config). `mirror = true` is an explicit synonym — same behavior.

### Per-screen rule cascade

When you want different lists on different screens — including orientation-aware portraits for vertical monitors — use `[[wallpapers.screens]]`. Each entry sets exactly **one** match criterion plus its own `paths`. Riced evaluates rules **in manifest order** for each desktop and applies the first one that matches (CSS-cascade semantics).

```toml
[wallpapers]
mode     = "slideshow"
interval = 600

[[wallpapers.screens]]
index = 0                              # most specific: pin to Plasma screen #0
paths = ["wallpapers/dashboard.png"]

[[wallpapers.screens]]
orientation = "vertical"               # detected at apply time via screenGeometry()
paths = ["wallpapers/portrait-1.png", "wallpapers/portrait-2.png"]

[[wallpapers.screens]]
orientation = "horizontal"
paths = ["wallpapers/landscape-1.png", "wallpapers/landscape-2.png"]

[[wallpapers.screens]]
match = "*"                            # fallback catch-all
paths = ["wallpapers/default.png"]
```

**Criteria** (set exactly one per entry):

| Criterion | Type | Matches |
|---|---|---|
| `index` | int | The Plasma desktop with `screen == index` (0-based). |
| `orientation` | `"vertical"` \| `"horizontal"` | Any screen whose `height > width` (or vice-versa) at apply time. |
| `match` | `"*"` | Any screen not yet claimed by an earlier rule (catch-all). |

**Robustness:**

- A criterion that matches nothing on the current machine (e.g. `index = 5` on a 2-screen rig, or `orientation = "vertical"` when all screens are horizontal) is a silent no-op. The remaining rules still evaluate normally.
- A screen claimed by no rule keeps its current wallpaper. Apply does not crash, does not warn loudly.
- Validation enforces: exactly one criterion per entry, no duplicate `index`/`orientation`/`match` across entries, `paths` non-empty, `index >= 0`, `orientation` in `{vertical, horizontal}`. Per-screen lists also require `mode = "slideshow"` — combining them with `mode = "single"` is rejected.

---

## `[panel]`

| Key | Type | Notes |
|---|---|---|
| `position` | string | `"top"`, `"bottom"`, `"left"`, or `"right"`. |
| `floating` | bool | KDE Plasma floating panel. |
| `height` | int | Pixels. `0` means "Plasma default". |
| `launcher_icon` | string | Path (relative to theme dir) to the icon that replaces the default launcher icon (e.g. the distro logo). |

```toml
[panel]
position      = "bottom"
floating      = true
height        = 44
launcher_icon = "icons/launcher.png"
```

---

## `[window]`

| Key | Type | Notes |
|---|---|---|
| `decoration` | string | Three syntaxes (since 0.1.4): bare names `"klassy"` / `"breeze"` / `"oxygen"` for the C++ KWin libraries; `"aurorae:<theme-dir-name>"` for any aurorae SVG theme under `~/.local/share/aurorae/themes/`; `"library:<id>"` escape hatch to point at any hand-installed `org.kde.kdecoration2/3` plugin (`paru -S darkly` + `"library:org.kde.darkly"`). The library / theme keys are written under `[org.kde.kdecoration2]` in `kwinrc`; KWin reads it for Plasma 5 and 6. See [docs/RECIPES.md](RECIPES.md) for the install patterns. |
| `animations` | string | `"magic-lamp"`, `"scale"`, `"glide"`, `"fade"`, or `"none"`. Only one is on at a time; Riced disables the others. |
| `animation_speed` | int (0–6) | KWin's global animation slider position. `0` = instant, `3` = KDE default, `6` = very slow (~1 s cap). Optional; omit to leave the user's current value alone. |
| `animation_duration_ms` | int (0–10000) | Per-effect duration override in milliseconds. Bypasses the slider cap. Written to `[Effect-<animations>] AnimationDuration` in `kwinrc`. Useful when the slider's `6` is still too fast for visual demos (try `2000`). |
| `blur` | bool | KWin blur effect. Visible behind semi-transparent regions (menus, Konsole with `opacity < 1`). |
| `wobbly` | bool | KWin wobbly-windows effect. Off by default. |

```toml
[window]
decoration            = "klassy"
animations            = "magic-lamp"
animation_speed       = 6     # slider max, ~1 s
animation_duration_ms = 2000  # explicit 2 s override
blur                  = true
wobbly                = false
```

---

## `[fonts]`

| Key | Type | Notes |
|---|---|---|
| `ui` | string | UI font family. Must be installed on the target system. |
| `mono` | string | Monospace font for Konsole and code. |

```toml
[fonts]
ui   = "Inter"
mono = "JetBrains Mono"
```

---

## `[terminal]`

| Key | Type | Notes |
|---|---|---|
| `opacity` | float | `0.0` (fully transparent) to `1.0` (opaque). Konsole reads this from the colorscheme's `[General]` section; KWin blur is what makes it look like frosted glass. |

```toml
[terminal]
opacity = 0.80
```

---

## `[icons]`, `[cursors]`, `[plasma]`

| Section | Key | Notes |
|---|---|---|
| `[icons]`   | `theme` | Icon theme directory name (under `/usr/share/icons/` or `~/.local/share/icons/`). Applied via `kwriteconfig6 kdeglobals Icons Theme`, followed by `kbuildsycoca6 --noincremental` so newly-launched apps pick up the change without a log out / log in. |
| `[cursors]` | `theme` | Cursor theme directory name (e.g. `capitaine-cursors`). Applied via `plasma-apply-cursortheme`. |
| `[plasma]`  | `desktop_theme` | Plasma Style name (panel widgets, popups). Applied via `plasma-apply-desktoptheme`. |

Each value is the theme's **installed directory name**, not a path. Theme must already exist on the system (or be brought in by an `[[looks]]` entry — see below).

---

## `[splash]`, `[widget_style]`, `[notifications]` (since 0.1.4)

Three small surface additions, each one extra `kwriteconfig6` call.

| Section | Key | Notes |
|---|---|---|
| `[splash]` | `theme` | Plasma boot splash theme (shown while Plasma loads). Written to `~/.config/ksplashrc` `KSplash/Theme`. Effect visible at the next session start, not during the current one. |
| `[widget_style]` | `name` | Qt widget style applied to every app launched after the apply (`"Breeze"`, `"kvantum"`, `"kvantum-dark"`, `"Oxygen"`, ...). Written to `~/.config/kdeglobals` `KDE/widgetStyle`. Distinct from `[plasma].desktop_theme`: widget style is "how apps look inside their windows", desktop theme is "how the panel + popups look". |
| `[notifications]` | `position` | Plasma notification popup anchor. One of: `"TopRight"`, `"TopCenter"`, `"TopLeft"`, `"BottomRight"`, `"BottomCenter"`, `"BottomLeft"`, `"CloseToWidget"` (Plasma default = near the system tray). Written to `~/.config/plasmanotifyrc` `General/PopupPosition`. |

```toml
[splash]
theme = "Catppuccin-Mocha-Maroon"

[widget_style]
name = "kvantum-dark"

[notifications]
position = "TopRight"
```

---

## Family themes via `[meta.modes]` (since 0.1.4)

A "family" theme has no palette / wallpapers / icons of its own. Its `[meta.modes]` table just points at two sibling slugs — one for dark, one for light — and the CLI picks which one to apply at call time.

```toml
[meta]
name = "My Design"
slug = "my-design"

[meta.modes]
dark    = "my-design-dark"   # slug of the dark variant
light   = "my-design-light"  # slug of the light variant
default = "dark"             # picked when --mode is omitted
```

The two sibling themes (`my-design-dark` and `my-design-light`) live as full, independent manifests with their own palette, wallpapers, etc. — they are **not** inherited from the family file.

Usage:

```bash
riced apply my-design --mode=light    # explicit
riced apply my-design                 # uses meta.modes.default
riced switch                          # toggles current family's sibling
```

Family-theme validation:

- Validation **skips** the per-section requirements (no palette mandatory, no wallpapers mandatory) — a family is a delegation table.
- Both `dark` and `light` may be omitted, but at least one must be present.
- `default` accepts `"dark"` or `"light"` only.
- The referenced slugs must be resolvable through the registry at apply time.

See `examples/family/` and [docs/RECIPES.md](RECIPES.md#author-a-family-theme-that-toggles-between-dark-and-light).

```toml
[icons]
theme = "breeze-dark"

[cursors]
theme = "capitaine-cursors"

[plasma]
desktop_theme = "default"
```

---

## `[lookandfeel]` — Plasma Global Theme

A "Global Theme" is a single KPackage that bundles a colorscheme, cursor theme, decoration, plasma theme and icons. Applying one resets all of those at once. Riced applies `lookandfeel` **before** the individual overrides (`[palette]`, `[icons]`, `[cursors]`, etc.) so the theme-specific fields can win.

```toml
[lookandfeel]
package = "org.kde.breezedark.desktop"
```

List installed candidates with `kpackagetool6 -t Plasma/LookAndFeel --list`. New ones can be brought in via `[[looks]]`.

---

## `[[looks]]` — community theme assets

Themes from [store.kde.org](https://store.kde.org/), github releases, or local git clones that Riced installs **before** the rest of the manifest runs. Each entry produces one `external` action in the apply plan that the user reviews before consent.

### Sources: URL or Local

Exactly one of `url` (with mandatory `sha256`) or `local` (without `sha256`) per entry.

**URL** — Riced downloads, sha256-verifies, then dispatches to the install strategy:
```toml
[[looks]]
name   = "Tela Dark Icons"
type   = "icons"
url    = "https://github.com/vinceliuice/Tela-icon-theme/archive/refs/tags/2024-04-20.tar.gz"
sha256 = "abc...64chars"        # strict 64-char hex (lowercase or uppercase), validated up front
install = "script"           # required because Tela ships install.sh, not extract-ready
script  = "install.sh"
args    = ["-d", "$HOME/.local/share/icons"]
```

**Local** — Riced points at a user-managed clone under `~/.riced/looks/<name>/`. Symmetric with `~/.riced/repositories/`: the user owns `git clone` / `git pull`, Riced just runs the install strategy on the local dir:
```bash
git clone https://github.com/foo/CustomIcons ~/.riced/looks/custom-icons
```
```toml
[[looks]]
name    = "Custom Icons"
type    = "icons"
local   = "custom-icons"
install = "script"
script  = "./install.sh"
```

Local sources skip sha256 (trust = whatever the user cloned), parallel to how `riced repo add` treats theme repositories. Reasoning in [`docs/REPOSITORIES.md`](REPOSITORIES.md#why-no-git-clone--git-pull-inside-riced).

### Install strategies

| `install` | Behavior |
|---|---|
| `auto` (default) | Inspect the (normalized) source: `metadata.json` at root → `kpackage`; `index.theme` at root → `extract`; `install.sh` present → refuse (asks user to set `install = "script"` explicitly). |
| `kpackage` | `kpackagetool6 -t <type> -i <source>` (with `-u` upgrade fallback). |
| `extract` | Copy contents into `~/.local/share/icons/`. |
| `script` | Exec `/bin/bash <source>/<script> <args...>`. Requires explicit opt-in. |

**Normalize step** runs before any strategy: if the source root has a single subdirectory and nothing else (the github-source layout `<repo>-<tag>/...`), Riced descends into it. So `script = "install.sh"` works whether the tarball wraps everything in `Tela-icon-theme-2024-04-20/` or not — the user doesn't need to know the wrapper name.

**Script safety**:
- `script` must be a **relative** path inside the (normalized) source (no `..`, no leading `/`).
- `args` supports `$HOME` / `$XDG_DATA_HOME` / `$XDG_CONFIG_HOME` expansion only — no shell substitution, no other env reads.
- The exact command lands in the apply plan for user consent before execution.
- `auto` NEVER picks `script` silently. Explicit opt-in is required.

### Where to find packages

- **[store.kde.org](https://store.kde.org/)** — the official KDE store. Filter by category: *Global Themes*, *Plasma Themes*, *Icon Themes*, *Cursors*, *Window Decorations*. Each product page has a Download button — right-click → copy link.
- **[kde-look.org](https://www.pling.com/browse/cat/100/)** — alias of store.kde.org with the same content.
- **GitHub releases** — many popular authors (Vinceliuice's WhiteSur/Layan, Tela, Dracula, Tokyo Night) publish parallel github releases. `archive/refs/tags/*.tar.gz` is fine with `install = "script"`; for `install = "kpackage"` you need a kpackage-shaped tarball (with `metadata.json` at the root) which most github source archives don't provide.

### Computing the SHA-256

```bash
curl -L -o pack.tar.gz "<the URL>"
sha256sum pack.tar.gz
# → 4e9d4...  pack.tar.gz
```

Paste the 64-char hex into the manifest. Riced refuses to install without it: the hash is the only thing standing between a trusted manifest and a swapped-out malicious archive. Local sources skip this check by design.

Archives are cached at `~/.riced/cache/external/<sha-prefix>-<basename>`. Re-applying a theme with the same SHA skips the download. **Riced does not browse `store.kde.org`** — you (the theme author) supply the URL + hash manually for now.

---

## Validation behavior

`riced generate` and `riced apply` validate the manifest before doing any work. Validation runs all checks and reports **every** issue at once rather than stopping at the first — so you fix the manifest in one pass, not one error per round-trip.

Unknown keys (e.g. fields introduced by a newer Riced version) produce a soft warning but do not fail validation: an older Riced will still apply a manifest written for a newer schema, ignoring fields it does not understand.

## Complete example

See [`internal/manifest/testdata/valid/theme.toml`](../internal/manifest/testdata/valid/theme.toml) inside this repo for a known-good fixture exercising every section.

## Editor integration

A JSON Schema describing this manifest lives at [`schemas/theme.schema.json`](../schemas/theme.schema.json). It powers:

- **Autocomplete** on every field name (type `[me<Tab>` and your editor suggests `meta`).
- **Inline validation** with line/column-accurate diagnostics: wrong enum values, malformed hex colors, path traversal segments, slug typos.
- **Hover documentation**: each field's description is displayed when you hover.

### How editors pick it up

Riced ships a `.taplo.toml` rule that pins the schema to every `theme.toml` in the workspace (excluding our own intentionally-broken test fixtures). Any editor with the [taplo](https://taplo.tamasfe.dev/) LSP picks it up automatically:

- **VSCode**: install the official "Even Better TOML" extension. Open the Riced repo; autocomplete + diagnostics activate on `theme.toml`.
- **Helix / Neovim / Zed**: install the `taplo` binary (`paru -S taplo-cli` on Arch) and ensure your TOML LSP config points at it.

### CLI validation alongside the LSP

The same schema can be invoked from the command line:

```bash
make lint-toml
# runs both `taplo format --check` (formatting) and `taplo lint` (schema)
```

`riced validate <theme-dir>` remains the authoritative runtime check — it merges inheritance and verifies file existence, which JSON Schema can't do.

### Important nuance: schema vs runtime validation

The schema is intentionally lenient on **required palette fields**. A child theme that inherits its `bg` / `text` / `accent` from a parent is valid against the schema even though it doesn't declare those keys. The runtime validator (`riced validate`) catches the missing-after-merge case by walking the inheritance chain first.

Use the schema for fast edit-time feedback. Use `riced validate` for "is this manifest actually applyable".

## Layers of validation

Riced and taplo each handle a different concern. They're independent on purpose — Riced is the runtime authority for "does this theme apply correctly", taplo is the dev/CI authority for "is this file stylistically clean".

| Concern | Tool | When |
|---|---|---|
| TOML syntax (the file parses) | Riced (`BurntSushi/toml`) | Always: any `riced` command that loads the manifest |
| Semantic correctness (hex format, enums, paths exist, slug shape, opacity range) | Riced (`manifest.Validate`) | `riced validate` and on every `riced apply` |
| Style / formatting (alignment, key order, trailing newline) | taplo | `make lint-toml`, your CI, your editor LSP |
| Schema validation (autocomplete + inline errors as you type) | taplo + this JSON Schema | Editor LSP, `taplo lint --schema ...` |

`riced validate --hints` prints a reminder of the taplo invocations for the formatting half — purely informational. **Riced never shells out to taplo at runtime.** That's a deliberate design choice: the binary's exec surface stays narrow (only the KDE helpers required to actually apply a theme). Formatting is a dev/CI concern, not a runtime concern.
