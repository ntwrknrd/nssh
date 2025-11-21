"""Experimental PTY-based connector that shells out to OpenSSH directly."""

from __future__ import annotations

import errno
import fcntl
import io
import os
import pty
import re
import select
import selectors
import shutil
import signal
import struct
import subprocess
import sys
import tempfile
import termios
import tty
from contextlib import contextmanager, nullcontext
from dataclasses import dataclass
from pathlib import Path
from types import FrameType
from typing import (
    TYPE_CHECKING,
    Any,
    BinaryIO,
    Callable,
    Optional,
    Sequence,
    Tuple,
    cast,
)

if TYPE_CHECKING:
    from nssh.core.recording.manager import RecordingPlan

from nssh.core.diag import timing as timing_core

PASSWORD_PATTERNS = (
    re.compile(rb"password:\s*$", re.IGNORECASE),
    re.compile(rb"passcode:\s*$", re.IGNORECASE),
)
HOSTKEY_PROMPT_RE = re.compile(
    rb"Are you sure you want to continue connecting.*\(yes/no/\[fingerprint\]\)\?",
    re.IGNORECASE | re.DOTALL,
)


def _is_tty(fd: int | None) -> bool:
    if fd is None:
        return False
    try:
        return os.isatty(fd)
    except OSError:
        return False


@contextmanager
def _raw_mode(fd: int | None):
    """Temporarily place the user terminal into raw mode."""

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
    """Ensure `fd` is non-blocking for the duration of the context."""

    if fd is None:
        yield
        return

    orig_flags = fcntl.fcntl(fd, fcntl.F_GETFL)
    try:
        fcntl.fcntl(fd, fcntl.F_SETFL, orig_flags | os.O_NONBLOCK)
        yield
    finally:
        fcntl.fcntl(fd, fcntl.F_SETFL, orig_flags)


def _fetch_winsize(fd: int | None) -> tuple[int, int] | None:
    if fd is None:
        return None

    try:
        winsize = fcntl.ioctl(fd, termios.TIOCGWINSZ, b"\x00" * 8)
        rows, cols, _, _ = struct.unpack("HHHH", winsize)
        if rows and cols:
            return rows, cols
    except OSError:
        pass

    try:
        size = shutil.get_terminal_size()
        return size.lines, size.columns
    except OSError:
        return None


def _apply_winsize(fd: int, rows: int, cols: int) -> None:
    buf = struct.pack("HHHH", rows, cols, 0, 0)
    fcntl.ioctl(fd, termios.TIOCSWINSZ, buf)


def _build_ssh_command(
    hostname: str,
    *,
    username: str | None,
    ssh_args: Sequence[str] | None,
) -> list[str]:
    target = f"{username}@{hostname}" if username else hostname
    cmd = ["ssh", "-tt"]

    # Split ssh_args into options and command
    # SSH syntax: ssh [options] hostname [command]
    # The -- separator marks the start of the remote command
    if ssh_args and "--" in ssh_args:
        separator_idx = list(ssh_args).index("--")
        options = ssh_args[:separator_idx]
        command = ssh_args[separator_idx:]  # Includes --

        if options:
            cmd.extend(options)
        cmd.append(target)
        cmd.extend(command)
    else:
        if ssh_args:
            cmd.extend(ssh_args)
        cmd.append(target)

    return cmd


def _wrap_with_recording_proxy(command: Sequence[str]) -> list[str]:
    python_bin = sys.executable or shutil.which("python3") or "python3"
    return [python_bin, "-m", "nssh.core.recording.proxy", *command]


@dataclass
class _ConnectorContext:
    master_fd: int
    child_pid: int


class PtyConnector:
    """Bridge stdin/stdout with a child ssh process running inside a PTY."""

    def __init__(
        self,
        *,
        hostname: str,
        username: str | None,
        password: str | None,
        ssh_args: Sequence[str] | None = None,
        env: dict[str, str] | None = None,
        recording_plan: RecordingPlan | None = None,
        stdout: BinaryIO | io.TextIOBase | None = None,
        attach_stdin: bool = True,
    ) -> None:
        self.hostname = hostname
        self.username = username
        self.password = password
        self.ssh_args = list(ssh_args or [])
        self.env = env or os.environ.copy()
        self.recording_plan = recording_plan
        self._selector = selectors.DefaultSelector()
        self._password_sent = False
        self._buffer = bytearray()
        self._winch_pending = False
        if attach_stdin:
            try:
                self._stdin_fd = sys.stdin.fileno()
            except (AttributeError, ValueError):
                self._stdin_fd = None
        else:
            self._stdin_fd = None
        stdout_handle: BinaryIO | io.TextIOBase
        if stdout is not None:
            stdout_handle = stdout
        else:
            stdout_handle = cast(io.TextIOBase, sys.stdout)
        self._stdout_handle: BinaryIO | io.TextIOBase = stdout_handle
        stdout_buffer = getattr(self._stdout_handle, "buffer", None)
        if stdout_buffer is not None:
            self._stdout = stdout_buffer
        elif isinstance(stdout_handle, io.TextIOBase):
            self._stdout = io.BytesIO()  # Text handles need a binary bridge
        else:
            self._stdout = cast(BinaryIO, stdout_handle)
        self._stdout_fd: int | None = self._resolve_stdout_fd()
        self._stdout_backlog = bytearray()
        self._original_sigwinch: Callable[[int, FrameType | None], Any] | int | None = (
            None
        )
        self._stdin_attrs = None
        self._stdin_orig_flags: int | None = None
        self._hostkey_prompt_handled = False
        self._hostkey_accept_once = False
        self._hostkey_aliases: set[str] = set()
        self._suppress_echo_bytes = 0
        self._pending_prompt_trim = 0
        self._timing_pipe_dir: Path | None = None
        self._timing_pipe_path: Path | None = None
        self._timing_reader_fd: int | None = None
        self._timing_dummy_writer_fd: int | None = None
        self._timing_buffer = bytearray()
        self._ssh_stage_token: Optional[Tuple[str, int, Optional[str]]] = None
        self._ssh_fallback_enabled = False

        if self._stdin_fd is not None and _is_tty(self._stdin_fd):
            try:
                attrs = termios.tcgetattr(self._stdin_fd)
                self._stdin_attrs = attrs[:] if attrs else None
            except termios.error:
                self._stdin_attrs = None

    def run(self) -> int:
        if shutil.which("ssh") is None:
            raise RuntimeError("OpenSSH binary 'ssh' not found in PATH")

        def _run_session() -> int:
            with timing_core.stage("pty-start", detail=self.hostname):
                ctx = self._spawn_child()

            exit_code = 1
            try:
                if self.recording_plan and self.recording_plan.enabled:
                    with timing_core.stage("recording-session", detail=self.hostname):
                        exit_code = self._relay(ctx, track_recording=True)
                else:
                    with timing_core.stage("ssh-connection", detail=self.hostname):
                        exit_code = self._relay(ctx, track_recording=False)
            finally:
                with timing_core.stage("pty-teardown", detail=self.hostname):
                    try:
                        os.close(ctx.master_fd)
                    except OSError:
                        pass
                    self._cleanup_hostkey_accept_once()
                    self._teardown_recording_channel()
            return exit_code

        lock_ctx: nullcontext[None] | Any = nullcontext()
        if self.recording_plan and self.recording_plan.enabled:
            from nssh.core.recording.manager import acquire_session_lock

            lock_ctx = acquire_session_lock(self.recording_plan.lock_directory)

        with lock_ctx:
            return _run_session()

    # ------------------------------------------------------------------
    # Internal helpers

    def _spawn_child(self) -> _ConnectorContext:
        recording_enabled = bool(self.recording_plan and self.recording_plan.enabled)
        if recording_enabled:
            self._initialize_recording_channel()

        command = _build_ssh_command(
            self.hostname, username=self.username, ssh_args=self.ssh_args
        )

        # Wrap with asciinema if recording enabled
        if recording_enabled:
            from nssh.core.recording.manager import build_asciinema_command

            assert self.recording_plan is not None  # Guaranteed by recording_enabled
            command = _wrap_with_recording_proxy(command)
            command = build_asciinema_command(self.recording_plan, command)

        child_pid, master_fd = pty.fork()
        if child_pid == 0:  # pragma: no cover - child process
            if recording_enabled:
                self._close_recording_channel_fds()
            os.execvpe(command[0], command, self.env)
            os._exit(127)

        os.set_blocking(master_fd, False)
        return _ConnectorContext(master_fd=master_fd, child_pid=child_pid)

    def _initialize_recording_channel(self) -> None:
        if self._timing_pipe_path is not None:
            return
        base_dir: Path | None = None
        pipe_path: Path | None = None
        try:
            base_dir = Path(tempfile.mkdtemp(prefix="nssh-recording-"))
            pipe_path = base_dir / "timing.pipe"
            os.mkfifo(pipe_path, 0o600)
            reader_fd = os.open(pipe_path, os.O_RDONLY | os.O_NONBLOCK)
            dummy_writer_fd = os.open(pipe_path, os.O_WRONLY | os.O_NONBLOCK)
        except (AttributeError, OSError):
            if pipe_path:
                try:
                    pipe_path.unlink()
                except OSError:
                    pass
            if base_dir:
                try:
                    base_dir.rmdir()
                except OSError:
                    pass
            # Platform does not support mkfifo or the filesystem rejected it.
            return
        self._timing_pipe_dir = base_dir
        self._timing_pipe_path = pipe_path
        self._timing_reader_fd = reader_fd
        self._timing_dummy_writer_fd = dummy_writer_fd
        self._timing_buffer.clear()
        self.env["NSSH_TIMING_PIPE_PATH"] = str(pipe_path)

    def _close_recording_channel_fds(self) -> None:
        for attr in ("_timing_reader_fd", "_timing_dummy_writer_fd"):
            fd = getattr(self, attr)
            if fd is None:
                continue
            try:
                os.close(fd)
            except OSError:
                pass

    def _teardown_recording_channel(self) -> None:
        for attr in ("_timing_reader_fd", "_timing_dummy_writer_fd"):
            fd = getattr(self, attr)
            if fd is None:
                continue
            try:
                os.close(fd)
            except OSError:
                pass
            setattr(self, attr, None)
        if self._timing_pipe_path:
            try:
                self._timing_pipe_path.unlink()
            except OSError:
                pass
        if self._timing_pipe_dir:
            try:
                self._timing_pipe_dir.rmdir()
            except OSError:
                pass
        self._timing_pipe_dir = None
        self._timing_pipe_path = None
        self._timing_buffer.clear()
        self.env.pop("NSSH_TIMING_PIPE_PATH", None)

    def _relay(self, ctx: _ConnectorContext, *, track_recording: bool) -> int:
        stdin_fd = self._stdin_fd if _is_tty(self._stdin_fd) else None

        self._register_signal_handlers(ctx.master_fd)
        try:
            with _raw_mode(stdin_fd), _nonblocking(stdin_fd):
                if stdin_fd is not None:
                    self._selector.register(stdin_fd, selectors.EVENT_READ, "stdin")
                self._selector.register(ctx.master_fd, selectors.EVENT_READ, "master")
                has_timing_pipe = track_recording and self._timing_reader_fd is not None
                if has_timing_pipe:
                    assert (
                        self._timing_reader_fd is not None
                    )  # Guaranteed by has_timing_pipe
                    self._selector.register(
                        self._timing_reader_fd, selectors.EVENT_READ, "timing"
                    )
                self._ssh_fallback_enabled = track_recording and not has_timing_pipe

                self._apply_initial_winsize(ctx.master_fd)

                while True:
                    if self._winch_pending:
                        self._apply_initial_winsize(ctx.master_fd)
                        self._winch_pending = False

                    events = self._selector.select()
                    for key, _ in events:
                        if key.data == "stdin":
                            if not self._drain_stdin(ctx.master_fd, key.fileobj):
                                self._selector.unregister(key.fileobj)
                        elif key.data == "timing":
                            self._drain_timing_channel()
                        else:
                            if not self._drain_master(ctx.master_fd):
                                exit_code = self._wait_for_child(ctx.child_pid)
                                self._end_ssh_stage()
                                return exit_code
        finally:
            self._end_ssh_stage()
            self._selector.close()
            self._restore_signal_handlers()
            self._ssh_fallback_enabled = False

    def _drain_timing_channel(self) -> None:
        if self._timing_reader_fd is None:
            return
        try:
            chunk = os.read(self._timing_reader_fd, 1024)
        except BlockingIOError:
            return
        except OSError:
            return
        if not chunk:
            return
        self._timing_buffer.extend(chunk)
        while True:
            newline = self._timing_buffer.find(b"\n")
            if newline == -1:
                break
            line = self._timing_buffer[:newline]
            del self._timing_buffer[: newline + 1]
            self._handle_timing_signal(line)

    def _handle_timing_signal(self, payload: bytes) -> None:
        try:
            message = payload.decode("utf-8").strip()
        except UnicodeDecodeError:
            return
        if message == "ssh-start":
            self._maybe_begin_ssh_stage(force=True)
        elif message == "ssh-end":
            self._end_ssh_stage()

    def _maybe_begin_ssh_stage(self, *, force: bool = False) -> None:
        if not force and not self._ssh_fallback_enabled:
            return
        if self._ssh_stage_token is not None:
            return
        logger = timing_core.get_logger()
        self._ssh_stage_token = logger.begin_stage(
            "ssh-connection", detail=self.hostname
        )

    def _end_ssh_stage(self) -> None:
        token = self._ssh_stage_token
        if token is None:
            return
        timing_core.get_logger().end_stage(token)
        self._ssh_stage_token = None

    def _apply_initial_winsize(self, master_fd: int) -> None:
        rows_cols = _fetch_winsize(self._stdin_fd)
        if rows_cols:
            rows, cols = rows_cols
            _apply_winsize(master_fd, rows, cols)

    def _register_signal_handlers(self, master_fd: int) -> None:
        self._tracked_master_fd = master_fd

        def _handle_winch(signum, frame):  # noqa: ARG001
            self._winch_pending = True

        self._original_sigwinch = signal.getsignal(signal.SIGWINCH)
        signal.signal(signal.SIGWINCH, _handle_winch)

    def _restore_signal_handlers(self) -> None:
        if self._original_sigwinch is not None:
            signal.signal(signal.SIGWINCH, self._original_sigwinch)
            self._original_sigwinch = None

    # Selector callbacks -------------------------------------------------

    def _drain_stdin(self, master_fd: int, stdin_obj):
        fd = stdin_obj if isinstance(stdin_obj, int) else stdin_obj.fileno()
        try:
            data = os.read(fd, 4096)
        except BlockingIOError:
            return True
        except OSError as exc:
            if exc.errno == errno.EIO:
                return False
            raise

        if not data:
            return False

        os.write(master_fd, data)
        return True

    def _drain_master(self, master_fd: int) -> bool:
        try:
            data = os.read(master_fd, 4096)
        except BlockingIOError:
            return True
        except OSError as exc:
            if exc.errno == errno.EIO:
                data = b""
            else:
                raise

        if not data:
            return False

        self._buffer.extend(data)
        if len(self._buffer) > 2048:
            del self._buffer[: len(self._buffer) - 2048]
        self._maybe_send_password(master_fd)
        filtered = self._filter_outgoing_bytes(data)
        if filtered:
            self._write_stdout(filtered)
        if self._ssh_fallback_enabled:
            self._maybe_begin_ssh_stage()
        self._maybe_handle_prompts(master_fd)
        return True

    def _maybe_send_password(self, master_fd: int) -> None:
        if not self.password or self._password_sent:
            return

        tail = bytes(self._buffer)
        for pattern in PASSWORD_PATTERNS:
            match = pattern.search(tail)
            if match and match.end() == len(tail):
                prompt_len = len(match.group(0))
                self._pending_prompt_trim += prompt_len
                del self._buffer[len(self._buffer) - prompt_len :]
                os.write(master_fd, self.password.encode("utf-8") + b"\n")
                self._password_sent = True
                break

    def _filter_outgoing_bytes(self, data: bytes) -> bytes:
        view = memoryview(data)
        if self._suppress_echo_bytes:
            drop = min(len(view), self._suppress_echo_bytes)
            view = view[drop:]
            self._suppress_echo_bytes -= drop
        if self._pending_prompt_trim:
            trim = min(len(view), self._pending_prompt_trim)
            if trim:
                if trim >= len(view):
                    self._pending_prompt_trim -= trim
                    return b""
                view = view[:-trim]
                self._pending_prompt_trim -= trim
        return bytes(view)

    def _maybe_handle_prompts(self, master_fd: int) -> None:
        if self._hostkey_prompt_handled:
            return

        if HOSTKEY_PROMPT_RE.search(bytes(self._buffer)):
            self._hostkey_prompt_handled = True
            self._capture_hostkey_aliases()
            self._handle_hostkey_prompt(master_fd)

    def _wait_for_child(self, child_pid: int) -> int:
        _, status = os.waitpid(child_pid, 0)
        return os.waitstatus_to_exitcode(status)

    # Terminal helpers ---------------------------------------------------

    def _enable_raw_mode(self) -> None:
        if self._stdin_fd is None or self._stdin_attrs is None:
            return
        tty.setraw(self._stdin_fd)

    def _disable_raw_mode(self) -> None:
        if self._stdin_fd is None or self._stdin_attrs is None:
            return
        termios.tcsetattr(self._stdin_fd, termios.TCSADRAIN, self._stdin_attrs)

    def _ensure_stdin_flag_cache(self) -> bool:
        """Record the blocking-mode flags for stdin if we haven't already."""

        if self._stdin_fd is None:
            return False
        if self._stdin_orig_flags is None:
            try:
                flags = fcntl.fcntl(self._stdin_fd, fcntl.F_GETFL)
            except OSError:
                return False
            # Remove O_NONBLOCK so future toggles can reapply blocking mode.
            self._stdin_orig_flags = flags & ~os.O_NONBLOCK
        return True

    def _set_nonblocking(self) -> None:
        if not self._ensure_stdin_flag_cache():
            return
        assert (
            self._stdin_orig_flags is not None
        )  # Guaranteed by _ensure_stdin_flag_cache
        fcntl.fcntl(
            self._stdin_fd, fcntl.F_SETFL, self._stdin_orig_flags | os.O_NONBLOCK
        )

    def _set_blocking(self) -> None:
        if not self._ensure_stdin_flag_cache():
            return
        assert (
            self._stdin_orig_flags is not None
        )  # Guaranteed by _ensure_stdin_flag_cache
        fcntl.fcntl(self._stdin_fd, fcntl.F_SETFL, self._stdin_orig_flags)

    @contextmanager
    def _interactive_prompt_mode(self):
        if self._stdin_fd is None:
            yield
            return
        self._set_blocking()
        self._disable_raw_mode()
        try:
            yield
        finally:
            self._enable_raw_mode()
            self._set_nonblocking()

    # Prompt handling ----------------------------------------------------

    def _handle_hostkey_prompt(self, master_fd: int) -> None:
        if self._stdin_fd is None or not _is_tty(self._stdin_fd):
            print(
                "nssh: Host key verification prompt detected but no interactive TTY available.",
                file=sys.stderr,
            )
            print(
                "nssh: Please answer the prompt directly in the SSH session.",
                file=sys.stderr,
            )
            return

        message = (
            "\n[?] New host key for {host}\n"
            "    [y] Accept once\n"
            "    [a] Accept and store in ~/.ssh/known_hosts\n"
            "    [n] Abort\n"
        ).format(host=self.hostname)

        choice = self._prompt_choice(message, default="n")

        if choice == "n":
            os.write(master_fd, b"no\n")
            self._suppress_echo_bytes += len(b"no\n")
            return

        if choice == "a":
            if not self._store_host_key():
                print(
                    "nssh: Failed to store host key; falling back to one-time accept.",
                    file=sys.stderr,
                )
                self._hostkey_accept_once = True
        elif choice == "y":
            self._hostkey_accept_once = True
        os.write(master_fd, b"yes\n")
        self._suppress_echo_bytes += len(b"yes\n")

    def _prompt_choice(self, prompt: str, default: str = "y") -> str:
        valid = {"y", "a", "n"}
        if self._stdin_fd is None:
            return default

        # Check if stdin is registered with the selector and unregister it temporarily
        stdin_was_registered = False
        stdin_key = None
        try:
            stdin_key = self._selector.get_key(self._stdin_fd)
            stdin_was_registered = True
            self._selector.unregister(self._stdin_fd)
        except (KeyError, ValueError):
            pass

        try:
            with self._interactive_prompt_mode():
                # Flush any buffered input that might cause input() to return immediately
                try:
                    termios.tcflush(self._stdin_fd, termios.TCIFLUSH)
                except (OSError, termios.error):
                    pass
                while True:
                    try:
                        response = input(f"{prompt}> ").strip().lower()
                    except EOFError:
                        return default
                    if not response:
                        return default
                    if response in valid:
                        return response
                    print("Enter y, a, or n", file=sys.stderr)
        finally:
            # Re-register stdin with the selector if it was registered before
            if stdin_was_registered and stdin_key is not None:
                try:
                    self._selector.register(
                        self._stdin_fd, stdin_key.events, stdin_key.data
                    )
                except (KeyError, ValueError):
                    pass

    def _store_host_key(self) -> bool:
        scanner = shutil.which("ssh-keyscan")
        if not scanner:
            print("nssh: ssh-keyscan not found in PATH", file=sys.stderr)
            return False

        scan_args = [scanner, "-T", "5"]
        port_override = self._discover_port_override()
        if port_override:
            scan_args.extend(["-p", port_override])
        scan_args.append(self.hostname)

        try:
            result = subprocess.run(
                scan_args,
                capture_output=True,
                text=True,
                check=True,
            )
        except (subprocess.CalledProcessError, OSError) as exc:
            print(
                f"nssh: Failed to capture host key via ssh-keyscan: {exc}",
                file=sys.stderr,
            )
            return False

        key_data = "\n".join(
            line.strip()
            for line in result.stdout.splitlines()
            if line.strip() and not line.startswith("#")
        )
        if not key_data:
            print("nssh: ssh-keyscan returned no key data", file=sys.stderr)
            return False

        known_hosts = self._resolve_known_hosts_file()
        known_hosts.parent.mkdir(parents=True, exist_ok=True)
        try:
            with known_hosts.open("a", encoding="utf-8") as handle:
                handle.write(key_data)
                if not key_data.endswith("\n"):
                    handle.write("\n")
        except OSError as exc:
            print(f"nssh: Failed to write to {known_hosts}: {exc}", file=sys.stderr)
            return False

        print(f"nssh: Stored host key for {self.hostname} in {known_hosts}")
        return True

    def _cleanup_hostkey_accept_once(self) -> None:
        if not self._hostkey_accept_once:
            return

        remover = shutil.which("ssh-keygen")
        if not remover:
            print(
                "nssh: Cannot remove temporary host key entry; ssh-keygen not found.",
                file=sys.stderr,
            )
            return

        known_hosts = self._resolve_known_hosts_file()
        if not known_hosts.exists() or not known_hosts.is_file():
            return

        for target in self._host_entries_for_cleanup():
            try:
                subprocess.run(
                    [remover, "-R", target, "-f", str(known_hosts)],
                    stdout=subprocess.DEVNULL,
                    stderr=subprocess.DEVNULL,
                    check=False,
                )
            except (OSError, subprocess.SubprocessError) as exc:
                print(
                    f"nssh: Failed to remove temporary host key entry for {target}: {exc}",
                    file=sys.stderr,
                )
                break

    def _resolve_known_hosts_file(self) -> Path:
        override = self._parse_ssh_option("UserKnownHostsFile")
        if override:
            first = self._first_option_token(override)
            if first:
                return Path(os.path.expanduser(os.path.expandvars(first)))
        return Path.home() / ".ssh" / "known_hosts"

    def _host_entries_for_cleanup(self) -> list[str]:
        entries = set(self._hostkey_aliases)
        entries.add(self.hostname)
        port = self._discover_port_override()
        if port:
            for alias in list(entries):
                if alias.startswith("[") and alias.endswith("]"):
                    continue
                entries.add(f"[{alias}]:{port}")
        return list(entries)

    def _discover_port_override(self) -> str | None:
        port = None
        for idx, arg in enumerate(self.ssh_args):
            if arg == "-p" and idx + 1 < len(self.ssh_args):
                port = self.ssh_args[idx + 1]
                break
            if arg.startswith("-p") and len(arg) > 2:
                port = arg[2:]
                break
        if port:
            return port

        port_opt = self._parse_ssh_option("Port")
        if port_opt:
            token = self._first_option_token(port_opt)
            if token:
                return token
        return None

    def _parse_ssh_option(self, name: str) -> str | None:
        target = name.lower()
        idx = 0
        args = self.ssh_args
        while idx < len(args):
            arg = args[idx]
            if arg == "-o":
                idx += 1
                if idx >= len(args):
                    break
                option = args[idx]
            elif arg.startswith("-o") and len(arg) > 2:
                option = arg[2:]
            else:
                idx += 1
                continue

            idx += 1
            key, value, idx = self._split_option_token(option, args, idx)
            if key.strip().lower() == target:
                return value.strip() if value else None
        return None

    @staticmethod
    def _split_option_token(
        token: str, args: Sequence[str], idx: int
    ) -> tuple[str, str | None, int]:
        if "=" in token:
            key, value = token.split("=", 1)
            return key, value, idx
        if idx < len(args):
            return token, args[idx], idx + 1
        return token, None, idx

    @staticmethod
    def _first_option_token(raw: str) -> str:
        token = raw.strip().strip('"').strip("'")
        if not token:
            return ""
        for sep in (" ", ","):
            if sep in token:
                token = token.split(sep, 1)[0]
        return token

    def _capture_hostkey_aliases(self) -> None:
        if self._hostkey_aliases:
            return
        try:
            prompt_text = bytes(self._buffer).decode("utf-8", errors="ignore")
        except Exception:
            return
        match = re.search(
            r"The authenticity of host '([^']+)'", prompt_text, flags=re.IGNORECASE
        )
        if match:
            blob = match.group(1).strip()
            if blob:
                host, _, rest = blob.partition(" (")
                host = host.strip()
                if host:
                    self._hostkey_aliases.add(host)
                ip = rest.rstrip(")") if rest else ""
                ip = ip.strip()
                if ip:
                    self._hostkey_aliases.add(ip)

    def _resolve_stdout_fd(self) -> int | None:
        candidates: list[BinaryIO | io.TextIOBase] = [self._stdout]
        if self._stdout_handle is not self._stdout:
            candidates.append(self._stdout_handle)
        for candidate in candidates:
            fileno = getattr(candidate, "fileno", None)
            if not fileno:
                continue
            try:
                return fileno()
            except (OSError, ValueError):
                continue
        return None

    def _write_stdout(self, data: bytes) -> None:
        if not data:
            return
        if self._stdout_fd is None:
            self._stdout.write(data)
            self._stdout.flush()
            return
        self._stdout_backlog.extend(data)
        self._flush_stdout_backlog()

    def _flush_stdout_backlog(self) -> None:
        while self._stdout_backlog:
            if self._stdout_fd is None:
                break
            try:
                written = os.write(self._stdout_fd, self._stdout_backlog)
            except BlockingIOError:
                self._wait_for_stdout_ready()
                continue
            except OSError as exc:
                if exc.errno == errno.EINTR:
                    continue
                raise
            if written == 0:
                # No progress; avoid tight loop by waiting for readiness
                self._wait_for_stdout_ready()
                continue
            del self._stdout_backlog[:written]

    def _wait_for_stdout_ready(self) -> None:
        if self._stdout_fd is None:
            return
        while True:
            try:
                select.select([], [self._stdout_fd], [])
                return
            except InterruptedError:
                continue


def run_with_pty(
    *,
    hostname: str,
    username: str | None,
    password: str | None,
    ssh_args: Sequence[str] | None = None,
    env: dict[str, str] | None = None,
    recording_plan: RecordingPlan | None = None,
) -> int:
    """Helper that instantiates and runs ``PtyConnector``."""

    connector = PtyConnector(
        hostname=hostname,
        username=username,
        password=password,
        ssh_args=ssh_args,
        env=env,
        recording_plan=recording_plan,
    )
    return connector.run()
