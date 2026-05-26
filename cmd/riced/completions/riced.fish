# fish completion for riced.
# Install with:
#   riced completion fish > ~/.config/fish/completions/riced.fish
# Open a new fish session for the completions to take effect.

# --- top-level subcommands --------------------------------------------------
complete -c riced -f -n __fish_use_subcommand -a themes        -d 'List themes across registered repositories'
complete -c riced -f -n __fish_use_subcommand -a list          -d 'List themes in a local directory'
complete -c riced -f -n __fish_use_subcommand -a validate      -d 'Validate a theme.toml (no rendering)'
complete -c riced -f -n __fish_use_subcommand -a generate      -d 'Render a theme to ./build/<slug>/'
complete -c riced -f -n __fish_use_subcommand -a repo          -d 'Manage registered repositories'
complete -c riced -f -n __fish_use_subcommand -a apply         -d 'Render + apply to the live KDE session'
complete -c riced -f -n __fish_use_subcommand -a status        -d 'Show the currently-applied theme'
complete -c riced -f -n __fish_use_subcommand -a revert        -d 'Undo the last apply'
complete -c riced -f -n __fish_use_subcommand -a clean-backups -d 'Garbage-collect backup snapshots'
complete -c riced -f -n __fish_use_subcommand -a doctor        -d 'Audit the live state'
complete -c riced -f -n __fish_use_subcommand -a new           -d 'Scaffold a new theme'
complete -c riced -f -n __fish_use_subcommand -a completion    -d 'Print shell completion script'
complete -c riced -f -n __fish_use_subcommand -a version       -d 'Print version'
complete -c riced -f -n __fish_use_subcommand -a help          -d 'Show help'

# --- riced repo <sub> -------------------------------------------------------
complete -c riced -f -n '__fish_seen_subcommand_from repo; and not __fish_seen_subcommand_from init add list remove ls rm' \
    -a 'init add list remove'

# --- riced new <sub> --------------------------------------------------------
complete -c riced -f -n '__fish_seen_subcommand_from new; and not __fish_seen_subcommand_from theme' \
    -a theme

# --- riced completion <shell> -----------------------------------------------
complete -c riced -f -n '__fish_seen_subcommand_from completion' -a 'fish bash'

# --- dynamic data -----------------------------------------------------------
# Slug completion after `riced apply`. Calls riced itself; the --slugs flag
# returns one fully qualified slug per line, no header.
complete -c riced -f -n '__fish_seen_subcommand_from apply' \
    -a '(riced themes --slugs 2>/dev/null)'

# Repo-name completion after `riced repo remove` / `riced repo rm`.
complete -c riced -f -n '__fish_seen_subcommand_from repo; and __fish_seen_subcommand_from remove rm' \
    -a '(riced repo list --names 2>/dev/null)'

# --- global flags -----------------------------------------------------------
complete -c riced -l log-level  -x -a 'debug info warn error' -d 'Log level'
complete -c riced -l log-format -x -a 'text json'             -d 'Log format'
complete -c riced -s v -l verbose                             -d 'Shortcut for --log-level=debug'

# --- per-subcommand flags ---------------------------------------------------
# riced apply
complete -c riced -n '__fish_seen_subcommand_from apply' -l dry-run -d 'Print plan, do not apply'
complete -c riced -n '__fish_seen_subcommand_from apply' -l yes     -d 'Skip the confirmation prompt'
complete -c riced -n '__fish_seen_subcommand_from apply' -l out -r  -d 'Cache / build output dir'

# riced validate
complete -c riced -n '__fish_seen_subcommand_from validate' -l hints -d 'Print reminder about taplo for formatting'

# riced themes
complete -c riced -n '__fish_seen_subcommand_from themes' -l repo   -x -a '(riced repo list --names 2>/dev/null)' -d 'Filter by repository'
complete -c riced -n '__fish_seen_subcommand_from themes' -l theme  -x -d 'Filter by Meta.Theme group'
complete -c riced -n '__fish_seen_subcommand_from themes' -l slugs     -d 'Plain slug list (for completion)'

# riced repo list
complete -c riced -n '__fish_seen_subcommand_from repo; and __fish_seen_subcommand_from list ls' \
    -l names -d 'Plain name list (for completion)'

# riced repo init
complete -c riced -n '__fish_seen_subcommand_from repo; and __fish_seen_subcommand_from init' \
    -l register -d 'Also add to ~/.config/riced/repositories.toml'

# riced new theme
complete -c riced -n '__fish_seen_subcommand_from new; and __fish_seen_subcommand_from theme' \
    -l from -x -a '(riced themes --slugs 2>/dev/null)' -d 'Parent theme slug to inherit from'
complete -c riced -n '__fish_seen_subcommand_from new; and __fish_seen_subcommand_from theme' \
    -l repo -x -a '(riced repo list --names 2>/dev/null)' -d 'Target repository'
complete -c riced -n '__fish_seen_subcommand_from new; and __fish_seen_subcommand_from theme' \
    -l mode -x -a 'dark light' -d 'Color mode'

# riced clean-backups
complete -c riced -n '__fish_seen_subcommand_from clean-backups' -l keep       -x -d 'Keep N most recent'
complete -c riced -n '__fish_seen_subcommand_from clean-backups' -l older-than -x -d 'Drop backups older than duration'

# riced revert
complete -c riced -n '__fish_seen_subcommand_from revert' -l yes -d 'Skip confirmation'
