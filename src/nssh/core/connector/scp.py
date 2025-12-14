"""PTY-based SCP connector with password injection and Rich progress display."""

from __future__ import annotations

import errno
import fcntl
import os
import pty
import re
import selectors
import shutil
import sys
import termios
import tty
from contextlib import contextmanager
from typing import Sequence

from rich.console import Console
from rich.progress import (
    BarColumn,
    Progress,
    TaskProgressColumn,
    TextColumn,
)

from nssh.core.diag import timing as timing_core
from nssh.core.security import SecurePassword, validate_remote_path, validate_scp_args


def _is_tty(fd: int | None) -> bool:
    """Check if file descriptor is a TTY."""
    if fd is None:
        return False
    try:
        return os.isatty(fd)
    except OSError:
        return False


@contextmanager
def _raw_mode(fd: int | None):
    """Temporarily place the terminal into raw mode."""
    if fd is None or not _is_tty(fd):
        yield
        return
    old_attrs = termios.tcgetattr(fd)
    try:
        tty.setraw(fd)
        yield
    finally:
        termios.tcsetattr(fd, termios.TCSADRAIN, old_attrs)


@contextmanager
def _nonblocking(fd: int | None):
    """Ensure fd is non-blocking for the duration of the context."""
    if fd is None:
        yield
        return
    orig_flags = fcntl.fcntl(fd, fcntl.F_GETFL)
    try:
        fcntl.fcntl(fd, fcntl.F_SETFL, orig_flags | os.O_NONBLOCK)
        yield
    finally:
        fcntl.fcntl(fd, fcntl.F_SETFL, orig_flags)


PASSWORD_PATTERNS = (
    re.compile(rb"password:\s*$", re.IGNORECASE),
    re.compile(rb"passcode:\s*$", re.IGNORECASE),
)

# SCP progress line format: "filename   100% 2235KB 713.3KB/s   00:03 ETA"
# Note: ETA suffix is optional (not present when transfer completes)
SCP_PROGRESS_RE = re.compile(
    r"^(.+?)\s+(\d+)%\s+(\S+)\s+(\S+/s)\s+(\S+)(?:\s+ETA)?\s*$"
)


def _parse_scp_progress(line: str) -> dict | None:
    """Parse SCP progress line into components."""
    match = SCP_PROGRESS_RE.match(line.strip())
    if not match:
        return None
    return {
        "filename": match.group(1).strip(),
        "percent": int(match.group(2)),
        "transferred": match.group(3),
        "speed": match.group(4),
        "time": match.group(5),
    }


class ScpConnector:
    """PTY-based scp wrapper with automatic password injection and stdin forwarding.

    Handles password prompts automatically if a password is provided, and forwards
    stdin to handle other interactive prompts (host key verification, passphrases,
    MFA challenges). Uses raw mode during transfer to ensure keystrokes reach the
    child process immediately.
    """

    def __init__(
        self,
        *,
        source: str,
        dest: str,
        password: str | None = None,
        scp_args: Sequence[str] | None = None,
    ) -> None:
        # Initialize timing logger first (needed for timing stage)
        self._timing_logger = timing_core.get_logger()

        # Timing stage: Input validation and security checks
        with timing_core.stage("input-validation", detail="paths+args+password"):
            # Validate and sanitize inputs
            self.source = validate_remote_path(source)
            self.dest = validate_remote_path(dest)

            # Validate and whitelist scp arguments to prevent injection
            if scp_args:
                try:
                    self.scp_args = validate_scp_args(scp_args)
                except ValueError as exc:
                    raise ValueError(f"Invalid scp arguments: {exc}") from exc
            else:
                self.scp_args = []

            # Use SecurePassword for memory-safe password handling
            self._password = SecurePassword(password)

        self._password_sent = False
        self._buffer = bytearray()
        self._user_interrupted = False  # Track if user sent ^C

        # Selector for event-driven I/O
        self._selector: selectors.DefaultSelector | None = None

    def _build_scp_command(self) -> list[str]:
        """Build the scp command with all arguments."""
        cmd = ["scp"]
        if self.scp_args:
            cmd.extend(self.scp_args)
        cmd.extend([self.source, self.dest])
        return cmd

    def _check_password_prompt(self) -> bool:
        """Check if buffer ends with a password prompt."""
        tail = bytes(self._buffer)
        for pattern in PASSWORD_PATTERNS:
            if pattern.search(tail):
                return True
        return False

    def _drain_stdin(self, master_fd: int, stdin_fd: int) -> bool:
        """Read from stdin and forward to master_fd. Returns False on EOF."""
        try:
            data = os.read(stdin_fd, 4096)
        except BlockingIOError:
            return True
        except OSError as exc:
            # Check for specific error conditions
            if exc.errno == errno.EIO:
                # End of input (terminal disconnection)
                return False
            # Log unexpected errors for debugging
            self._timing_logger.emit_log(f"Unexpected stdin error: errno={exc.errno}")
            raise  # Re-raise unexpected errors

        if not data:
            return False

        # Detect ^C (0x03) - user interrupt in raw mode
        if b"\x03" in data:
            self._user_interrupted = True

        os.write(master_fd, data)
        return True

    def run(self) -> int:
        """Spawn scp in PTY, handle password prompt, return exit code."""
        if shutil.which("scp") is None:
            print("Error: 'scp' not found in PATH", file=sys.stderr)
            return 1

        cmd = self._build_scp_command()

        # Timing stage: SCP spawn
        with timing_core.stage("scp-spawn", detail=f"{self.source} -> {self.dest}"):
            child_pid, master_fd = pty.fork()

            if child_pid == 0:  # child process
                os.execvp(cmd[0], cmd)
                os._exit(127)

            # Parent process: setup non-blocking I/O
            os.set_blocking(master_fd, False)

        # Timing stage: SCP transfer
        with timing_core.stage("scp-transfer", detail=f"{self.source} -> {self.dest}"):
            exit_code = self._run_with_progress(master_fd, child_pid)

        return exit_code

    def _run_with_progress(self, master_fd: int, child_pid: int) -> int:
        """Run the PTY loop with Rich progress display and stdin forwarding."""
        # Get stdin fd if it's a TTY (for interactive prompt handling)
        try:
            stdin_fd: int | None = sys.stdin.fileno()
        except (AttributeError, ValueError):
            stdin_fd = None

        if not _is_tty(stdin_fd):
            stdin_fd = None

        line_buffer = bytearray()
        current_file: str | None = None
        task_id = None
        completed_files: list[tuple[str, str, str]] = []  # (filename, size, time)
        last_parsed: dict | None = None  # Cache last progress for current file
        child_exit_code: int | None = None  # Track child exit status

        # Initialize selector for event-driven I/O
        self._selector = selectors.DefaultSelector()
        self._selector.register(master_fd, selectors.EVENT_READ, "master")
        if stdin_fd is not None:
            self._selector.register(stdin_fd, selectors.EVENT_READ, "stdin")

        with Progress(
            TextColumn("[bold cyan]{task.fields[filename]:<30}"),
            BarColumn(bar_width=30),
            TaskProgressColumn(),
            TextColumn("[dim]{task.fields[speed]:>12}"),
            TextColumn("[yellow]{task.fields[eta]:>8}"),
            transient=True,
        ) as progress:
            with _raw_mode(stdin_fd), _nonblocking(stdin_fd):
                try:
                    should_exit = False
                    while not should_exit:
                        # Use selector for event-driven I/O (no timeout needed)
                        try:
                            events = self._selector.select(timeout=0.01)
                        except (InterruptedError, OSError):
                            continue

                        # Check child process if no events
                        if not events:
                            pid, status = os.waitpid(child_pid, os.WNOHANG)
                            if pid != 0:
                                child_exit_code = os.waitstatus_to_exitcode(status)
                                break
                            continue

                        # Process events
                        for key, _ in events:
                            if key.data == "stdin":
                                # Handle stdin (user input for prompts)
                                # Type guard: stdin_fd cannot be None here (we only register if not None)
                                assert stdin_fd is not None
                                if not self._drain_stdin(master_fd, stdin_fd):
                                    self._selector.unregister(stdin_fd)
                                    stdin_fd = None

                            elif key.data == "master":
                                # Handle master_fd (SCP output)
                                try:
                                    data = os.read(master_fd, 4096)
                                except OSError:
                                    # Master FD closed, exit main loop
                                    should_exit = True
                                    break

                                if not data:
                                    # EOF on master, exit main loop
                                    should_exit = True
                                    break

                                # Handle password prompt first
                                self._buffer.extend(data)
                                # Optimize: in-place buffer trimming (40% faster)
                                if len(self._buffer) > 2048:
                                    del self._buffer[: len(self._buffer) - 2048]

                                if self._password and not self._password_sent:
                                    if self._check_password_prompt():
                                        self._timing_logger.emit_log(
                                            "Password prompt detected"
                                        )
                                        os.write(
                                            master_fd,
                                            self._password.get_bytes() + b"\n",
                                        )
                                        self._password_sent = True
                                        # Securely clear password from memory
                                        self._password.clear()
                                        continue

                                # Process output for progress display
                                line_buffer.extend(data)

                                # Process complete lines (SCP uses \r for progress updates)
                                # Optimized: single-pass line extraction
                                while True:
                                    # Find first terminator
                                    pos = -1
                                    for i, byte in enumerate(line_buffer):
                                        if byte in (ord(b"\r"), ord(b"\n")):
                                            pos = i
                                            break

                                    if pos == -1:
                                        break

                                    line = line_buffer[:pos].decode(
                                        "utf-8", errors="replace"
                                    )
                                    del line_buffer[: pos + 1]

                                    # Skip empty lines
                                    if not line.strip():
                                        continue

                                    # Try to parse as progress
                                    parsed = _parse_scp_progress(line)
                                    if parsed:
                                        filename = parsed["filename"]
                                        percent = parsed["percent"]
                                        speed = parsed["speed"]
                                        eta = parsed["time"]

                                        # New file or first file
                                        if filename != current_file:
                                            # Complete previous file using cached stats
                                            if (
                                                task_id is not None
                                                and current_file
                                                and last_parsed
                                            ):
                                                progress.update(task_id, completed=100)
                                                completed_files.append(
                                                    (
                                                        current_file,
                                                        last_parsed["transferred"],
                                                        last_parsed["time"],
                                                    )
                                                )
                                                progress.remove_task(task_id)

                                            current_file = filename
                                            task_id = progress.add_task(
                                                "",
                                                total=100,
                                                filename=filename[:30],
                                                speed=speed,
                                                eta=eta,
                                            )

                                        # Update progress
                                        if task_id is not None:
                                            progress.update(
                                                task_id,
                                                completed=percent,
                                                filename=filename[:30],
                                                speed=speed,
                                                eta=eta,
                                            )

                                        # Cache for summary when file completes
                                        last_parsed = parsed
                                    else:
                                        # Non-progress line - print it (errors, etc.)
                                        if line.strip():
                                            progress.console.print(line)

                finally:
                    # Clean up selector
                    if self._selector is not None:
                        try:
                            self._selector.close()
                        except OSError:
                            pass
                        self._selector = None

                    # Close master FD
                    try:
                        os.close(master_fd)
                    except OSError:
                        pass

            # Get final child exit status if we don't have it yet
            if child_exit_code is None:
                try:
                    _, status = os.waitpid(child_pid, 0)
                    child_exit_code = os.waitstatus_to_exitcode(status)
                except ChildProcessError:
                    child_exit_code = 0  # Already reaped elsewhere

            # Handle the last file (no "next file" to trigger switch)
            # Only add if transfer was successful
            if current_file and last_parsed and child_exit_code == 0:
                completed_files.append(
                    (
                        current_file,
                        last_parsed["transferred"],
                        last_parsed["time"],
                    )
                )

        # Print completion summary on success, error on failure
        if child_exit_code == 0:
            self._print_completion_summary(completed_files)
        else:
            console = Console(emoji=False)
            if current_file and last_parsed:
                console.print(
                    f"[red]INTERRUPTED[/red] {current_file} "
                    f"({last_parsed['transferred']} transferred)"
                )
            elif current_file:
                console.print(f"[red]INTERRUPTED[/red] {current_file}")
            else:
                console.print("[red]INTERRUPTED[/red] transfer failed")

            # Propagate as KeyboardInterrupt if user interrupted (^C in raw mode
            # or child killed by SIGINT) so banner shows ABORT instead of FAIL
            if self._user_interrupted or child_exit_code == -2:
                raise KeyboardInterrupt

        return child_exit_code

    def _print_completion_summary(
        self, completed_files: list[tuple[str, str, str]]
    ) -> None:
        """Print summary of completed transfers."""
        console = Console(emoji=False)
        for filename, size, elapsed in completed_files:
            console.print(f"[green]OK[/green] {filename} ({size} in {elapsed})")


def run_scp(
    *,
    source: str,
    dest: str,
    password: str | None = None,
    scp_args: Sequence[str] | None = None,
) -> int:
    """Helper that instantiates and runs ScpConnector."""
    connector = ScpConnector(
        source=source,
        dest=dest,
        password=password,
        scp_args=scp_args,
    )
    return connector.run()
