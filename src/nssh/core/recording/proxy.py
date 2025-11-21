"""Helper invoked under asciinema to emit SSH timing markers.

This tiny wrapper runs the real ``ssh`` command (or any provided command)
as a child process, forwarding the user's TTY and propagating signals.
While the command executes it writes ``ssh-start`` / ``ssh-end`` markers
to a named pipe so the parent PTY connector can measure SSH latency
independently from the recording session.
"""

from __future__ import annotations

import os
import signal
import subprocess
import sys
from typing import Optional, Sequence


def _resolve_pipe_fd() -> Optional[int]:
    path = os.getenv("NSSH_TIMING_PIPE_PATH")
    if not path:
        return None
    try:
        return os.open(path, os.O_WRONLY)
    except OSError:
        return None


def _write_marker(fd: Optional[int], marker: str) -> None:
    if fd is None:
        return
    try:
        os.write(fd, marker.encode("utf-8") + b"\n")
    except OSError:
        pass


def _run_command(args: Sequence[str]) -> int:
    proc: Optional[subprocess.Popen[bytes]] = None

    def _forward(signum, frame):  # noqa: ARG001
        if proc and proc.poll() is None:
            try:
                proc.send_signal(signum)
            except ProcessLookupError:
                pass

    for sig in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP):
        try:
            signal.signal(sig, _forward)
        except (ValueError, OSError):
            # Ignore environments that disallow installing handlers (e.g., thread)
            pass

    try:
        proc = subprocess.Popen(args)  # Inherit the PTY stdio
    except (FileNotFoundError, OSError) as exc:
        print(f"nssh: Failed to start command '{args[0]}': {exc}", file=sys.stderr)
        return 127

    try:
        return proc.wait()
    except KeyboardInterrupt:
        if proc.poll() is None:
            try:
                proc.send_signal(signal.SIGINT)
            except ProcessLookupError:
                pass
        return proc.wait()


def main(argv: Sequence[str] | None = None) -> int:
    args = list(sys.argv[1:] if argv is None else argv)
    if not args:
        print(
            "Usage: python -m nssh.core.recording.proxy COMMAND [ARGS...]",
            file=sys.stderr,
        )
        return 2

    writer_fd = _resolve_pipe_fd()
    try:
        _write_marker(writer_fd, "ssh-start")
        exit_code = _run_command(args)
    finally:
        _write_marker(writer_fd, "ssh-end")
        if writer_fd is not None:
            try:
                os.close(writer_fd)
            except OSError:
                pass
    return exit_code


if __name__ == "__main__":  # pragma: no cover - module entry point
    sys.exit(main())
