# Security Policy

## Supported versions

Riced is pre-1.0; only the latest `0.1.x` release receives security fixes.
Older patch releases are not back-ported.

| Version | Supported |
|---|---|
| latest `0.1.x` | ✅ |
| anything earlier | ❌ |

## Reporting a vulnerability

Use **GitHub's private vulnerability reporting** on this repo:
<https://github.com/Altagen/Riced/security/advisories/new>.

Do **not** open a public issue, PR, or Discussion for a suspected vulnerability.

You should get a first reply within 7 days. If the issue is confirmed, a fix
lands on `develop` and ships in the next `0.1.x` patch release; an advisory
with credit is published when the fix is tagged.

## Scope

The areas where a real vulnerability is plausible:

- **Manifest parsing & validation** (`internal/manifest/`) — anything that
  lets a `theme.toml` or `repository.toml` escape its sandbox (path
  traversal, symlink escape, arbitrary-write, command injection through
  rendered output).
- **External assets** (`internal/apply/external.go`, `[[looks]]`) — bypass
  of the sha256 verification, archive extraction issues (zip-slip,
  symlink traversal in tar/zip), or unsafe `script` strategy invocation.
- **KDE adapter** (`internal/apply/kde.go`) — command injection through
  any of the values Riced forwards to `kwriteconfig6`, `kpackagetool6`,
  `qdbus6` / `qdbus`, or the JS evaluated by `plasmashell`.
- **State & backup handling** (`internal/apply/`) — paths under
  `~/.riced/`, `~/.config/`, `~/.local/share/` written or removed
  outside the documented set.

Out of scope: bugs that require root, vulnerabilities in upstream KDE
helpers themselves, or theft via a theme the user explicitly chose to
install (the install is an intentional code-execution surface — that's
why `script` requires explicit opt-in and `looks.url` requires sha256).
