# Troubleshooting

Concrete failure modes Riced has hit in the field, and how to get around
them. Symptom -> diagnosis -> fix. Each entry comes from a real observed
failure so the recipe is grounded, not theoretical.

---

## `[[looks]]` script install fails with `sudo: a terminal is required` and `cmake: command not found`

**Symptom:** `riced apply` aborts mid-install with a wall of
`sudo: a terminal is required to read the password` (the exact phrasing
varies by locale) followed by the install script's own error
(`cmake: command not found`, `make: command not found`, etc.) and
finally:

```
level=ERROR msg=execute err="look \"<name>\": script failed: exit status 1"
```

**Diagnosis:** the package is a C++ KWin component (the canonical case is
the `Darkly` decoration, fork of Lightly, but `Lightly` itself, most
`SierraBreezeEnhanced`-family decorations, `Kvantum` engine builds, and
several `KWin/Decoration` projects share the pattern). Its `install.sh`
relies on:

1. `cmake` + `make` being installed (build deps);
2. `sudo make install` to drop the `.so` into a system path KWin scans
   (typically `/usr/lib/qt6/plugins/org.kde.kdecoration2/`).

Riced's `[[looks]]` install strategy = `"script"` runs `bash <script>`
inside the extracted source tree with the user's own privileges. There
is no TTY (Riced is not interactive at that point), so `sudo` cannot
prompt; even if it could, Riced does not own a system path.

The download + sha256 verify + extract + normalize steps **all worked
correctly** -- the failure is strictly inside the package's own
`install.sh`. State is not corrupted (the [[looks]] phase runs before
any KDE side effect; Riced aborts cleanly).

**Why Riced does not "just" support it**

Compiling a C++ Plasma plugin from a manifest is well outside Riced's
scope today:

- Different distros need different cmake flags (`-DCMAKE_INSTALL_LIBDIR`)
- Build dependencies (Qt6 dev headers, kdecoration2 dev) live in
  per-distro packages we cannot guess
- A user-side install would need a custom `CMAKE_INSTALL_PREFIX` plus
  KWin plugin scan path manipulation (`QT_PLUGIN_PATH`, etc.) -- in
  practice the plugin won't load if it isn't where KWin looks

**Fix: use the supported workflow**

1. Install the package via your distro's package manager (Arch:
   `paru -S darkly` or `paru -S darkly-bin`; Fedora / openSUSE have
   equivalents).
2. Discover the KDecoration2 library id the package exposes
   (`pkgfile -l darkly 2>/dev/null | grep org.kde.kdecoration2 | head`
   or just inspect `/usr/lib/qt6/plugins/org.kde.kdecoration2/`).
3. Reference it via the `library:` escape hatch in your theme:

   ```toml
   [window]
   decoration = "library:org.kde.darkly"   # whatever the .so reports
   ```

`library:` was added precisely for this case (commit `855b716`).
Riced still owns the apply pipeline (palette, wallpapers, animations,
KWin reconfigure) -- it just doesn't try to compile anything.

**Future improvement (tracked)**

Riced could detect known cmake-only projects in `[[looks]]` and refuse
them up front with a "use your package manager + library:" message
instead of running the script and surfacing sudo errors. See the
0.1.4 roadmap on the README.

---

## Icons swap, but already-open apps still show the old theme

**Symptom:** you change `[icons].theme`, run `riced apply`, the panel /
launcher / Dolphin shows the new theme, but the windows you had open
before the apply (Konsole, Firefox, your editor) still show the old
icons -- buttons in their UI, tab icons, etc.

**Diagnosis:** icon themes are cached per-process. KDE rebuilds the
service cache after the change (Riced emits `kbuildsycoca6
--noincremental` for you since 0.1.3) and Plasma itself reloads, but
already-running Qt apps **only re-scan icons when they receive the
`KGlobalSettings::IconChanged` signal AND their internal cache misses**.
Many apps either ignore the signal or cache aggressively (Firefox is
notorious).

**Fix:** close + reopen the affected apps. The new theme is correctly
on disk -- only the in-memory cache of those long-lived processes is
stale. There is no Plasma 6 API for "force every app to drop its icon
cache".

---

## Launcher icon file was edited but Plasma still shows the old image

**Symptom:** you edited the PNG / SVG that `[panel].launcher_icon` points
at (same filename, new contents), re-ran `riced apply`, and the kickoff /
kicker widget keeps displaying the previous artwork.

**Diagnosis:** `riced apply` re-emits the `set-launcher-icon` JS, which
writes the same config key with the same path and calls
`reloadConfig()`. Plasma re-reads the key, but the icon pixmap itself is
cached by `QIcon` keyed on `(path, size)`. The path is identical, so Qt
serves the cached bitmap instead of re-reading the file.

The path-stable cache is independent of the icon-theme cache
(`kbuildsycoca6`) -- that one rebuilds the registry of installed
themes, not in-process pixmap caches.

**Fix:** force Plasma to drop the bitmap. Three options, lightest first:

1. Remove Plasma's icon cache file and trigger a panel redraw:

   ```bash
   rm -f ~/.cache/icon-cache.kcache
   ```

   The next time the launcher widget paints, it re-reads the file.

2. Restart plasmashell only (panel + desktop reload; open windows stay):

   ```bash
   kquitapp6 plasmashell && kstart plasmashell
   ```

3. Log out + log in for a fully clean state.

A workaround that side-steps the cache: bump the file name on every
edit (`s4-blue-neon-v2.png` instead of overwriting `s4-blue-neon.png`)
and update `launcher_icon` accordingly. The path changes, Qt cannot
hit its cache, the new bitmap loads on the next `riced apply`.

---

## Aurorae decoration applied but window buttons unchanged

**Symptom:** you set `[window].decoration = "aurorae:<theme>"`, apply,
and `kreadconfig6 --file kwinrc --group org.kde.kdecoration2 --key
library` returns `org.kde.kwin.aurorae` -- yet your titlebar still
looks identical to what it was (Klassy or Breeze).

**Diagnosis:** KWin redraws a window's decoration only on creation /
reload. The `qdbus6 org.kde.KWin /KWin reconfigure` call Riced emits
asks KWin to re-read its config, but already-decorated windows are not
forced to swap in the new aurorae theme until they restart or the
compositor is restarted.

**Fix:** open a new window (the new decoration shows up there
immediately) or re-trigger it explicitly:

```bash
qdbus6 org.kde.KWin /KWin reconfigure
```

(Riced runs this automatically at the end of every apply -- but if you
edited kwinrc by hand outside Riced, run it yourself.) Worst case, log
out + back in.

---

## `riced apply` aborts before any write: missing KDE Plasma 6 tools

**Symptom:** `riced apply` exits with
`level=ERROR msg=prerequisites err="missing KDE Plasma 6 tools: ..."`
without touching anything.

**Diagnosis:** the prerequisite check before the lock step lists every
binary Riced will need (`plasma-apply-*`, `kwriteconfig6`,
`kbuildsycoca6`, `kpackagetool6`, `qdbus6` or `qdbus`, `tar`, `unzip`).
Missing any of them stops the pipeline up front -- by design, to avoid
half-applied state.

**Fix:** install the missing tools. On Arch / CachyOS the metapackage
`plasma-workspace` carries the `plasma-apply-*` family;
`kf6-kconfig` carries `kwriteconfig6`. For the archive tools, `tar`
and `unzip` are usually already there.

---

## `riced apply` says "plasmashell isn't running"

**Symptom:**

```
level=WARN msg="plasmashell probe failed; launcher icon may not update"
err="plasmashell didn't respond within 5s"
```

The rest of the apply still runs (kdeglobals, GTK, Konsole, palette)
but the launcher icon swap is skipped.

**Diagnosis:** Riced's launcher icon swap uses Plasma JS sent over DBus
to `org.kde.plasmashell`. If the shell crashed or is restarting, the
probe times out at 5s and Riced logs a warning rather than aborting --
the rest of your theme still applies usefully.

**Fix:** start / restart plasmashell:

```bash
kstart plasmashell
```

Then re-run `riced apply` to pick up the launcher icon swap.

---

## State file got out of sync after a crash mid-apply

**Symptom:** `riced status` reports a slug as applied but the live files
don't match (some are from theme A, some from theme B), or `riced
revert` complains about missing backups.

**Diagnosis:** Riced uses `defer`-protected state writes -- even on
failure mid-apply the state file lists every path successfully written
*so far*. So state is normally consistent. A genuine corruption case
would only happen if the process is `kill -9`-ed between the file
copy and the state write, or if `~/.riced/state/` itself was tampered
with externally.

**Fix:**

1. Run `riced doctor` -- it reads the state file and the live tree and
   prints every divergence.
2. If you trust the state file, `riced revert` will walk
   `state.WrittenAt` and remove / restore as appropriate.
3. If the state file is itself corrupt, remove
   `~/.riced/state/current.toml` and re-apply the desired theme. You
   lose the "what was the previous theme" pointer but nothing is
   destroyed -- the backup root still holds the originals.
