# bash completion for riced.
# Install with:
#   riced completion bash > ~/.local/share/bash-completion/completions/riced
# (or system-wide:  sudo riced completion bash > /usr/share/bash-completion/completions/riced)
# Open a new bash session for completions to take effect.

_riced() {
    local cur prev words cword
    _init_completion -n : || return

    local top_subs="themes list validate generate repo apply status revert clean-backups doctor new completion version help"

    # Find the first non-flag positional after `riced` — that's the subcommand.
    local sub=""
    local subi=0
    local i
    for (( i = 1; i < cword; i++ )); do
        if [[ "${words[i]}" == -* ]]; then continue; fi
        sub="${words[i]}"
        subi=$i
        break
    done

    # No subcommand yet → suggest top-level subcommands or global flags.
    if [[ -z "$sub" ]]; then
        if [[ "$cur" == -* ]]; then
            COMPREPLY=( $(compgen -W "--log-level --log-format -v --verbose" -- "$cur") )
        else
            COMPREPLY=( $(compgen -W "$top_subs" -- "$cur") )
        fi
        return
    fi

    case "$sub" in
        apply)
            if [[ "$cur" == -* ]]; then
                COMPREPLY=( $(compgen -W "--dry-run --yes --out" -- "$cur") )
            else
                local slugs
                slugs=$(riced themes --slugs 2>/dev/null)
                COMPREPLY=( $(compgen -W "$slugs" -- "$cur") )
            fi
            ;;
        validate)
            if [[ "$cur" == -* ]]; then
                COMPREPLY=( $(compgen -W "--hints" -- "$cur") )
            else
                COMPREPLY=( $(compgen -d -- "$cur") )
            fi
            ;;
        generate)
            COMPREPLY=( $(compgen -d -- "$cur") )
            ;;
        themes)
            if [[ "$cur" == -* ]]; then
                COMPREPLY=( $(compgen -W "--repo --theme --slugs" -- "$cur") )
            elif [[ "$prev" == "--repo" ]]; then
                COMPREPLY=( $(compgen -W "$(riced repo list --names 2>/dev/null)" -- "$cur") )
            fi
            ;;
        repo)
            # Look for the repo sub-subcommand.
            local rsub="${words[subi+1]}"
            if [[ "$cword" == "$((subi+1))" ]]; then
                COMPREPLY=( $(compgen -W "init add list remove" -- "$cur") )
                return
            fi
            case "$rsub" in
                init)
                    if [[ "$cur" == -* ]]; then
                        COMPREPLY=( $(compgen -W "--register" -- "$cur") )
                    else
                        COMPREPLY=( $(compgen -d -- "$cur") )
                    fi
                    ;;
                add)
                    COMPREPLY=( $(compgen -d -- "$cur") )
                    ;;
                list|ls)
                    COMPREPLY=( $(compgen -W "--names" -- "$cur") )
                    ;;
                remove|rm)
                    COMPREPLY=( $(compgen -W "$(riced repo list --names 2>/dev/null)" -- "$cur") )
                    ;;
            esac
            ;;
        new)
            local nsub="${words[subi+1]}"
            if [[ "$cword" == "$((subi+1))" ]]; then
                COMPREPLY=( $(compgen -W "theme" -- "$cur") )
                return
            fi
            case "$nsub" in
                theme)
                    if [[ "$cur" == -* ]]; then
                        COMPREPLY=( $(compgen -W "--from --repo --mode" -- "$cur") )
                    elif [[ "$prev" == "--from" ]]; then
                        COMPREPLY=( $(compgen -W "$(riced themes --slugs 2>/dev/null)" -- "$cur") )
                    elif [[ "$prev" == "--repo" ]]; then
                        COMPREPLY=( $(compgen -W "$(riced repo list --names 2>/dev/null)" -- "$cur") )
                    elif [[ "$prev" == "--mode" ]]; then
                        COMPREPLY=( $(compgen -W "dark light" -- "$cur") )
                    fi
                    ;;
            esac
            ;;
        completion)
            COMPREPLY=( $(compgen -W "fish bash" -- "$cur") )
            ;;
        clean-backups)
            COMPREPLY=( $(compgen -W "--keep --older-than" -- "$cur") )
            ;;
        revert)
            COMPREPLY=( $(compgen -W "--yes" -- "$cur") )
            ;;
    esac
}
complete -F _riced riced
