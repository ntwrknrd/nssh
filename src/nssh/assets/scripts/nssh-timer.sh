# shellcheck shell=bash
# Timing helper functions sourced by nssh-wrapper to keep the main script lean.

if [[ -n "${NSSH_TIMER_LIB_SOURCED:-}" ]]; then
    return
fi
NSSH_TIMER_LIB_SOURCED=1

NSSH_DEBUG=${NSSH_DEBUG:-0}
if [ -z "${TIMING_ENABLED:-}" ]; then
    TIMING_ENABLED=$([ "$NSSH_DEBUG" = "1" ] && echo 1 || echo 0)
fi

_nssh_timer_python() {
    local mode="$1"
    shift
    local project_root="${SCRIPT_DIR:-}"

    # Try uv run from project root (preferred method)
    if command -v uv >/dev/null 2>&1 && [ -n "$project_root" ] && [ -f "$project_root/pyproject.toml" ]; then
        uv run python -m nssh.core.diag.timing_server "$mode" "$@" && return
    fi

    # Try uv tool environment Python (for uv tool install)
    local uv_tool_python="${HOME}/.local/share/uv/tools/nssh/bin/python"
    if [ -x "$uv_tool_python" ]; then
        "$uv_tool_python" -m nssh.core.diag.timing_server "$mode" "$@" && return
    fi

    # Try with PYTHONPATH if running from source
    local cmd=(python3 -m nssh.core.diag.timing_server "$mode" "$@")
    if [ -n "$project_root" ] && [ -d "$project_root/src" ]; then
        PYTHONPATH="${project_root}/src${PYTHONPATH:+:$PYTHONPATH}" "${cmd[@]}" && return
    fi

    # Try direct python3 invocation (works if package is installed, last resort)
    "${cmd[@]}" 2>/dev/null && return

    return 1
}

TIMER_HELPER_PID_VALUE=""
TIMER_HELPER_TMPDIR=""
TIMER_HELPER_IN=""
TIMER_HELPER_OUT=""
TIMER_HELPER_WRITE_FD=""
TIMER_HELPER_READ_FD=""

timer_snapshot_fallback() {
    local start_ns="${1:-}"
    _nssh_timer_python fallback "$start_ns"
}

_use_timer_fallback() {
    local __dest="$1"
    local start_ns="${2:-}"
    local fb
    fb=$(timer_snapshot_fallback "$start_ns")
    printf -v "$__dest" '%s' "$fb"
}

# Helper process stays resident via mkfifo pipes so START/END requests avoid repeated Python startup cost.
init_timer_helper() {
    if [ "$TIMING_ENABLED" != "1" ] || [ -n "$TIMER_HELPER_PID_VALUE" ]; then
        return
    fi

    local tmpdir
    tmpdir=$(mktemp -d "${TMPDIR:-/tmp}/nssh-timer.XXXXXX" 2>/dev/null)
    if [ -z "$tmpdir" ]; then
        tmpdir=$(mktemp -d -t nssh-timer)
    fi
    if [ -z "$tmpdir" ]; then
        return
    fi

    local fifo_in="$tmpdir/timer.in"
    local fifo_out="$tmpdir/timer.out"
    if ! mkfifo "$fifo_in" "$fifo_out" 2>/dev/null; then
        rmdir "$tmpdir" 2>/dev/null
        return
    fi
    _nssh_timer_python helper "$fifo_in" "$fifo_out" &
    TIMER_HELPER_PID_VALUE=$!

    exec 3>"$fifo_in"
    TIMER_HELPER_WRITE_FD=3
    exec 4<"$fifo_out"
    TIMER_HELPER_READ_FD=4

    TIMER_HELPER_TMPDIR="$tmpdir"
    TIMER_HELPER_IN="$fifo_in"
    TIMER_HELPER_OUT="$fifo_out"
}

shutdown_timer_helper() {
	if [ -n "$TIMER_HELPER_WRITE_FD" ]; then
		printf 'STOP\n' >&"$TIMER_HELPER_WRITE_FD" || true
        eval "exec ${TIMER_HELPER_WRITE_FD}>&-"
        TIMER_HELPER_WRITE_FD=""
    fi
    if [ -n "$TIMER_HELPER_READ_FD" ]; then
        eval "exec ${TIMER_HELPER_READ_FD}<&-"
        TIMER_HELPER_READ_FD=""
    fi
    if [ -n "$TIMER_HELPER_PID_VALUE" ]; then
        wait "$TIMER_HELPER_PID_VALUE" 2>/dev/null
        TIMER_HELPER_PID_VALUE=""
    fi
    if [ -n "$TIMER_HELPER_TMPDIR" ]; then
        rm -f "$TIMER_HELPER_IN" "$TIMER_HELPER_OUT" 2>/dev/null
        rmdir "$TIMER_HELPER_TMPDIR" 2>/dev/null
        TIMER_HELPER_TMPDIR=""
    fi
}

# Issue a START/END command and capture the helper response, falling back to the inline Python snippet on any failure.
fetch_timer_snapshot() {
    local __dest="$1"
    local start_ns="${2:-}"
    if [ -z "$__dest" ]; then
        return 1
    fi
    if [ "$TIMING_ENABLED" != "1" ]; then
        _use_timer_fallback "$__dest" "$start_ns"
        return
    fi

    init_timer_helper
    if [ -z "$TIMER_HELPER_READ_FD" ] || [ -z "$TIMER_HELPER_WRITE_FD" ]; then
        _use_timer_fallback "$__dest" "$start_ns"
        return
    fi

    local command="START"
    if [ -n "$start_ns" ]; then
        command="END $start_ns"
    fi

	local line=""
	if { printf '%s\n' "$command" >&"$TIMER_HELPER_WRITE_FD"; } && \
       IFS= read -r -u "$TIMER_HELPER_READ_FD" line && \
       [ -n "$line" ]; then
        printf -v "$__dest" '%s' "$line"
        return
    fi

    shutdown_timer_helper
    _use_timer_fallback "$__dest" "$start_ns"
}

nssh_timer_init() {
    trap 'shutdown_timer_helper' EXIT
}

timer_log() {
    if [ "$NSSH_DEBUG" = "1" ]; then
        local snapshot
        fetch_timer_snapshot snapshot
        local timestamp
        read -r timestamp _ <<<"$snapshot"
        echo "[${timestamp}] TIMING: $1" >&2
    fi
}

emit_run_event() {
    if [ "$TIMING_ENABLED" != "1" ]; then
        return
    fi
    local label="$1"
    local phase="$2"
    local detail="$3"
    local duration_ms="$4"
    local status="$5"
    local exit_code="$6"
    local timestamp="${7:-}"
    if [ -z "$timestamp" ]; then
        local snapshot
        fetch_timer_snapshot snapshot
        read -r timestamp _ <<<"$snapshot"
    fi
    local payload="{\"kind\":\"run\",\"label\":\"${label}\",\"phase\":\"${phase}\""
    if [ -n "$NSSH_BENCHMARK_RUN" ]; then
        payload+=" ,\"run\":${NSSH_BENCHMARK_RUN}"
    fi
    if [ -n "$detail" ]; then
        payload+=" ,\"detail\":\"${detail}\""
    fi
    if [ -n "$duration_ms" ]; then
        payload+=" ,\"duration_ms\":${duration_ms}"
    fi
    if [ -n "$status" ]; then
        payload+=" ,\"status\":\"${status}\""
    fi
    if [ -n "$exit_code" ]; then
        payload+=" ,\"exit_code\":${exit_code}"
    fi
    payload=${payload// ,/,}
    payload+="}"
    echo "[${timestamp}] TIMING: ${payload}" >&2
}

begin_run() {
    local label="$1"
    local detail="$2"
    local dest_var="${3:-}"
    if [ "$TIMING_ENABLED" != "1" ]; then
        emit_run_event "$label" "start" "$detail"
        if [ -n "$dest_var" ]; then
            printf -v "$dest_var" '%s' ""
        fi
        return
    fi
    local snapshot
    fetch_timer_snapshot snapshot
    local timestamp start_ns
    read -r timestamp start_ns <<<"$snapshot"
    emit_run_event "$label" "start" "$detail" "" "" "" "$timestamp"
    if [ -n "$dest_var" ]; then
        printf -v "$dest_var" '%s' "$start_ns"
    fi
}

end_run() {
    local label="$1"
    local start_ns="$2"
    local detail="$3"
    local exit_code="$4"
    if [ "$TIMING_ENABLED" != "1" ] || [ -z "$start_ns" ]; then
        emit_run_event "$label" "finish" "$detail" "" "" "$exit_code"
        return
    fi
    local snapshot
    fetch_timer_snapshot snapshot "$start_ns"
    local timestamp end_ns duration
    read -r timestamp end_ns duration <<<"$snapshot"
    local status="ok"
    if [ -n "$exit_code" ] && [ "$exit_code" -ne 0 ]; then
        status="error"
    fi
    emit_run_event "$label" "finish" "$detail" "$duration" "$status" "$exit_code" "$timestamp"
}

emit_stage_event() {
    if [ "$TIMING_ENABLED" != "1" ]; then
        return
    fi
    local stage="$1"
    local phase="$2"
    local detail="$3"
    local duration_ms="$4"
    local timestamp="${5:-}"
    if [ -z "$timestamp" ]; then
        local snapshot
        fetch_timer_snapshot snapshot
        read -r timestamp _ <<<"$snapshot"
    fi
    local payload="{\"kind\":\"stage\",\"stage\":\"${stage}\",\"phase\":\"${phase}\""
    if [ -n "$NSSH_BENCHMARK_RUN" ]; then
        payload+=" ,\"run\":${NSSH_BENCHMARK_RUN}"
    fi
    if [ -n "$detail" ]; then
        payload+=" ,\"detail\":\"${detail}\""
    fi
    if [ -n "$duration_ms" ]; then
        payload+=" ,\"duration_ms\":${duration_ms}"
    fi
    payload=${payload// ,/,}
    payload+="}"
    echo "[${timestamp}] TIMING: ${payload}" >&2
}

begin_stage() {
    local stage="$1"
    local detail="$2"
    local dest_var="${3:-}"
    if [ "$TIMING_ENABLED" != "1" ]; then
        emit_stage_event "$stage" "start" "$detail"
        if [ -n "$dest_var" ]; then
            printf -v "$dest_var" '%s' ""
        fi
        return
    fi
    local snapshot
    fetch_timer_snapshot snapshot
    local timestamp start_ns
    read -r timestamp start_ns <<<"$snapshot"
    emit_stage_event "$stage" "start" "$detail" "" "$timestamp"
    if [ -n "$dest_var" ]; then
        printf -v "$dest_var" '%s' "$start_ns"
    fi
}

end_stage() {
    local stage="$1"
    local start_ns="$2"
    local detail="$3"
    if [ "$TIMING_ENABLED" != "1" ] || [ -z "$start_ns" ]; then
        emit_stage_event "$stage" "finish" "$detail"
        return
    fi
    local snapshot
    fetch_timer_snapshot snapshot "$start_ns"
    local timestamp end_ns duration
    read -r timestamp end_ns duration <<<"$snapshot"
    emit_stage_event "$stage" "finish" "$detail" "$duration" "$timestamp"
}
