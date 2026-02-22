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

    if test (count $argv) -eq 0
        command $nssh_cmd --help
        return 0
    end

    # Detect top-level subcommands (fall back to defaults if lookup fails)
    set -l subcommands host ctx log cp benchmark self help version completion
    set -l detected (command $nssh_cmd __list-subcommands 2>/dev/null)
    if test $status -eq 0 -a (count $detected) -gt 0
        set subcommands $detected
    end

    set -l first $argv[1]

    if contains -- $first $subcommands
        command $nssh_cmd $argv
        return $status
    end

    # Bare connect path with history/atuin tracking
    set -l actual_cmd "nssh $argv"

    # Show command if verbose flag or NSSH_VERBOSE is set
    if contains -- -v $argv; or contains -- --verbose $argv; or test -n "$NSSH_VERBOSE"
        echo "-> $actual_cmd" >&2
    end

    set -l atuin_id ""
    if type -q atuin
        set atuin_id (atuin history start "$actual_cmd")
    end

    # Set terminal/pane title to target hostname
    printf '\e]2;%s\a' $first

    # Promote disposable tmux sessions to persistent (named = survives detach)
    if set -q TMUX; and string match -qr '^temp-[0-9]+$' (tmux display-message -p '#{session_name}')
        set -l name $first
        if tmux has-session -t $name 2>/dev/null
            set -l i 2
            while tmux has-session -t "$name-$i" 2>/dev/null
                set i (math $i + 1)
            end
            set name "$name-$i"
        end
        tmux rename-session $name
        tmux select-pane -T $name
        tmux set destroy-unattached off
    end

    command $nssh_cmd $argv
    set -l exit_code $status

    # Reset title to local hostname
    printf '\e]2;%s\a' (hostname -s)

    if test -n "$atuin_id"
        atuin history end --exit $exit_code $atuin_id
    end

    printf "- cmd: %s\n  when: %s\n" "$actual_cmd" (date +%s) >> $__fish_user_data_dir/fish_history
    builtin history --merge

    return $exit_code
end
