"""Recording helper utilities for nssh.

This module centralizes configuration parsing for the automatic asciinema
recorder, surfaces runtime helpers for the PTY connector, and stores simple
session metadata files that future tooling (like ``nssh log``) can consume.
"""

from __future__ import annotations

import fnmatch
import json
import os
import re
import shutil
from contextlib import contextmanager
from dataclasses import dataclass
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Dict, Iterable, Iterator, List, Optional, Pattern, Sequence, Tuple

from nssh.core.diag import timing as timing_core
from nssh.core.env.settings import (
    default_config_path,
    default_state_root,
    expand_path,
    load_toml_config,
)


LOGGER = timing_core.get_logger()


@dataclass(frozen=True)
class HostPattern:
    raw: str
    kind: str  # "glob" or "regex"
    pattern: str
    regex: Optional[Pattern[str]] = None


@dataclass(frozen=True)
class SessionRecord:
    host: str
    cast_path: Path
    started_at: datetime
    finished_at: datetime
    argv: Tuple[str, ...]
    session_label: Optional[str] = None


@dataclass(frozen=True)
class CleanupSummary:
    cutoff: datetime
    dry_run: bool
    removed_casts: Tuple[Path, ...]
    removed_indexes: Tuple[Path, ...]
    removed_host_dirs: Tuple[Path, ...]


def _sanitize_host(hostname: str) -> str:
    # Allow letters, numbers, dot, dash, underscore. Replace others with underscore.
    return re.sub(r"[^A-Za-z0-9._-]+", "_", hostname)


def sanitize_hostname(hostname: str) -> str:
    return _sanitize_host(hostname)


def _load_config() -> Dict[str, object]:
    config_path = default_config_path()
    data = load_toml_config(config_path)
    if data:
        return data
    return {}


def _parse_bool(value: object, default: bool = False) -> bool:
    if value is None:
        return default
    if isinstance(value, bool):
        return value
    if isinstance(value, (int, float)):
        return bool(value)
    if isinstance(value, str):
        lowered = value.strip().lower()
        if lowered in {"1", "true", "yes", "on"}:
            return True
        if lowered in {"0", "false", "no", "off"}:
            return False
    return default


def _parse_list(value: object) -> List[str]:
    if value is None:
        return []
    if isinstance(value, (list, tuple)):
        return [str(item) for item in value]
    return [str(value)]


def _parse_host_patterns(values: Iterable[str]) -> Tuple[HostPattern, ...]:
    patterns: List[HostPattern] = []
    for raw_value in values:
        raw = raw_value.strip()
        if not raw:
            continue
        if raw.lower().startswith("regex:"):
            body = raw.split(":", 1)[1]
            if not body:
                LOGGER.emit_log(
                    "recording.py - Ignoring empty regex pattern in recording config"
                )
                continue
            try:
                compiled = re.compile(body)
            except re.error as exc:
                LOGGER.emit_log(f"recording.py - Invalid regex pattern '{raw}': {exc}")
                continue
            patterns.append(
                HostPattern(raw=raw, kind="regex", pattern=body, regex=compiled)
            )
        else:
            patterns.append(HostPattern(raw=raw, kind="glob", pattern=raw))
    return tuple(patterns)


@dataclass(frozen=True)
class RecordingSettings:
    enabled: bool
    force: bool
    append_mode: bool
    include_patterns: Sequence[HostPattern]
    exclude_patterns: Sequence[HostPattern]
    directory: Path
    max_age_days: Optional[int]
    asciinema_server_url: Optional[str]
    window_size: Optional[str]


def load_recording_settings() -> RecordingSettings:
    config = _load_config()
    recording_cfg = config.get("recording", {}) if isinstance(config, dict) else {}
    if not isinstance(recording_cfg, dict):
        recording_cfg = {}

    env_override = os.getenv("NSSH_RECORD")
    env_dir = os.getenv("NSSH_RECORD_DIR")

    default_dir = default_state_root() / "casts"
    directory = expand_path(env_dir) if env_dir else recording_cfg.get("dir")
    if isinstance(directory, str):
        directory_path = expand_path(directory)
    elif isinstance(directory, Path):
        directory_path = directory
    else:
        directory_path = default_dir

    enabled = _parse_bool(recording_cfg.get("enabled"), default=True)
    force = False
    if env_override:
        normalized = env_override.strip().lower()
        if normalized in {"0", "false", "off"}:
            enabled = False
        elif normalized in {"1", "true", "on"}:
            enabled = True
        elif normalized == "force":
            enabled = True
            force = True
    append_mode = _parse_bool(recording_cfg.get("append_mode"), default=True)
    include_patterns = _parse_host_patterns(
        _parse_list(recording_cfg.get("include_hosts"))
    )
    exclude_patterns = _parse_host_patterns(
        _parse_list(recording_cfg.get("exclude_hosts"))
    )

    max_age = recording_cfg.get("max_age_days")
    max_age_days = None
    if isinstance(max_age, (int, float)):
        max_age_days = int(max_age)
    elif isinstance(max_age, str) and max_age.isdigit():
        max_age_days = int(max_age)

    asciinema_server_url = recording_cfg.get("asciinema_server_url")
    if asciinema_server_url is not None and not isinstance(asciinema_server_url, str):
        asciinema_server_url = str(asciinema_server_url)

    window_size = recording_cfg.get("window_size")
    if window_size is not None and not isinstance(window_size, str):
        window_size = str(window_size)

    return RecordingSettings(
        enabled=enabled,
        force=force,
        append_mode=append_mode,
        include_patterns=include_patterns,
        exclude_patterns=exclude_patterns,
        directory=directory_path,
        max_age_days=max_age_days,
        asciinema_server_url=asciinema_server_url,
        window_size=window_size,
    )


def should_record(hostname: str, settings: Optional[RecordingSettings] = None) -> bool:
    settings = settings or load_recording_settings()
    if not settings.enabled:
        return False
    host = hostname.strip()
    if not host:
        return False
    if settings.exclude_patterns and _matches_patterns(host, settings.exclude_patterns):
        return False
    if settings.include_patterns:
        return _matches_patterns(host, settings.include_patterns)
    return True


def _matches_patterns(host: str, patterns: Sequence[HostPattern]) -> bool:
    for entry in patterns:
        if entry.kind == "regex" and entry.regex:
            if entry.regex.search(host):
                return True
        elif entry.kind == "glob":
            if fnmatch.fnmatch(host, entry.pattern):
                return True
    return False


@dataclass(frozen=True)
class RecordingPlan:
    enabled: bool
    reason: Optional[str] = None
    cast_path: Optional[Path] = None
    append: bool = True
    title: Optional[str] = None
    asciinema_bin: Optional[str] = None
    lock_directory: Optional[Path] = None
    sequence: Optional[int] = None
    session_label: Optional[str] = None
    window_size: Optional[str] = None


def _session_directory(
    hostname: str,
    *,
    settings: RecordingSettings,
    when: Optional[datetime] = None,
) -> Path:
    sanitized = _sanitize_host(hostname)
    if when is None:
        when = datetime.now().astimezone()
    date_part = when.strftime("%Y-%m-%d")
    return settings.directory / sanitized / date_part


def _format_session_label(sequence: int) -> str:
    return f"session-{sequence:03d}"


def _session_counter_path(session_dir: Path) -> Path:
    return session_dir / ".session-counter"


def _is_session_locked(lock_dir: Path) -> bool:
    """Check if a session lock directory is held by a live process.

    Args:
        lock_dir: Path to the lock directory (e.g., .session-000.lock/)

    Returns:
        True if the lock exists and is held by a live process, False otherwise.
    """
    if not lock_dir.exists() or not lock_dir.is_dir():
        return False

    info_file = lock_dir / ".lockinfo"
    if not info_file.exists():
        # Lock directory exists but no info file - treat as stale
        return False

    try:
        content = info_file.read_text(encoding="utf-8")
        # Parse pid=NNN from the .lockinfo file
        for line in content.splitlines():
            if line.startswith("pid="):
                pid_str = line.split("=", 1)[1].strip()
                try:
                    pid = int(pid_str)
                    # Use kill with signal 0 to check if process exists
                    os.kill(pid, 0)
                    # If we get here, process exists
                    return True
                except (ValueError, ProcessLookupError, PermissionError):
                    # Invalid PID or process doesn't exist or no permission
                    return False
    except (OSError, UnicodeDecodeError):
        # Can't read the file - treat as unlocked
        return False

    return False


def _find_unlocked_session(session_dir: Path) -> Optional[int]:
    """Find the most recent unlocked session in the given directory.

    Args:
        session_dir: Path to the session directory (e.g., host/2025-11-15/)

    Returns:
        The session sequence number if an unlocked session is found, None otherwise.
    """
    if not session_dir.exists():
        return None

    # Find all existing session files
    cast_files = list(session_dir.glob("session-*.cast"))
    if not cast_files:
        return None

    # Extract session numbers and sort descending (most recent first)
    session_numbers: List[int] = []
    for cast_file in cast_files:
        match = SESSION_FILE_PATTERN.search(cast_file.name)
        if match:
            try:
                num = int(match.group("num"))
                session_numbers.append(num)
            except (ValueError, TypeError):
                continue

    if not session_numbers:
        return None

    # Check sessions from highest to lowest
    for sequence in sorted(session_numbers, reverse=True):
        session_label = _format_session_label(sequence)
        lock_dir = session_dir / f".{session_label}.lock"
        if not _is_session_locked(lock_dir):
            # Found an unlocked session
            return sequence

    # All sessions are locked
    return None


def _allocate_session_sequence(session_dir: Path) -> int:
    counter_path = _session_counter_path(session_dir)
    counter_path.parent.mkdir(parents=True, exist_ok=True)
    with acquire_lock(counter_path):
        current = -1
        if counter_path.exists():
            try:
                contents = counter_path.read_text(encoding="utf-8").strip()
                if contents:
                    current = int(contents)
            except (OSError, ValueError):
                current = -1

        # Reuse the most recent sequence allocation if no cast exists yet and the
        # slot is not actively locked. This prevents gaps like jumping directly to
        # session-001 when session-000 never produced a recording.
        if current >= 0:
            session_label = _format_session_label(current)
            cast_path = session_dir / f"{session_label}.cast"
            lock_dir = session_dir / f".{session_label}.lock"
            if not cast_path.exists() and not _is_session_locked(lock_dir):
                return current

        next_value = current + 1
        tmp_path = counter_path.with_suffix(".tmp")
        with open(tmp_path, "w", encoding="utf-8") as handle:
            handle.write(str(next_value))
        os.replace(tmp_path, counter_path)
    return next_value


def build_cast_path(
    hostname: str,
    *,
    settings: RecordingSettings,
    when: Optional[datetime] = None,
) -> Path:
    session_dir = _session_directory(hostname, settings=settings, when=when)
    return session_dir / f"{_format_session_label(0)}.cast"


def _compute_plan(
    hostname: str,
    *,
    when: Optional[datetime] = None,
    prepare_dirs: bool = True,
    allocate_sequence: bool = True,
) -> RecordingPlan:
    settings = load_recording_settings()
    if not should_record(hostname, settings):
        reason = "recording disabled by settings"
        return RecordingPlan(enabled=False, reason=reason)

    asciinema_bin = shutil.which("asciinema")
    if not asciinema_bin:
        message = "asciinema binary not found in PATH"
        LOGGER.emit_log(f"recording.py - {message}")
        if not settings.force:
            return RecordingPlan(enabled=False, reason=message)
        raise RuntimeError(message)

    session_dir = _session_directory(hostname, settings=settings, when=when)
    cast_path: Optional[Path]
    sequence: Optional[int] = None
    session_label: Optional[str] = None
    lock_directory: Optional[Path] = None

    if prepare_dirs:
        session_dir.mkdir(parents=True, exist_ok=True)
        if allocate_sequence:
            # In append mode, try to reuse an unlocked session
            if settings.append_mode:
                sequence = _find_unlocked_session(session_dir)

            # If no unlocked session found, allocate a new one
            if sequence is None:
                sequence = _allocate_session_sequence(session_dir)

            session_label = _format_session_label(sequence)
            cast_path = session_dir / f"{session_label}.cast"
            lock_directory = session_dir / f".{session_label}.lock"
        else:
            cast_path = session_dir / f"{_format_session_label(0)}.cast"
    else:
        cast_path = session_dir / f"{_format_session_label(0)}.cast"
    title = f"nssh:{hostname}"

    return RecordingPlan(
        enabled=True,
        cast_path=cast_path,
        append=settings.append_mode,
        title=title,
        asciinema_bin=asciinema_bin,
        lock_directory=lock_directory,
        sequence=sequence,
        session_label=session_label,
        window_size=settings.window_size,
    )


@contextmanager
def acquire_lock(lock_path: Path) -> Iterator[None]:
    """Cross-platform best-effort exclusive lock around a sidecar file."""

    lock_file = lock_path.with_suffix(lock_path.suffix + ".lck")
    lock_file.parent.mkdir(parents=True, exist_ok=True)
    handle = lock_file.open("a+")
    try:
        _lock_handle(handle)
        yield
    finally:
        _unlock_handle(handle)
        handle.close()


@contextmanager
def acquire_session_lock(lock_directory: Path | None) -> Iterator[None]:
    """Acquire directory-based lock for recording sessions.

    Args:
        lock_directory: Path to lock directory (e.g., .session-000.lock/)

    Yields:
        None

    The lock directory contains a .lockinfo file with PID for stale detection.
    """
    if lock_directory is None:
        yield
        return

    lock_dir = Path(lock_directory)
    lock_info = lock_dir / ".lockinfo"

    # Try to create lock directory
    max_attempts = 100
    for attempt in range(max_attempts):
        try:
            lock_dir.mkdir(parents=True, exist_ok=False)
            # Successfully created - write our PID
            lock_info.write_text(f"pid={os.getpid()}\ncmd=nssh\n", encoding="utf-8")
            break
        except FileExistsError:
            # Lock exists - check if stale
            if _is_session_locked(lock_dir):
                # Still locked by live process
                import time

                time.sleep(0.1)
                continue
            else:
                # Stale lock - remove and retry
                try:
                    import shutil as shutil_mod

                    shutil_mod.rmtree(lock_dir)
                except OSError:
                    pass
                continue
    else:
        raise RuntimeError(
            f"Failed to acquire recording lock after {max_attempts} attempts"
        )

    try:
        yield
    finally:
        # Release lock
        try:
            lock_info.unlink(missing_ok=True)
            lock_dir.rmdir()
        except OSError:
            pass


def _lock_handle(handle) -> None:
    if os.name == "posix":
        import fcntl

        fcntl.flock(handle.fileno(), fcntl.LOCK_EX)
    elif os.name == "nt":  # pragma: no cover - Windows CI not enabled
        import msvcrt  # type: ignore[import-not-found]

        msvcrt.locking(handle.fileno(), msvcrt.LK_LOCK, 1)  # type: ignore[attr-defined]


def _unlock_handle(handle) -> None:
    if os.name == "posix":
        import fcntl

        fcntl.flock(handle.fileno(), fcntl.LOCK_UN)
    elif os.name == "nt":  # pragma: no cover - Windows CI not enabled
        import msvcrt  # type: ignore[import-not-found]

        msvcrt.locking(handle.fileno(), msvcrt.LK_UNLCK, 1)  # type: ignore[attr-defined]


def build_asciinema_command(plan: RecordingPlan, ssh_cmd: Sequence[str]) -> List[str]:
    if not plan.cast_path or not plan.asciinema_bin:
        raise ValueError("Recording plan missing cast path or asciinema binary")

    # Build SSH command as a single string for asciinema's --command flag
    import shlex

    ssh_cmd_string = " ".join(shlex.quote(arg) for arg in ssh_cmd)

    command: List[str] = [plan.asciinema_bin, "rec", "--quiet"]

    # Use headless mode if requested (disables terminal capability detection)
    if os.getenv("NSSH_RECORD_HEADLESS") in ("1", "true", "True"):
        command.append("--headless")

    # Set window dimensions if configured (e.g., "100x30")
    if plan.window_size:
        command.extend(["--window-size", plan.window_size])

    if plan.append:
        command.append("--append")
    if plan.title:
        command.extend(["--title", plan.title])
    command.extend(["--command", ssh_cmd_string])
    command.append(str(plan.cast_path))
    return command


def _index_payload_path(cast_path: Path) -> Path:
    return cast_path.with_suffix(".index.json")


def cleanup_old_recordings(
    settings: Optional[RecordingSettings] = None,
    *,
    now: Optional[datetime] = None,
    dry_run: bool = False,
) -> Optional[CleanupSummary]:
    cfg = settings or load_recording_settings()
    if cfg.max_age_days is None or cfg.max_age_days <= 0:
        return None
    reference = now or datetime.now().astimezone()
    cutoff = reference - timedelta(days=cfg.max_age_days)
    removed_casts: List[Path] = []
    removed_indexes: List[Path] = []
    removed_host_dirs: List[Path] = []

    base = cfg.directory
    if not base.exists():
        return CleanupSummary(
            cutoff=cutoff,
            dry_run=dry_run,
            removed_casts=tuple(),
            removed_indexes=tuple(),
            removed_host_dirs=tuple(),
        )

    for host_dir in sorted(base.iterdir()):
        if not host_dir.is_dir():
            continue
        cast_files = list(sorted(host_dir.rglob("*.cast")))
        for cast_file in cast_files:
            try:
                mtime = datetime.fromtimestamp(
                    cast_file.stat().st_mtime, tz=timezone.utc
                )
            except OSError:
                continue
            if mtime < cutoff:
                index_path = _index_payload_path(cast_file)
                removed_casts.append(cast_file)
                if not dry_run:
                    try:
                        cast_file.unlink(missing_ok=True)
                    except OSError as exc:
                        LOGGER.emit_log(
                            f"recording.py - Failed to remove {cast_file}: {exc}"
                        )
                if index_path.exists():
                    removed_indexes.append(index_path)
                    if not dry_run:
                        try:
                            index_path.unlink()
                        except OSError as exc:
                            LOGGER.emit_log(
                                f"recording.py - Failed to remove {index_path}: {exc}"
                            )
        if dry_run:
            continue
        # Clean up empty date directories before host directory
        for path in sorted(host_dir.rglob("*"), reverse=True):
            if not path.is_dir():
                continue
            try:
                next(path.iterdir())
            except StopIteration:
                try:
                    path.rmdir()
                except OSError:
                    pass
        if host_dir.exists():
            try:
                next(host_dir.iterdir())
            except StopIteration:
                try:
                    host_dir.rmdir()
                    removed_host_dirs.append(host_dir)
                except OSError:
                    pass

    return CleanupSummary(
        cutoff=cutoff,
        dry_run=dry_run,
        removed_casts=tuple(removed_casts),
        removed_indexes=tuple(removed_indexes),
        removed_host_dirs=tuple(removed_host_dirs),
    )


def validate_configuration(
    settings: Optional[RecordingSettings] = None,
    *,
    check_asciinema: bool = True,
) -> List[str]:
    cfg = settings or load_recording_settings()
    issues: List[str] = []

    directory = cfg.directory
    existing = directory
    while existing and not existing.exists():
        next_parent = existing.parent
        if next_parent == existing:
            break
        existing = next_parent
    if existing and not os.access(existing, os.W_OK):
        issues.append(f"Directory '{directory}' is not writable")

    if cfg.max_age_days is not None and cfg.max_age_days <= 0:
        issues.append("recording.max_age_days must be positive when set")

    if cfg.enabled and check_asciinema and not shutil.which("asciinema"):
        issues.append("asciinema binary not found in PATH")

    return issues


def write_index(
    *,
    cast_path: Path,
    hostname: str,
    started_at: datetime,
    finished_at: datetime,
    exit_code: int,
    auth_method: str,
    ssh_arguments: Sequence[str],
    session_label: Optional[str] = None,
) -> None:
    index_path = _index_payload_path(cast_path)
    sessions: List[Dict[str, object]] = []
    if index_path.exists():
        try:
            with open(index_path, "r", encoding="utf-8") as handle:
                data = json.load(handle)
                sessions = list(data.get("sessions", []))  # type: ignore[arg-type]
        except Exception as exc:  # pragma: no cover - defensive
            LOGGER.emit_log(
                f"recording.py - Failed to parse session index {index_path}: {exc}"
            )

    if session_label is None:
        session_label = extract_session_label(cast_path)

    session_entry = {
        "host": hostname,
        "started_at": started_at.astimezone(timezone.utc).isoformat(),
        "finished_at": finished_at.astimezone(timezone.utc).isoformat(),
        "exit_code": exit_code,
        "auth": auth_method,
        "argv": list(ssh_arguments),
    }
    if session_label:
        session_entry["session"] = session_label
    sessions.append(session_entry)

    payload = {"host": hostname, "cast": str(cast_path), "sessions": sessions}
    tmp_path = index_path.with_suffix(".index.json.tmp")
    index_path.parent.mkdir(parents=True, exist_ok=True)
    with acquire_lock(index_path):
        with open(tmp_path, "w", encoding="utf-8") as handle:
            json.dump(payload, handle, indent=2)
            handle.write("\n")
        os.replace(tmp_path, index_path)


def list_cast_files(settings: Optional[RecordingSettings] = None) -> Iterator[Path]:
    cfg = settings or load_recording_settings()
    base = cfg.directory
    if not base.exists():
        return
    for host_dir in sorted(base.iterdir()):
        if not host_dir.is_dir():
            continue
        cast_files = sorted(host_dir.rglob("*.cast"))
        for cast_file in cast_files:
            yield cast_file


SESSION_FILE_PATTERN = re.compile(r"(session-(?P<num>\d+))\.cast$")


def extract_session_label(path: Path) -> Optional[str]:
    match = SESSION_FILE_PATTERN.search(path.name)
    if match:
        return match.group(1)
    return None


def extract_session_number(path: Path) -> Optional[int]:
    match = SESSION_FILE_PATTERN.search(path.name)
    if not match:
        return None
    try:
        return int(match.group("num"))
    except (TypeError, ValueError):
        return None


def _parse_iso8601(value: str) -> datetime:
    """Parse ISO 8601 datetime string with optional Z suffix."""
    if value.endswith("Z"):
        value = value[:-1] + "+00:00"
    return datetime.fromisoformat(value)


def _read_cast_metadata(cast_file: Path) -> Optional[dict]:
    """Read metadata from asciinema v3 .cast file.

    Returns dict with:
        - started_at: datetime when session started
        - finished_at: datetime when session ended
        - host: hostname extracted from title
        - argv: list of command args
    """
    try:
        with open(cast_file, "r", encoding="utf-8") as f:
            # First line is the header
            header_line = f.readline().strip()
            if not header_line:
                return None
            header = json.loads(header_line)

            # Extract start timestamp (Unix timestamp)
            timestamp = header.get("timestamp")
            if not timestamp:
                return None
            started_at = datetime.fromtimestamp(timestamp, tz=timezone.utc)

            # Extract hostname from title (format: "nssh:hostname")
            title = header.get("title", "")
            host = title.replace("nssh:", "", 1) if title.startswith("nssh:") else ""
            if not host:
                # Fallback: extract from path (casts/hostname/date/session.cast)
                try:
                    host = cast_file.parent.parent.name
                except (IndexError, AttributeError):
                    return None

            # Extract command argv
            command = header.get("command", "")
            argv = command.split() if command else []

            # Calculate duration by summing all event deltas. asciinema stores
            # per-event elapsed seconds rather than absolute offsets, so we add
            # each delta to capture append-mode sessions accurately.
            total_time = 0.0
            for line in f:
                line = line.strip()
                if not line:
                    continue
                try:
                    event = json.loads(line)
                    if isinstance(event, list) and len(event) >= 1:
                        total_time += max(float(event[0]), 0.0)
                except (json.JSONDecodeError, ValueError, TypeError):
                    continue

            finished_at = started_at + timedelta(seconds=total_time)

            return {
                "started_at": started_at,
                "finished_at": finished_at,
                "host": host,
                "argv": argv,
            }
    except Exception as exc:
        LOGGER.emit_log(f"recording.py - Failed to read cast file {cast_file}: {exc}")
        return None


def iter_session_records(
    settings: Optional[RecordingSettings] = None,
) -> Iterator[SessionRecord]:
    cfg = settings or load_recording_settings()
    for cast_file in list_cast_files(cfg):
        metadata = _read_cast_metadata(cast_file)
        if not metadata:
            continue

        session_label = extract_session_label(cast_file)
        yield SessionRecord(
            host=metadata["host"],
            cast_path=cast_file,
            started_at=metadata["started_at"],
            finished_at=metadata["finished_at"],
            argv=tuple(str(arg) for arg in metadata["argv"]),
            session_label=session_label,
        )
