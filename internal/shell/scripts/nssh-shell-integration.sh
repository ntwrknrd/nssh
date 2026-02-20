#!/bin/bash
# nssh shell integration for bash/zsh
# Source this file in your ~/.bashrc or ~/.zshrc for better history integration
#
# For bash: Add to ~/.bashrc:
#   source ~/.local/share/nssh/nssh-shell-integration.sh
#
# For zsh: Add to ~/.zshrc:
#   source ~/.local/share/nssh/nssh-shell-integration.sh

# Bash/Zsh integration function
nssh() {
    local -a original_args=("$@")

    # Resolve the installed nssh CLI (prefer PATH, fall back to common locations)
    local nssh_cmd
    if ! nssh_cmd="$(type -P nssh 2>/dev/null)"; then
        if [ -x "$HOME/.local/bin/nssh" ]; then
            nssh_cmd="$HOME/.local/bin/nssh"
        elif [ -x "$HOME/bin/nssh" ]; then
            nssh_cmd="$HOME/bin/nssh"
        else
            echo "Error: nssh binary not found on PATH or in \$HOME/.local/bin" >&2
            return 1
        fi
    fi

    if [ $# -eq 0 ]; then
        command "$nssh_cmd" --help
        return 0
    fi

    # Detect top-level subcommands (fall back to defaults if lookup fails)
    local -a subcommands=(host ctx log cp benchmark self help version completion)
    if [ -n "$BASH_VERSION" ]; then
        mapfile -t subcommands < <(command "$nssh_cmd" __list-subcommands 2>/dev/null) || true
    elif [ -n "$ZSH_VERSION" ]; then
        subcommands=("${(@f)$(command "$nssh_cmd" __list-subcommands 2>/dev/null)}") || true
    fi

    local first_token="${original_args[0]}"
    for cmd in "${subcommands[@]}"; do
        if [ "$first_token" = "$cmd" ]; then
            command "$nssh_cmd" "${original_args[@]}"
            return $?
        fi
    done

    # Bare connect path: build history entry and arrow output
    local actual_cmd="nssh"
    for arg in "${original_args[@]}"; do
        actual_cmd+=" $(printf '%q' "$arg")"
    done

    # Show command if verbose flag or NSSH_VERBOSE is set
    local verbose=false
    for arg in "${original_args[@]}"; do
        if [ "$arg" = "-v" ] || [ "$arg" = "--verbose" ]; then
            verbose=true
            break
        fi
    done
    if [ "$verbose" = true ] || [ -n "$NSSH_VERBOSE" ]; then
        echo "-> $actual_cmd" >&2
    fi

    # Start atuin tracking if available
    local atuin_id=""
    if command -v atuin >/dev/null 2>&1; then
        atuin_id=$(atuin history start "$actual_cmd")
    fi

    # Set terminal/pane title to target hostname
    printf '\033]2;%s\a' "$first_token"

    # Promote disposable tmux sessions to persistent (named = survives detach)
    if [ -n "$TMUX" ]; then
        local session_name
        session_name=$(tmux display-message -p '#{session_name}' 2>/dev/null)
        if [[ "$session_name" =~ ^[0-9]+$ ]]; then
            local name="$first_token"
            if tmux has-session -t "$name" 2>/dev/null; then
                local i=2
                while tmux has-session -t "$name-$i" 2>/dev/null; do
                    i=$((i + 1))
                done
                name="$name-$i"
            fi
            tmux rename-session "$name"
            tmux select-pane -T "$name"
            tmux set destroy-unattached off
        fi
    fi

    # Use 'command' to bypass this function and invoke the installed CLI
    command "$nssh_cmd" "${original_args[@]}"
    local exit_code=$?

    # Reset title to local hostname
    printf '\033]2;%s\a' "$(hostname -s)"

    # End atuin tracking if it was started
    if [ -n "$atuin_id" ]; then
        atuin history end --exit "$exit_code" "$atuin_id"
    fi

    # Add actual command to shell history
    if [ -n "$BASH_VERSION" ]; then
        history -s "$actual_cmd"
    elif [ -n "$ZSH_VERSION" ]; then
        print -s "$actual_cmd"
    fi

    return $exit_code
}
