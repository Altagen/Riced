# Recipes

Concrete how-to snippets for the situations that come up most often when
authoring themes. Each one is a self-contained block you can copy into a
`theme.toml` (or a CLI session) and adapt.

---

## Use a KDE Global Theme as a visual base, override only what you care about

`[lookandfeel]` is applied **before** the rest, so anything you redeclare
afterwards wins. Drop a section to inherit it from the look-and-feel
defaults.

```toml
[lookandfeel]
package = "Catppuccin-Mocha-Maroon"

[palette]
# your own colors -- override the look-and-feel's palette
bg     = "#070406"
text   = "#f0e8e8"
accent = "#ff2a4b"

[icons]
theme = "Tela-dark"   # override the look-and-feel's icons

# Drop [cursors] to inherit the look-and-feel's cursor theme.
```

See `examples/global-theme-override/` for a complete example.

---

## Author a "family" theme that toggles between dark and light

Each variant is a regular theme. The family is a one-table manifest that
points at them. Apply with `--mode`, switch with `riced switch`.

```toml
# themes/my-design/theme.toml -- the family
schema_version = 1

[meta]
name = "My Design"
slug = "my-design"

[meta.modes]
dark    = "my-design-dark"   # slug of an existing theme
light   = "my-design-light"
default = "dark"             # used when --mode is omitted
```

Then `themes/my-design-dark/theme.toml` and `themes/my-design-light/theme.toml`
are full themes with their own palette / wallpapers / icons.

```bash
riced apply my-design --mode=light    # explicit
riced apply my-design                 # uses meta.modes.default
riced switch                          # toggles current variant's sibling
```

See `examples/family/` for the umbrella template.

---

## Switch dark <-> light automatically at fixed hours

`riced schedule` installs systemd **user** timers (no sudo, no system
units). Two timers fire at the boundaries; their services invoke
`riced apply --yes --mode=<dark|light> <family>`.

```bash
# Light from 07:00 to 19:00, dark otherwise.
riced schedule install my-design --day 07:00-19:00

# Inspect what's installed
riced schedule list
systemctl --user list-timers riced-mode-*

# Tear down
riced schedule uninstall my-design
```

systemd handles DST and clock skew via `OnCalendar`, not Riced. The
absolute path of the running `riced` binary is baked into the unit at
install time so a later `$PATH` change does not break it.

---

## Install an Aurorae window decoration from KDE Store + use it

Aurorae themes are pure SVG -- Riced's `[[looks]]` install strategy
`"extract"` handles them with no compilation step.

```toml
[[looks]]
name    = "Some Aurorae"
type    = "KWin/Aurorae"
url     = "https://github.com/<author>/<repo>/archive/refs/tags/<version>.tar.gz"
sha256  = "..."                # sha256sum the tarball before committing
install = "extract"            # SVG only -- no script run

[window]
decoration = "aurorae:<theme-dir-name>"   # the dir created under ~/.local/share/aurorae/themes/
```

Replace `<theme-dir-name>` with the exact name of the directory the
archive lands as. Most github source archives wrap everything in
`<repo>-<tag>/...`; Riced's normalize step strips that wrapper, so the
theme dir is the inner one.

---

## Use a hand-installed C++ KWin decoration (Darkly, SierraBreezeEnhanced, ...)

C++ KWin decorations need `cmake` + `sudo make install` -- well outside
Riced's `[[looks]]` script install. **Riced does not download or build
these packages**; the supported workflow is install via your distro,
then point Riced at the result:

1. Install via your distro:

   ```bash
   paru -S darkly                # AUR on Arch / CachyOS
   ```

2. Find the library id the package exposes:

   ```bash
   ls /usr/lib/qt6/plugins/org.kde.kdecoration2/  # the .so name minus the prefix
   ```

3. Point Riced at it with the `library:` escape hatch:

   ```toml
   [window]
   decoration = "library:org.kde.darkly"
   ```

This works for any KDecoration2 library installed system-wide.
See [TROUBLESHOOTING.md](TROUBLESHOOTING.md) for the longer story on
why Riced does not compile from source.

---

## Set an icon theme with a deeper red tint (or any variant)

Tela ships color variants (Tela-red, Tela-blue, ...). Install the one
you want via the official installer, then reference it:

```toml
[icons]
theme = "Tela-red-dark"
```

The `kbuildsycoca6 --noincremental` call Riced fires after the icon
write makes newly-launched apps see the change without a log out.
Already-open apps keep their stale cache -- close + reopen them.
See [TROUBLESHOOTING.md](TROUBLESHOOTING.md#icons-swap-but-already-open-apps-still-show-the-old-theme).

---

## Per-screen wallpapers with mixed orientation

For a multi-monitor rig with at least one vertical screen, the
per-screen cascade picks the right list per geometry:

```toml
[wallpapers]
mode     = "slideshow"
interval = 600

[[wallpapers.screens]]
orientation = "vertical"
paths = ["wallpapers/portrait-01.png", "wallpapers/portrait-02.png"]

[[wallpapers.screens]]
match = "*"                 # everything else -- catch-all, must come last
paths = ["wallpapers/landscape-01.png", "wallpapers/landscape-02.png"]
```

Rules evaluate in manifest order, CSS cascade style. A screen claimed
by no rule keeps its current wallpaper.

---

## Add a lock screen wallpaper that differs from the desktop set

Lock screen is a single image (Plasma applies it identically across
every monitor). Drop it under `wallpapers/` next to the others and set
`lock_image`:

```toml
[wallpapers]
mode       = "slideshow"
interval   = 600
lock_image = "wallpapers/lock-bg.png"
paths      = ["wallpapers/01.png", "wallpapers/02.png"]
```

---

## Set a panel layout (position, floating, height)

Since 0.1.4, all three knobs work end-to-end -- they used to be
silently dropped:

```toml
[panel]
position      = "bottom"   # "top" | "bottom" | "left" | "right"
floating      = true
height        = 44
launcher_icon = "icons/launcher.svg"
```

The launcher icon is a path relative to the theme dir; Riced symlinks
it under `~/.local/share/riced/icons/<slug>-launcher.<ext>` and points
the kickoff / kicker / kickerdash applets at it.

---

## Slim window decoration with rounded corners

For a quick "minimal but pretty" titlebar without installing anything
new, point Riced at Plasma's default Breeze and let Klassy's corner
radius win on the windows themselves:

```toml
[window]
decoration = "breeze"
blur       = true

# Use animations sparingly so the slim look feels snappy
animations            = "scale"
animation_duration_ms = 180
```
