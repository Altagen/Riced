# Applying themes — `apply`, `status`, `revert`, `doctor`

Riced isn't just a generator. Once a theme is rendered, the `apply` command pushes the result into the live KDE Plasma 6 session: it copies files into `~/.config/` and `~/.local/share/`, invokes the canonical `plasma-apply-*` and `kwriteconfig6` helpers, runs scripted JS against `plasmashell` for the bits KDE has no CLI for (launcher icon, slideshow), and records what it did so a single `riced revert` can undo it.

This document covers the apply pipeline end-to-end, the safety guarantees, and the state files Riced maintains under `~/.riced/state/`.

## What `apply` actually does

```bash
riced apply [--dry-run] [--yes] [--out DIR] <slug>
```

Pipeline, in order:

1. **Resolve** the slug through the registry (`registry.Source`). Accepts bare slugs (`my-dark`) or qualified ones (`my-repo/my-dark`).
2. **Load + validate** the manifest. Walks the `inherits` chain, deep-merges parents, then runs the same `Validate()` as `riced validate`. Abort here on any issue.
3. **Generate** into `~/.riced/cache/<slug>/` (override with `--out`). Same renderer pipeline as `riced generate` — color scheme, Konsole, GTK, wallpapers (flat or per-screen), launcher icon, KWin/Klassy/Kvantum settings.
4. **Load previous state** from `~/.riced/state/current.toml`. Used by the planner to mark Riced-managed files (already on disk from a previous apply) as non-backupable and to inherit a sticky `BackupRoot` (see [Sticky BackupRoot](#sticky-backuproot)).
5. **Build a Plan**: a deterministic list of file actions (`mkdir`, `copy`, `symlink`) plus KDE side-effects. The plan is computed up front so dry-run can print it and confirmation can quote it.
6. **Display** the plan and (unless `--yes`) prompt `Proceed? [y/N]`. Anything other than `y` / `yes` aborts without writing anything.
7. **Check prerequisites** before any write: `plasma-apply-colorscheme`, `plasma-apply-wallpaperimage`, `plasma-apply-cursortheme`, `plasma-apply-desktoptheme`, `plasma-apply-lookandfeel`, `kwriteconfig6`, `kbuildsycoca6`, `kpackagetool6`, a qdbus binary (`qdbus6` or `qdbus`), plus `tar` and `unzip` (for the `looks` extract strategy) must be on `PATH`. `plasmashell` must respond to a DBus probe within 5 seconds. Missing tooling aborts with an actionable message — no half-applied state.
8. **Acquire the lock** (`~/.riced/state/.lock`, atomic `O_CREATE|O_EXCL`).
9. **Cleanup previous apply**: files that the previous theme wrote but that the new plan won't re-write (slug-scoped `.colors` / `.profile` files when switching themes) are removed and empty Riced-managed parent dirs are pruned.
10. **Execute** the plan:
    - For each `copy`/`symlink` whose destination already exists **AND** isn't a Riced-managed file from a previous apply, **back up the original** to the backup root before overwriting.
    - Copy/symlink the rendered file into place (atomic tmp+rename).
    - Invoke the KDE adapter for `plasma-apply-*`, `kwriteconfig6`, `kbuildsycoca6` (after an icon theme write), `set-launcher-icon`, `set-wallpaper-slideshow`, and `qdbus6 reconfigure`.
11. **Record state** in `~/.riced/state/current.toml`: slug, repo, timestamp, every path written, backup root.

`--dry-run` exits between steps 6 and 7: plan is printed, nothing else happens.

State persistence is **defer-protected**: if step 10 errors mid-flight (e.g. a `kwriteconfig6` invocation fails on the third key), the state file is still saved with the list of paths that were successfully written before the failure. `riced revert` can clean those up — there are no orphans Riced doesn't know about.

## Destination mapping

The current `Targets.Map` integrates these renderer outputs:

| Generated artifact | Live destination |
|---|---|
| `<slug>.colors` | `<XDG_DATA_HOME>/color-schemes/<slug>.colors` |
| `konsole/<slug>.colorscheme` | `<XDG_DATA_HOME>/konsole/<slug>.colorscheme` |
| `konsole/<slug>.profile` | `<XDG_DATA_HOME>/konsole/<slug>.profile` |
| `gtk-3.0/gtk.css` | `<XDG_CONFIG_HOME>/gtk-3.0/gtk.css` |
| `gtk-4.0/gtk.css` | `<XDG_CONFIG_HOME>/gtk-4.0/gtk.css` |
| `wallpapers/NN-*.png` (flat) | `<XDG_DATA_HOME>/riced/wallpapers/<slug>/NN-*.png` (symlinks) |
| `wallpapers/<criterion>/NN-*.png` (per-screen) | `<XDG_DATA_HOME>/riced/wallpapers/<slug>/<criterion>/NN-*.png` |
| `icons/launcher.*` | `<XDG_DATA_HOME>/riced/icons/<slug>-launcher.*` (symlink), then a `set-launcher-icon` KDE action runs Plasma's JS API via qdbus to swap every kickoff / kicker / kickerdash applet's icon |

`XDG_DATA_HOME` defaults to `$HOME/.local/share`, `XDG_CONFIG_HOME` to `$HOME/.config`. Both env vars are honored when set.

KWin, Klassy, and Kvantum settings don't go through a generated file at all — the renderer (`render.KWinSettings`, `render.KlassySettings`, `render.KvantumSettings`) returns a structured list of `(file, group, key, value)` tuples and the apply layer turns each into a `kwriteconfig6` invocation. This is auditable in the dry-run plan: every key Riced touches in `~/.config/kwinrc`, `~/.config/klassyrc`, and `~/.config/Kvantum/kvantum.kvconfig` shows up as a `kde` line before any consent.

## KDE side-effect surface

Every line tagged `kde` in the apply plan maps to one of these adapter methods on the `apply.KDE` interface:

| Plan src | Adapter call | Underlying exec |
|---|---|---|
| `plasma-apply-colorscheme <slug>` | `ApplyColorScheme(slug)` | `plasma-apply-colorscheme <slug>` |
| `plasma-apply-wallpaperimage <path>` | `SetWallpaperImage(path)` | `plasma-apply-wallpaperimage <path>` (single mode only) |
| `kwriteconfig6-konsole-default <filename>` | `SetKonsoleDefaultProfile(...)` | `kwriteconfig6 --file konsolerc --group "Desktop Entry" --key DefaultProfile ...` |
| `kwriteconfig6 <file> <group> <key> <value>` | `WriteINIKey(...)` | `kwriteconfig6 --file <file> --group <group> --key <key> <value>` |
| `set-launcher-icon <path>` | `SetLauncherIcon(path)` | `qdbus6 org.kde.plasmashell /PlasmaShell evaluateScript "<JS>"` — walks `panels()` → `widgets()`, swaps the `icon` config key on every kickoff/kicker/kickerdash applet. |
| `set-wallpaper-slideshow <interval>` (Args = rule list) | `SetWallpaperSlideshow(rules, interval)` | `qdbus6 … evaluateScript "<JS>"` — walks `desktops()`, evaluates rules CSS-cascade-style per screen, writes `SlidePaths` / `SlideInterval` / `FillMode`. |
| `qdbus6 org.kde.KWin /KWin reconfigure` | `ReconfigureKWin()` | `qdbus6 org.kde.KWin /KWin reconfigure` — picks up the `kwriteconfig6` edits. |
| `kbuildsycoca6` | `RefreshSystemCache()` | `kbuildsycoca6 --noincremental` — emitted right after the `kdeglobals` `Icons/Theme` write so newly-launched apps see the new icon theme without a log out / log in. |

### qdbus6 vs qdbus

Plasma 6 ships `qdbus6` on most distros; some ship a bare `qdbus` that is actually the Qt 6 variant. Riced resolves the binary name once at package init by walking PATH (`qdbus6` preferred, `qdbus` fallback) and uses the resolved name everywhere — including the displayed plan, so the user sees the actual command that will run. `riced doctor` and the prerequisite check accept either binary.

### Slideshow rule cascade

For multi-screen rigs with per-screen rules, the JS sent to plasmashell is:

```js
var rules = [ {kind, value, path}, … ];   // manifest order
for each desktop {
  for each rule in order {
    if (kind === "idx"   && desktop.screen === parseInt(value)) → match
    if (kind === "orient") {
       var g = screenGeometry(desktop.screen);
       var isVert = g.height > g.width;
       if ((value === "vertical" && isVert) || (value === "horizontal" && !isVert)) → match
    }
    if (kind === "match" && value === "*")                                          → match
  }
  if (match) writeConfig(SlidePaths, [match.path]);
}
```

Desktops that no rule claims keep their current wallpaper.

## Safety guarantees

- **Confirmation is mandatory in interactive mode.** Without `--yes`, anything other than an explicit `y`/`yes` aborts. EOF on stdin = abort.
- **Files are backed up before overwrite — but only originals, never Riced's own output.** The planner marks any destination listed in `prev.WrittenAt` as non-backupable: re-applying a theme will not overwrite the previous backup of the user's genuine pre-Riced state with Riced's own output (a class of silent data loss this codebase has a regression test for).
- **Sticky BackupRoot.** When a previous apply already has a backup directory that exists on disk, the new apply **reuses** it instead of creating a new timestamped one. State always points at a single backup root — the original one capturing the user's pre-Riced state.
- **Prereq check before lock.** The full set (`plasma-apply-*`, `kwriteconfig6`, `kbuildsycoca6`, `kpackagetool6`, `qdbus6`, `tar`, `unzip`) must be on `PATH` before the lock is taken. Wrong desktop environment? Clear error, no half-applied state, no orphan lock.
- **State persisted on failure.** Execute uses a deferred state save so even a partial apply records every path successfully written. `riced revert` can clean them.
- **The KDE side-effect surface is mockable.** `apply.KDE` is an interface. Tests use `apply.FakeKDE` which records calls; production uses `apply.RealKDE` which shells out. The apply pipeline can be tested end-to-end without ever touching a live session.
- **Cross-process exclusion via a filesystem lock.** A file at `~/.riced/state/.lock` is created with `O_CREATE|O_EXCL` (POSIX-atomic on local filesystems) before any mutating operation. `apply`, `revert`, and `clean-backups` all acquire it; the second concurrent invocation fails with a clear error naming the PID of the holder. `doctor` does NOT take the lock — it's read-only and should report the situation honestly even when an apply is mid-flight.

### Sticky BackupRoot

The naive design — "every apply gets a new timestamped backup root" — has a silent-corruption hazard: re-apply theme A on top of theme A, the planner sees the existing GTK files on disk, marks them backupable, copies them into a new backup root … but the on-disk content is now Riced's own output from the first apply. The user's genuine original is shadowed by Riced's output in the new backup; the *previous* backup (with the actual original) is no longer referenced by `state.toml`. `revert` would restore Riced's output.

Riced avoids this in two ways:

1. **Skip-managed-files**: the planner reads `prev.WrittenAt` from the previous state. Any destination in that set is marked `PreexistingBackupable = false` — re-apply does not back up Riced-owned files.
2. **Sticky BackupRoot**: when `prev.BackupRoot` exists on disk, the new apply reuses it. The state file always points at the *original* backup directory, never at a stale empty one.

`state.BackupRoot` is also omitted entirely on a clean install where nothing pre-existed (no backups means no root to record). That fixes a `doctor` false positive where a phantom path was flagged as "missing".

### Concurrency table

| Scenario | Guarded |
|---|---|
| `apply` ↔ `apply` | ✅ |
| `apply` ↔ `revert` | ✅ |
| `apply` ↔ `clean-backups` | ✅ |
| `revert` ↔ `revert` | ✅ |
| `revert` ↔ `clean-backups` | ✅ |
| `clean-backups` ↔ `clean-backups` | ✅ |
| `repo {add,remove,update}` | atomic file write (last-writer-wins, no corruption) |
| Read-only commands (`status`, `themes`, `list`, `validate`, `generate`, `doctor`) | no lock needed — they don't mutate |

### Stale lock recovery

If Riced crashes mid-apply, the lock file is left behind. Subsequent `apply` / `revert` / `clean-backups` will refuse to run with an error quoting the previous holder's PID. To recover:

```bash
# Confirm no riced is actually running, then:
rm ~/.riced/state/.lock
```

`riced doctor` flags a stale lock as an `issue`, which is the canonical way to discover one.

## `riced status`

Reads `~/.riced/state/current.toml` and prints what's currently applied:

```
$ riced status
Applied:    my-dark
Repository: my-themes
At:         2026-05-24 19:50:33 UTC
Backup:     /home/me/.riced/state/backup/2026-05-24T19-50-33Z
Files:      11
  /home/me/.local/share/color-schemes/my-dark.colors
  /home/me/.local/share/konsole/my-dark.colorscheme
  ...
```

If no state file exists: `No theme is currently applied by Riced.`

## `riced doctor`

Audits the live state and reports inconsistencies:

```
$ riced doctor
Riced health check:

  [ok] KDE Plasma 6 tools available (plasma-apply-*, kwriteconfig6, qdbus)
  [ok] plasmashell responds on DBus
  [ok] no stale apply lock
  [ok] theme "my-dark" applied at 2026-05-24T19:50:33Z
  [ok] all 12 declared files exist
  [ok] backup root present at /home/me/.riced/state/backup/2026-05-24T19-50-33Z
  [ok] 1 backup(s), none older than 90 days

All clear.
```

Each line is one check. `issue` lines bump the exit code to 1; `warn` and `info` do not. Stale lock, missing declared files, corrupt state file, missing backup root all surface as issues. Backups older than 90 days surface as info hints (run `riced clean-backups`).

## `riced revert`

```bash
riced revert [--yes]
```

Reverses the last apply:

1. Removes every file listed in `current.toml.written_at` (silently skips entries that no longer exist).
2. Walks `current.toml.backup_root` and restores each backed-up file to its original location (computed by mirroring the path under the backup root back to `/`).
3. Deletes `current.toml`.

Same confirmation contract as `apply`: prompts interactively, `--yes` skips. Same lock as `apply`.

`revert` only restores the **most recent** apply. The full chain of backups is preserved under `~/.riced/state/backup/<timestamp>/` for forensic purposes, but Riced doesn't yet offer "revert two applies back".

## State file layout

```
~/.riced/
├── cache/                       # `riced generate` / `apply` output
│   └── <slug>/...
└── state/
    ├── .lock                    # ephemeral, created during apply/revert/clean-backups
    ├── current.toml             # what's applied right now
    └── backup/
        ├── 2026-05-24T19-50-33Z/
        │   └── home/me/.config/gtk-4.0/gtk.css   # original, pre-apply
        └── 2026-05-25T08-12-04Z/...
```

`current.toml` schema:

```toml
schema_version = 1
slug           = "my-dark"
repo           = "my-themes"
applied_at     = 2026-05-24T19:50:33Z
backup_root    = "/home/me/.riced/state/backup/2026-05-24T19-50-33Z"
written_at = [
  "/home/me/.local/share/color-schemes/my-dark.colors",
  "/home/me/.config/gtk-4.0/gtk.css",
  …
]
```

## Testing apply

The package is fully testable without ever touching a real KDE session:

```go
env := setupFakeApply(t)
plan, _ := apply.Build(env.Manifest, env.BuildDir,
    apply.Targets{HomeDir: env.Home},
    apply.StatePath(env.Home), apply.BackupRoot(env.Home, env.Now), nil)
_ = apply.Execute(plan, apply.ExecOptions{KDE: env.KDE, Now: …})
// env.KDE.Calls now contains ["ApplyColorScheme:my-test", "ReconfigureKWin", …]
// env.Home contains the live file destinations under a temp tree
```

See `internal/apply/apply_test.go` for the full pattern. The current test suite exercises: clean install, backup of pre-existing user files, revert restoring originals, missing-state revert error, plan-format human readability, partial-failure state persistence, XDG env var routing, sticky BackupRoot (no backup recorded on clean install), skip-managed (no re-backup of Riced's own output), and the per-screen slideshow rule cascade.
