function nssh --description "SSH to network equipment with password management"
    # Resolve installed CLI (prefer PATH, fall back to common locations)
    set -l nssh_cmd (type -P nssh 2>/dev/null)
    if test -z "$nssh_cmd"
        if test -x "$HOME/.local/bin/nssh"
            set nssh_cmd "$HOME/.local/bin/nssh"
        else if test -x "$HOME/bin/nssh"
            set nssh_cmd "$HOME/bin/nssh"
        else
            echo "Error: nssh binary not found in PATH or \$HOME/.local/bin" >&2
            return 1
        end
    end

    # Completion requests from Typer should bypass custom routing entirely
    # Typer's Fish completion uses _TYPER_COMPLETE_FISH_ACTION
    if set -q _NSSH_COMPLETE; or set -q _TYPER_COMPLETE; or set -q _TYPER_COMPLETE_FISH_ACTION
        command $nssh_cmd $argv
        return $status
    end

    if test (count $argv) -eq 0
        command $nssh_cmd help
        return 1
    end

    # Detect top-level subcommands (fall back to defaults if lookup fails)
    set -l subcommands host cred log benchmark bootstrap recording-check help version __list-subcommands
    set -l detected (command $nssh_cmd __list-subcommands 2>/dev/null)
    if test $status -eq 0 -a (count $detected) -gt 0
        set subcommands $detected
    end

    set -l first $argv[1]

    if set -q NSSH_TRACE_SUBCOMMANDS
        echo "[nssh fish] args=$argv first=$first" >&2
        for cmd in $subcommands
            echo "[nssh fish] subcommand=$cmd" >&2
        end
    end

    if contains -- $first $subcommands
        command $nssh_cmd $argv
        return $status
    end

    # Bare connect path with history/atuin tracking
    set -l actual_cmd "nssh $argv"
    echo "→ $actual_cmd" >&2

    set -l atuin_id ""
    if type -q atuin
        set atuin_id (atuin history start $actual_cmd)
    end

    command $nssh_cmd $argv
    set -l exit_code $status

    if test -n "$atuin_id"
        atuin history end --exit $exit_code $atuin_id
    end

    printf "- cmd: %s\n  when: %s\n" "$actual_cmd" (date +%s) >> $__fish_user_data_dir/fish_history
    builtin history --merge

    return $exit_code
end
