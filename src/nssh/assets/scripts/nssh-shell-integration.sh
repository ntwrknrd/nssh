#!/bin/bash
# nssh shell integration for bash/zsh
# Source this file in your ~/.bashrc or ~/.zshrc for better history integration
#
# For bash: Add to ~/.bashrc:
#   source /path/to/nssh/nssh-shell-integration.sh
#
# For zsh: Add to ~/.zshrc:
#   source /path/to/nssh/nssh-shell-integration.sh

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
        command "$nssh_cmd" help
        return 1
    fi

    # Allow Typer/click completion helpers to reach the CLI without custom routing
    if [ -n "${_NSSH_COMPLETE:-}" ] || [ -n "${_TYPER_COMPLETE:-}" ]; then
        command "$nssh_cmd" "$@"
        return $?
    fi

    local -a subcommands=(host cred log benchmark bootstrap recording-check help version __list-subcommands)
    local detected
    if detected=$(command "$nssh_cmd" __list-subcommands 2>/dev/null); then
        read -r -a subcommands <<<"$detected"
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

    echo "→ $actual_cmd" >&2

    # Start atuin tracking if available
    local atuin_id=""
    if command -v atuin >/dev/null 2>&1; then
        atuin_id=$(atuin history start $actual_cmd)
    fi

    # Use 'command' to bypass this function and invoke the installed CLI
    command "$nssh_cmd" "${original_args[@]}"
    local exit_code=$?

    # End atuin tracking if it was started
    if [ -n "$atuin_id" ]; then
        atuin history end --exit $exit_code $atuin_id
    fi

    # Add actual command to shell history
    if [ -n "$BASH_VERSION" ]; then
        history -s "$actual_cmd"
    elif [ -n "$ZSH_VERSION" ]; then
        print -s "$actual_cmd"
    fi

    return $exit_code
}
