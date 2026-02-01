# bash completion for global-logrotate

_global_logrotate() {
    local cur prev opts
    COMPREPLY=()
    cur="${COMP_WORDS[COMP_CWORD]}"
    prev="${COMP_WORDS[COMP_CWORD-1]}"

    # All available options
    opts="-H -D -n -h -p -o --pattern --parallel --encrypt --read --pass-gen --pass-reset --version --exclude-from --log-file --log-level"

    # Handle options that require specific value completions
    case "${prev}" in
        --pattern)
            # Common log patterns
            COMPREPLY=( $(compgen -W "*.log *.txt *.out *.err *.log.* access.log error.log" -- "${cur}") )
            return 0
            ;;
        --parallel)
            # Number of parallel jobs
            COMPREPLY=( $(compgen -W "1 2 4 8 16 32" -- "${cur}") )
            return 0
            ;;
        --log-level)
            # Log level completion
            COMPREPLY=( $(compgen -W "error info debug" -- "${cur}") )
            return 0
            ;;
    esac

    # Complete flags only when input starts with -
    if [[ "${cur}" == -* ]]; then
        COMPREPLY=( $(compgen -W "${opts}" -- "${cur}") )
        return 0
    fi
}

# -o default: fall back to default (path) completion when no matches
# -o bashdefault: use bash default completions
complete -o default -o bashdefault -F _global_logrotate global-logrotate
