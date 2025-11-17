#!/usr/bin/env bash
# nssh - SSH wrapper with age-encrypted password management
#
# This script orchestrates credential streaming and timing around the
# unified Python CLI (`nssh connect`). For better shell history integration,
# use the shell function wrappers instead.

# Resolve script directory for sourcing helpers and Python modules
SCRIPT_PATH="${BASH_SOURCE[0]}"
while [ -L "$SCRIPT_PATH" ]; do
    SCRIPT_DIR="$(cd "$(dirname "$SCRIPT_PATH")" && pwd)"
    SCRIPT_PATH="$(readlink "$SCRIPT_PATH")"
    [[ "$SCRIPT_PATH" != /* ]] && SCRIPT_PATH="$SCRIPT_DIR/$SCRIPT_PATH"
done
SCRIPT_DIR="$(cd "$(dirname "$SCRIPT_PATH")" && pwd)"

NSSH_DEBUG=${NSSH_DEBUG:-0}
REPO_PYTHONPATH="$(cd "${SCRIPT_DIR}/../../.." >/dev/null 2>&1 && pwd)"
EXTRA_PYTHONPATH=""
if [ -d "$REPO_PYTHONPATH/nssh" ]; then
    EXTRA_PYTHONPATH="$REPO_PYTHONPATH"
fi

CLI_SHIM="$SCRIPT_DIR/nssh-python-cli"
if [ ! -x "$CLI_SHIM" ] && [ -x "$HOME/.local/share/nssh/nssh-python-cli" ]; then
    CLI_SHIM="$HOME/.local/share/nssh/nssh-python-cli"
fi

if [ ! -x "$CLI_SHIM" ]; then
    echo "Error: missing nssh-python-cli shim" >&2
    exit 1
fi

run_cli_command() {
    if [ -n "$EXTRA_PYTHONPATH" ]; then
        PYTHONPATH="${EXTRA_PYTHONPATH}${PYTHONPATH:+:$PYTHONPATH}" "$CLI_SHIM" "$@"
    else
        "$CLI_SHIM" "$@"
    fi
}

exec_cli_command() {
    if [ -n "$EXTRA_PYTHONPATH" ]; then
        PYTHONPATH="${EXTRA_PYTHONPATH}${PYTHONPATH:+:$PYTHONPATH}" exec "$CLI_SHIM" "$@"
    else
        exec "$CLI_SHIM" "$@"
    fi
}

# Completion helpers (Click/Typer) invoke the CLI with special env vars.
# When those are present we bypass the wrapper logic and hand off to Typer
# directly so we don't accidentally trigger the connect flow.
if [ -n "${_NSSH_COMPLETE:-}" ] || [ -n "${_TYPER_COMPLETE:-}" ] || \
   [ -n "${_PYTHON__M_NSSH_CLI_MAIN_COMPLETE:-}" ] || [ -n "${_PYTHON__M_NSSH_MANAGER_CLI_MAIN_COMPLETE:-}" ]; then
    exec_cli_command "$@"
fi

PASS_PIPE_READ_FD=""
PASS_PIPE_WRITE_FD=""
ACTIVE_PASS_READ_FD=""
RECORDING_LOCK_DIR=""
PENDING_PASS_READ_FD=""

close_fd_if_set() {
    local fd_var="$1"
    local direction="$2"
    local fd_value

    fd_value="${!fd_var}"
    if [ -n "$fd_value" ]; then
        if [ "$direction" = "in" ]; then
            exec {fd_value}<&-
        else
            exec {fd_value}>&-
        fi
        printf -v "$fd_var" '%s' ""
    fi
}

cleanup_pass_pipes() {
    close_fd_if_set PASS_PIPE_READ_FD in
    close_fd_if_set PASS_PIPE_WRITE_FD out
    close_fd_if_set PENDING_PASS_READ_FD in
    close_fd_if_set ACTIVE_PASS_READ_FD in
    release_recording_lock
}

trap cleanup_pass_pipes EXIT

_recording_lock_info_file() {
    echo "$1/.lockinfo"
}

release_recording_lock() {
    if [ -n "$RECORDING_LOCK_DIR" ]; then
        local info_file
        info_file=$(_recording_lock_info_file "$RECORDING_LOCK_DIR")
        rm -f "$info_file" 2>/dev/null || true
        rmdir "$RECORDING_LOCK_DIR" 2>/dev/null || true
        RECORDING_LOCK_DIR=""
    fi
}

cleanup_stale_recording_lock() {
    local lock_dir="$1"
    local info_file owner_pid ps_cmd
    info_file=$(_recording_lock_info_file "$lock_dir")

    if [ -f "$info_file" ]; then
        owner_pid=$(awk -F'=' '/^pid=/ {print $2}' "$info_file" 2>/dev/null | tr -d ' ')
    else
        owner_pid=""
    fi

    if [ -n "$owner_pid" ] && kill -0 "$owner_pid" 2>/dev/null; then
        ps_cmd=$(ps -p "$owner_pid" -o command= 2>/dev/null | tr -d '\n')
        if [ -n "$ps_cmd" ] && [[ "$ps_cmd" == *"nssh"* || "$ps_cmd" == *"asciinema"* ]]; then
            return 1
        fi
    fi

    # Either no owner info, the process is gone, or a different program holds the PID
    rm -rf "$lock_dir" 2>/dev/null || true
    return 0
}

acquire_recording_lock() {
    local lock_dir="$1"
    if [ -z "$lock_dir" ]; then
        return 0
    fi

    local info_file
    info_file=$(_recording_lock_info_file "$lock_dir")

    while true; do
        if mkdir "$lock_dir" 2>/dev/null; then
            {
                echo "pid=$$"
                echo "cmd=$0"
                echo "since=$(date -u +%s)"
            } >"$info_file" 2>/dev/null || true
            RECORDING_LOCK_DIR="$lock_dir"
            break
        fi

        if cleanup_stale_recording_lock "$lock_dir"; then
            continue
        fi

        sleep 0.1
    done
}

open_pass_pipe() {
    local fifo_path
    fifo_path=$(mktemp "${TMPDIR:-/tmp}/nssh-pass-fifo.XXXXXX") || return 1
    rm -f "$fifo_path"
    if ! mkfifo "$fifo_path"; then
        echo "Error: unable to create FIFO at $fifo_path" >&2
        return 1
    fi

    # Open fifo for read/write in a single descriptor to avoid blocking, then dup for writer
    eval "exec {PASS_PIPE_READ_FD}<>\"$fifo_path\""
    eval "exec {PASS_PIPE_WRITE_FD}>&\${PASS_PIPE_READ_FD}"
    rm -f "$fifo_path"
    return 0
}

prepare_pass_pipe_env() {
    if ! open_pass_pipe; then
        echo "Error: failed to initialize credential pipe" >&2
        finalize_and_exit 1
    fi
    export NSSH_PASS_FD="$PASS_PIPE_WRITE_FD"
    export NSSH_PASS_FMT="fifo-v1"
}

teardown_pass_pipe_after_connect() {
    unset NSSH_PASS_FD NSSH_PASS_FMT
    close_fd_if_set PASS_PIPE_WRITE_FD out
}

discard_pending_pass_fd() {
    close_fd_if_set PENDING_PASS_READ_FD in
}

activate_pending_pass_fd() {
    if [ -n "$PENDING_PASS_READ_FD" ]; then
        close_fd_if_set ACTIVE_PASS_READ_FD in
        ACTIVE_PASS_READ_FD="$PENDING_PASS_READ_FD"
        PENDING_PASS_READ_FD=""
    fi
}

# Detect SSH compatibility errors and offer to fix them
detect_and_fix_ssh_compatibility() {
    local target_host="$1"
    local ssh_stderr="$2"
    local ssh_status="$3"

    # Only check if SSH failed with exit code 255 (connection/protocol error)
    if [ "$ssh_status" -ne 255 ]; then
        return
    fi

    # Check for compatibility error patterns
    local needs_kex=0
    local needs_macs=0
    local needs_ciphers=0
    local needs_hostkey=0
    local compat_types=()

    if echo "$ssh_stderr" | grep -qi "no matching key exchange method found"; then
        needs_kex=1
        compat_types+=("kex")
    fi

    if echo "$ssh_stderr" | grep -qi "no matching MAC found"; then
        needs_macs=1
        compat_types+=("macs")
    fi

    if echo "$ssh_stderr" | grep -qi "no matching cipher found"; then
        needs_ciphers=1
        compat_types+=("ciphers")
    fi

    if echo "$ssh_stderr" | grep -qi "no matching host key type found"; then
        needs_hostkey=1
        compat_types+=("hostkey")
    fi

    # If compatibility issues detected, offer to fix
    if [ ${#compat_types[@]} -gt 0 ]; then
        echo "" >&2
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━" >&2
        echo "Legacy SSH compatibility issue detected!" >&2
        echo "" >&2
        echo "The remote server requires legacy algorithm support:" >&2
        for compat_type in "${compat_types[@]}"; do
            case "$compat_type" in
                kex)
                    echo "  • Key Exchange Algorithms (KexAlgorithms)" >&2
                    ;;
                macs)
                    echo "  • Message Authentication Codes (MACs)" >&2
                    ;;
                ciphers)
                    echo "  • Ciphers" >&2
                    ;;
                hostkey)
                    echo "  • Host Key Algorithms" >&2
                    ;;
            esac
        done
        echo "" >&2

        # Prompt user to apply fix
        echo -n "Apply compatibility fix for '$target_host'? [y/N] " >&2
        read -r response

        if [[ "$response" =~ ^[Yy]$ ]]; then
            echo "" >&2
            echo "Applying compatibility fixes..." >&2

            # Build nssh host update command
            local update_cmd=("nssh host" "update" "$target_host")
            for compat_type in "${compat_types[@]}"; do
                update_cmd+=("--compat" "$compat_type")
            done

            # Run the update command
            if "${update_cmd[@]}"; then
                echo "" >&2
                echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━" >&2
                echo "✓ Compatibility fix applied!" >&2
                echo "You can now retry the connection: nssh $target_host" >&2
                echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━" >&2
            else
                echo "" >&2
                echo "Failed to apply compatibility fix." >&2
                echo "You can manually update with: ${update_cmd[*]}" >&2
            fi
        else
            echo "" >&2
            echo "Skipped. You can manually apply the fix with:" >&2
            echo "  nssh host update $target_host ${compat_types[@]/#/--compat }" >&2
            echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━" >&2
        fi
    fi
}

# Timing helper lives beside the wrapper to keep this file small.
if [ "$NSSH_DEBUG" = "1" ]; then
    TIMER_SCRIPT=""
    # Check multiple locations for the timer helper
    if [ -f "$SCRIPT_DIR/nssh-timer.sh" ]; then
        TIMER_SCRIPT="$SCRIPT_DIR/nssh-timer.sh"
    elif [ -f "${HOME}/.local/share/nssh/nssh-timer.sh" ]; then
        TIMER_SCRIPT="${HOME}/.local/share/nssh/nssh-timer.sh"
    fi

    if [ -n "$TIMER_SCRIPT" ]; then
        # shellcheck disable=SC1090
        source "$TIMER_SCRIPT"
        nssh_timer_init
    else
        echo "Error: missing timing helper (checked $SCRIPT_DIR and ~/.local/share/nssh)" >&2
        exit 1
    fi
else
    TIMING_ENABLED=0
    timer_log() { :; }
    begin_run() {
        local _label="$1"
        local _detail="$2"
        local dest_var="${3:-}"
        if [ -n "$dest_var" ]; then
            printf -v "$dest_var" '%s' ""
        fi
    }
    end_run() {
        :
    }
    begin_stage() {
        local _stage="$1"
        local _detail="$2"
        local dest_var="${3:-}"
        if [ -n "$dest_var" ]; then
            printf -v "$dest_var" '%s' ""
        fi
    }
    end_stage() {
        :
    }
fi


forward_timing_lines() {
    # Re-emit TIMING lines to stderr so downstream tooling can consume them
    local line
    local filtered=""
    while IFS= read -r line; do
        if [[ "$line" == *"TIMING:"* ]]; then
            printf '%s\n' "$line" >&2
        else
            filtered+="${line}"$'\n'
        fi
    done <<<"$1"
    # Remove the trailing newline we added during accumulation
    printf '%s' "$filtered"
}

finalize_and_exit() {
    local code="$1"
    local detail="$2"
    if [ -z "$detail" ]; then
        detail="$RUN_DETAIL"
    fi
    if [ -n "$WRAPPER_START_STAGE" ]; then
        end_stage "wrapper-start" "$WRAPPER_START_STAGE" "$detail"
        WRAPPER_START_STAGE=""
    fi
    if [ -n "$WRAPPER_TEARDOWN_STAGE" ]; then
        end_stage "wrapper-teardown" "$WRAPPER_TEARDOWN_STAGE" "$detail"
        WRAPPER_TEARDOWN_STAGE=""
    fi
    if [ -n "$WRAPPER_RUN_START" ]; then
        end_run "$WRAPPER_RUN_LABEL" "$WRAPPER_RUN_START" "$detail" "$code"
        WRAPPER_RUN_START=""
    fi
    cleanup_pass_pipes
    exit "$code"
}

verbose_flag=""
username=""
literal_mode=0

if [ $# -eq 0 ]; then
    exec_cli_command --help
fi

args=("$@")
total_args=$#
arg_index=0
while [ $arg_index -lt $total_args ]; do
    arg="${args[$arg_index]}"
    case "$arg" in
        --)
            literal_mode=1
            arg_index=$((arg_index + 1))
            break
            ;;
        --help|-h)
            exec_cli_command --help
            ;;
        --version|-v)
            exec_cli_command --version
            ;;
        --verbose|-V)
            verbose_flag="1"
            ;;
        --user)
            arg_index=$((arg_index + 1))
            if [ $arg_index -ge $total_args ]; then
                echo "Error: --user requires a value" >&2
                exit 1
            fi
            username="${args[$arg_index]}"
            ;;
        -u)
            arg_index=$((arg_index + 1))
            if [ $arg_index -ge $total_args ]; then
                echo "Error: -u requires a value" >&2
                exit 1
            fi
            username="${args[$arg_index]}"
            ;;
        *)
            break
            ;;
    esac
    arg_index=$((arg_index + 1))
done

shift $arg_index

if [ $# -eq 0 ]; then
    exec_cli_command --help
fi

first_token="$1"
if [ "$literal_mode" -eq 0 ]; then
    case "$first_token" in
        connect|host|cred|log|benchmark|install-shell|recording-check|__list-subcommands)
            exec_cli_command "$@"
            ;;
        help)
            exec_cli_command --help
            ;;
        version)
            exec_cli_command --version
            ;;
    esac
fi

search_term="$1"
shift

# Extract user@ prefix if present when -u not provided
if [ -z "$username" ] && [[ "$search_term" =~ ^([^@]+)@(.+)$ ]]; then
    username="${BASH_REMATCH[1]}"
    search_term="${BASH_REMATCH[2]}"
fi

WRAPPER_RUN_LABEL="nssh-wrapper"
WRAPPER_RUN_START=""
RUN_DETAIL="$search_term"
WRAPPER_START_STAGE=""
WRAPPER_TEARDOWN_STAGE=""

begin_run "$WRAPPER_RUN_LABEL" "$RUN_DETAIL" WRAPPER_RUN_START
begin_stage "wrapper-start" "$RUN_DETAIL" WRAPPER_START_STAGE

run_connect_module() {
    local search_term="$1"
    local user_arg="$2"
    local cmd=(connect "$search_term")
    if [ -n "$user_arg" ]; then
        cmd+=("$user_arg")
    fi
    run_cli_command "${cmd[@]}"
}

run_recording_helper() {
    local subcommand="$1"
    shift
    run_cli_command log "$subcommand" "$@"
}

run_recording_check() {
    local hostname="$1"
    run_cli_command recording-check "$hostname"
}

check_age_key() {
    local age_key_path
    # Check explicit env var first
    if [ -n "${NSSH_AGE_KEY:-}" ]; then
        age_key_path="$NSSH_AGE_KEY"
    else
        # Default path
        age_key_path="${HOME}/.config/age/keys.txt"
    fi

    # Expand ~ if needed
    age_key_path="${age_key_path/#\~/$HOME}"

    if [ ! -f "$age_key_path" ]; then
        echo "Error: Age encryption key not found at: $age_key_path" >&2
        echo "" >&2
        echo "To generate a new age key, run:" >&2
        echo "  mkdir -p ~/.config/age" >&2
        echo "  age-keygen -o ~/.config/age/keys.txt" >&2
        echo "" >&2
        echo "For more information, see: https://github.com/FiloSottile/age" >&2
        finalize_and_exit 1
    fi
}

# Check age key exists before initializing credential pipes
check_age_key

# Unified host selection + credential resolution (single Python call)
prepare_pass_pipe_env
timer_log "START: connect module"
raw_output=$(run_connect_module "$search_term" "$username" 2>&1)
connect_status=$?
output=$(forward_timing_lines "$raw_output")
timer_log "END: connect module (status=$connect_status)"
teardown_pass_pipe_after_connect
PENDING_PASS_READ_FD="$PASS_PIPE_READ_FD"
PASS_PIPE_READ_FD=""

# Extract result from output (last line that matches hostname|filepath|... format)
# This filters out timing logs and other stderr output
result=$(echo "$output" | grep -v "TIMING:" | grep -v "MULTIPLE_MATCHES" | grep "|" | tail -n1)

# Check for multiple matches requiring fzf
if [ $connect_status -eq 2 ]; then
    # Multiple matches - need fzf selection
    # Get match list (filter out timing and MULTIPLE_MATCHES marker)
    matches=$(echo "$output" | grep -v "TIMING:" | grep -v "^MULTIPLE_MATCHES$" | grep "|")

    discard_pending_pass_fd

    if ! command -v fzf &> /dev/null; then
        echo "Error: Multiple matches found but 'fzf' not installed" >&2
        echo "$matches" >&2
        finalize_and_exit 1
    fi

    # Extract hostnames for fzf
    hostnames=$(echo "$matches" | cut -d'|' -f1)
    selected_host=$(echo "$hostnames" | fzf --height=40% --prompt="Select host: " --query="$search_term")

    if [ -z "$selected_host" ]; then
        echo "No host selected" >&2
        finalize_and_exit 1
    fi

    RUN_DETAIL="$selected_host"

    # Re-run connect with selected host
    prepare_pass_pipe_env
    timer_log "START: connect module (after fzf)"
    raw_output=$(run_connect_module "$selected_host" "$username" 2>&1)
    connect_status=$?
    output=$(forward_timing_lines "$raw_output")
    timer_log "END: connect module (status=$connect_status)"
    teardown_pass_pipe_after_connect
    PENDING_PASS_READ_FD="$PASS_PIPE_READ_FD"
    PASS_PIPE_READ_FD=""

    # Extract result from output
    result=$(echo "$output" | grep -v "TIMING:" | grep "|" | tail -n1)
fi

# Check for error
if [ $connect_status -ne 0 ]; then
    # Special handling for "no host found" - passthrough to vanilla SSH
    if [ $connect_status -eq 3 ]; then
        echo "Note: Host '$search_term' not in nssh config, using vanilla SSH" >&2
        discard_pending_pass_fd

        # Build vanilla SSH command with all original arguments
        vanilla_ssh_cmd=(ssh)

        # Add username if provided
        if [ -n "$username" ]; then
            vanilla_ssh_cmd+=(-l "$username")
        fi

        # Add verbose flag if set
        if [ "$verbose_flag" = "1" ]; then
            vanilla_ssh_cmd+=(-v)
        fi

        # Add search term (the hostname)
        vanilla_ssh_cmd+=("$search_term")

        # Add remaining arguments (captured in $@ after shift on line 451)
        if [ $# -gt 0 ]; then
            vanilla_ssh_cmd+=("$@")
        fi

        # Clean up timing stages before exec
        if [ -n "$WRAPPER_START_STAGE" ]; then
            end_stage "wrapper-start" "$WRAPPER_START_STAGE" "$search_term"
            WRAPPER_START_STAGE=""
        fi
        if [ -n "$WRAPPER_RUN_START" ]; then
            end_run "$WRAPPER_RUN_LABEL" "$WRAPPER_RUN_START" "$search_term" "0"
            WRAPPER_RUN_START=""
        fi
        cleanup_pass_pipes

        # exec replaces current process with vanilla SSH
        exec "${vanilla_ssh_cmd[@]}"
    fi

    # Show error messages for other errors (filter out timing logs)
    echo "$output" | grep -v "TIMING:" >&2
    discard_pending_pass_fd
    finalize_and_exit $connect_status
fi

# Parse result: hostname|filepath|username|password-token|auth-type
if [[ "$result" =~ ^([^|]+)\|([^|]*)\|([^|]*)\|([^|]*)\|(.*)$ ]]; then
    target_host="${BASH_REMATCH[1]}"
    filepath="${BASH_REMATCH[2]}"
    resolved_username="${BASH_REMATCH[3]}"
    password_token="${BASH_REMATCH[4]}"
    detected_auth_type="${BASH_REMATCH[5]}"

    has_pipe_password=0

    if [[ "$password_token" == @fd:* ]]; then
        if [ -z "$PENDING_PASS_READ_FD" ]; then
            echo "Error: Credential pipe missing" >&2
            finalize_and_exit 1
        fi
        activate_pending_pass_fd
        has_pipe_password=1
    elif [ -n "$password_token" ]; then
        echo "Error: Unsupported credential token '$password_token'" >&2
        discard_pending_pass_fd
        finalize_and_exit 1
    else
        discard_pending_pass_fd
    fi

    # Determine final username: use resolved username, or fall back to requested username
    final_username="${resolved_username}"
    if [ -z "$final_username" ] && [ -n "$username" ]; then
        final_username="$username"
    fi

    RUN_DETAIL="$target_host"

    if [ -n "$WRAPPER_START_STAGE" ]; then
        end_stage "wrapper-start" "$WRAPPER_START_STAGE" "$target_host"
        WRAPPER_START_STAGE=""
    fi

    # Recording setup stage - check if session recording is needed
    begin_stage "recording-setup" "$target_host" recording_stage_start

    recording_enabled=0
    recording_cast_path=""
    recording_append=1
    recording_title=""
    recording_asciinema_bin=""
    recording_lock_dir=""
    recording_reason=""
    recording_sequence=""
    recording_session_label=""

    # Skip expensive recording check if explicitly disabled (e.g., during benchmarks)
    if [ "${NSSH_RECORD:-1}" = "0" ]; then
        recording_enabled=0
        recording_reason="recording disabled by NSSH_RECORD=0"
    else
        plan_output=$(run_recording_check "$target_host" 2>/dev/null)
        plan_status=$?
        if [ $plan_status -ne 0 ]; then
            echo "Error: recording check failed" >&2
            finalize_and_exit $plan_status
        fi

        while IFS='=' read -r key value; do
            [ -z "$key" ] && continue
            case "$key" in
                enabled)
                    recording_enabled="$value"
                    ;;
                reason)
                    recording_reason="$value"
                    ;;
                cast_path)
                    recording_cast_path="$value"
                    ;;
                append)
                    recording_append="$value"
                    ;;
                title)
                    recording_title="$value"
                    ;;
                asciinema_bin)
                    recording_asciinema_bin="$value"
                    ;;
                lock_dir)
                    recording_lock_dir="$value"
                    ;;
                sequence)
                    recording_sequence="$value"
                    ;;
                session_label)
                    recording_session_label="$value"
                    ;;
            esac
        done <<<"$plan_output"
    fi

    end_stage "recording-setup" "$recording_stage_start" "$target_host"

    # Build SSH command
    ssh_cmd=()
    ssh_exec_cmd=()
    auth_method="${detected_auth_type:-unknown}"
    SSH_PASS_FD=""

    if [ $has_pipe_password -eq 1 ]; then
        if [ -z "$ACTIVE_PASS_READ_FD" ]; then
            echo "Error: credential pipe not activated" >&2
            finalize_and_exit 1
        fi

        exec {SSH_PASS_FD}<&${ACTIVE_PASS_READ_FD}
        close_fd_if_set ACTIVE_PASS_READ_FD in
        ssh_cmd=(sshpass -d "$SSH_PASS_FD")
        auth_method="password"
    fi

    # Build SSH command with optional verbose flag
    ssh_base_opts=()
    if [ "$verbose_flag" = "1" ]; then
        ssh_base_opts+=(-v)
    fi

    if [ -n "$final_username" ]; then
        ssh_exec_cmd=(ssh "${ssh_base_opts[@]}" -o "User=$final_username" "$target_host")
    else
        ssh_exec_cmd=(ssh "${ssh_base_opts[@]}" "$target_host")
    fi
    if [ $# -gt 0 ]; then
        ssh_exec_cmd+=("$@")
    fi

    ssh_cmd+=("${ssh_exec_cmd[@]}")

    ssh_cmd_string=""
    if [ ${#ssh_cmd[@]} -gt 0 ]; then
        printf -v ssh_cmd_string '%q ' "${ssh_cmd[@]}"
        ssh_cmd_string="${ssh_cmd_string% }"
    fi

    session_start_iso=""
    session_end_iso=""

    # SSH connection stage - actual SSH connection only
    begin_stage "ssh-connection" "$target_host" ssh_stage_start

    if [ "$recording_enabled" = "1" ]; then
        if [ -z "$recording_cast_path" ] || [ -z "$recording_asciinema_bin" ]; then
            echo "Error: invalid recording plan (missing cast path or binary)" >&2
            finalize_and_exit 1
        fi
        session_start_iso=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
        acquire_recording_lock "$recording_lock_dir"
        timer_log "START: asciinema recording"
        record_cmd=("$recording_asciinema_bin" record --quiet)
        if [ "$recording_append" = "1" ]; then
            record_cmd+=(--append)
        fi
        if [ -n "$recording_title" ]; then
            record_cmd+=(--title "$recording_title")
        fi
        record_cmd+=(--return)
        if [ -n "$ssh_cmd_string" ]; then
            record_cmd+=(--command "$ssh_cmd_string")
        fi
        record_cmd+=("$recording_cast_path")

        # Capture stderr to detect compatibility errors (for recording path)
        ssh_stderr_file=$(mktemp)
        "${record_cmd[@]}" 2>"$ssh_stderr_file"
        ssh_status=$?

        # Output captured stderr to terminal
        if [ -s "$ssh_stderr_file" ]; then
            cat "$ssh_stderr_file" >&2
        fi

        session_end_iso=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
        timer_log "END: asciinema recording (status=$ssh_status)"
        release_recording_lock

        # Show error context if SSH connection failed
        if [ $ssh_status -ne 0 ]; then
            echo "" >&2
            echo "SSH connection failed with exit code $ssh_status" >&2
            echo "Host: $target_host" >&2
            if [ -n "$final_username" ]; then
                echo "User: $final_username" >&2
            fi
            echo "Auth: $auth_method" >&2
            echo "" >&2
            echo "Common causes:" >&2
            echo "  - Host unreachable or SSH service not running" >&2
            echo "  - Authentication failure (wrong credentials or key)" >&2
            echo "  - SSH protocol mismatch (KexAlgorithms, MACs, Ciphers)" >&2
            echo "  - Network connectivity issues" >&2
            echo "" >&2
            echo "Check SSH config and try: ssh -v $target_host" >&2

            # If verbose flag is set, also output raw SSH stderr for parsing
            if [ "$verbose_flag" = "1" ]; then
                echo "" >&2
                echo "=== RAW SSH OUTPUT ===" >&2
                if [ -f "$ssh_stderr_file" ]; then
                    echo "[DEBUG: File exists, size=$(wc -c < "$ssh_stderr_file")]" >&2
                    cat "$ssh_stderr_file" >&2
                else
                    echo "[DEBUG: File does not exist: $ssh_stderr_file]" >&2
                fi
                echo "=== END RAW OUTPUT ===" >&2
            fi

            # Check for compatibility errors and offer to fix
            if [ -f "$ssh_stderr_file" ]; then
                ssh_stderr_content=$(cat "$ssh_stderr_file")
                detect_and_fix_ssh_compatibility "$target_host" "$ssh_stderr_content" "$ssh_status"
            fi
        fi

        # Clean up temp file
        [ -f "$ssh_stderr_file" ] && rm -f "$ssh_stderr_file"
    else
        if [ $has_pipe_password -eq 1 ]; then
            timer_log "START: SSH connection (with sshpass)"
        else
            timer_log "START: SSH connection (key-based)"
        fi

        # Capture stderr to detect compatibility errors
        ssh_stderr_file=$(mktemp)
        "${ssh_cmd[@]}" 2>"$ssh_stderr_file"
        ssh_status=$?

        # Output captured stderr to terminal
        if [ -s "$ssh_stderr_file" ]; then
            cat "$ssh_stderr_file" >&2
        fi

        session_end_iso=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
        timer_log "END: SSH connection (status=$ssh_status)"

        # Show error context if SSH connection failed
        if [ $ssh_status -ne 0 ]; then
            echo "" >&2
            echo "SSH connection failed with exit code $ssh_status" >&2
            echo "Host: $target_host" >&2
            if [ -n "$final_username" ]; then
                echo "User: $final_username" >&2
            fi
            echo "Auth: $auth_method" >&2
            echo "" >&2
            echo "Common causes:" >&2
            echo "  - Host unreachable or SSH service not running" >&2
            echo "  - Authentication failure (wrong credentials or key)" >&2
            echo "  - SSH protocol mismatch (KexAlgorithms, MACs, Ciphers)" >&2
            echo "  - Network connectivity issues" >&2
            echo "" >&2
            echo "Check SSH config and try: ssh -v $target_host" >&2

            # If verbose flag is set, also output raw SSH stderr for parsing
            if [ "$verbose_flag" = "1" ]; then
                echo "" >&2
                echo "=== RAW SSH OUTPUT ===" >&2
                if [ -f "$ssh_stderr_file" ]; then
                    echo "[DEBUG: File exists, size=$(wc -c < "$ssh_stderr_file")]" >&2
                    cat "$ssh_stderr_file" >&2
                else
                    echo "[DEBUG: File does not exist: $ssh_stderr_file]" >&2
                fi
                echo "=== END RAW OUTPUT ===" >&2
            fi

            # Check for compatibility errors and offer to fix
            if [ -f "$ssh_stderr_file" ]; then
                ssh_stderr_content=$(cat "$ssh_stderr_file")
                detect_and_fix_ssh_compatibility "$target_host" "$ssh_stderr_content" "$ssh_status"
            fi
        fi

        # Clean up temp file
        [ -f "$ssh_stderr_file" ] && rm -f "$ssh_stderr_file"
    fi

    if [ $has_pipe_password -eq 1 ] && [ -n "$SSH_PASS_FD" ]; then
        exec {SSH_PASS_FD}<&-
    fi

    end_stage "ssh-connection" "$ssh_stage_start" "$target_host"
    begin_stage "wrapper-teardown" "$target_host" WRAPPER_TEARDOWN_STAGE
    RUN_DETAIL="$target_host"
    finalize_and_exit $ssh_status "$target_host"
else
    echo "Error: Invalid output format from connect module" >&2
    echo "$result" >&2
    finalize_and_exit 1
fi
