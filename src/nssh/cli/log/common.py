"""Shared helpers for nssh log CLI commands."""

from __future__ import annotations

import json
import shutil
import subprocess
from datetime import datetime
from pathlib import Path
from typing import List, Optional, Sequence

from nssh.cli import typer

from nssh.cli.common import ui
from nssh.cli.common.selectors import select_via_fzf
from nssh.core.recording import manager as recording
from nssh.core.ui.console import get_console

RECORDING_FILE_OPTION = typer.Option(
    None,
    "--file",
    "-f",
    help="Direct path to recording file",
)

RECORDING_DATE_OPTION = typer.Option(
    None,
    "--date",
    help="Filter interactive picker by date (default: recent sessions)",
)

DRY_RUN_OPTION = typer.Option(
    False,
    "--dry-run",
    help="Preview actions without executing",
)


def _session_updated_timestamp(entry: recording.SessionRecord) -> float:
    """Return POSIX timestamp representing the last update time for a session."""
    try:
        return entry.cast_path.stat().st_mtime
    except OSError:
        # Fall back to finished_at/started_at if the cast is missing (e.g., deleted).
        return entry.finished_at.timestamp()


def _parse_iso_datetime(value: str) -> datetime:
    if value.endswith("Z"):
        value = value[:-1] + "+00:00"
    return datetime.fromisoformat(value)


def _session_duration_seconds(entry: recording.SessionRecord) -> int:
    index_path = entry.cast_path.with_suffix(".index.json")
    total = 0
    try:
        with open(index_path, "r", encoding="utf-8") as handle:
            payload = json.load(handle)
            sessions = payload.get("sessions", [])
    except (OSError, json.JSONDecodeError, AttributeError):
        sessions = []

    for session in sessions or []:
        try:
            started_str = session.get("started_at")
            finished_str = session.get("finished_at")
            if not started_str or not finished_str:
                continue
            started = _parse_iso_datetime(started_str)
            finished = _parse_iso_datetime(finished_str)
            duration = max(int((finished - started).total_seconds()), 0)
            total += duration
        except (ValueError, TypeError):
            continue

    if total > 0:
        return total

    fallback_delta = entry.finished_at - entry.started_at
    return max(int(fallback_delta.total_seconds()), 0)


def load_sessions(
    settings: Optional[recording.RecordingSettings] = None,
) -> List[recording.SessionRecord]:
    """Return all recorded sessions sorted by cast modification time."""
    sessions = list(recording.iter_session_records(settings=settings))
    sessions.sort(key=_session_updated_timestamp, reverse=True)
    return sessions


def print_sessions(rows: Sequence[recording.SessionRecord]) -> None:
    """Render recorded sessions in a shared panel/table layout."""
    console = get_console()

    if not rows:
        console.print("[dim]No sessions found.[/dim]")
        return

    ui.show_panel("SSH Session Log", "Recorded SSH sessions with playback")

    home = str(Path.home())
    local_tz = datetime.now().astimezone().tzinfo
    table_rows = []
    for entry in rows:
        seconds = _session_duration_seconds(entry)
        minutes, rem = divmod(seconds, 60)
        duration_str = f"{minutes:02d}:{rem:02d}"
        session_label = entry.session_label or "-"
        updated_ts = _session_updated_timestamp(entry)
        updated_dt = datetime.fromtimestamp(updated_ts, tz=local_tz)
        updated_str = updated_dt.strftime("%Y-%m-%dT%H:%M:%S")
        cast_display = str(entry.cast_path).replace(home, "~", 1)
        table_rows.append(
            (updated_str, entry.host, session_label, duration_str, cast_display)
        )

    ui.print_table(
        (
            ("Last Updated", "cyan"),
            ("Host", "dim"),
            ("Session", "dim"),
            ("Duration", "dim"),
            ("Cast", ""),
        ),
        table_rows,
    )
    console.print(f"\n[dim]Total: {len(rows)} sessions[/dim]")


def _format_duration(seconds: int) -> str:
    """Return HH:MM:SS when long enough, otherwise MM:SS."""
    minutes, rem = divmod(seconds, 60)
    hours, minutes = divmod(minutes, 60)
    if hours:
        return f"{hours:02d}:{minutes:02d}:{rem:02d}"
    return f"{minutes:02d}:{rem:02d}"


def _select_session_from_rows(
    rows: Sequence[recording.SessionRecord], prompt: str
) -> Path:
    """Launch fzf to pick from provided session rows."""
    options: List[str] = []
    option_map: dict[str, Path] = {}
    home = str(Path.home())

    for idx, entry in enumerate(rows, 1):
        seconds = _session_duration_seconds(entry)
        duration_str = _format_duration(seconds)
        started_local = entry.started_at.astimezone()
        started_str = started_local.strftime("%Y-%m-%d %H:%M:%S")
        label = entry.session_label or "-"
        cast_display = str(entry.cast_path).replace(home, "~", 1)
        option = (
            f"[{idx:03}] {started_str} | {entry.host} | {label} | "
            f"{duration_str} | {cast_display}"
        )
        options.append(option)
        option_map[option] = entry.cast_path

    try:
        selected_line = select_via_fzf(options, prompt)
    except typer.Exit as exc:  # pragma: no cover - depends on user cancellation
        if exc.exit_code == 0:
            typer.echo("Selection cancelled", err=True)
        elif exc.exit_code == 1:
            typer.echo("Install fzf (brew install fzf) or pass --file", err=True)
        raise

    return option_map[selected_line]


def require_binary(name: str) -> str:
    """Ensure an external binary is present on PATH (asciinema, etc.)."""
    path = shutil.which(name)
    if not path:
        typer.echo(f"Error: '{name}' not found in PATH", err=True)
        raise typer.Exit(code=1)
    return path


def pick_recording_interactive(
    date_str: str | None, settings: recording.RecordingSettings
) -> Path:
    """Launch fzf to select a recording file interactively."""
    sessions = load_sessions(settings)
    recordings_dir = settings.directory.expanduser()

    if not sessions:
        typer.echo(f"No recordings found in {recordings_dir}", err=True)
        raise typer.Exit(code=1)

    if date_str:
        try:
            datetime.strptime(date_str, "%Y-%m-%d")
        except ValueError as exc:
            raise typer.BadParameter("--date must be YYYY-MM-DD") from exc

        filtered = [
            entry
            for entry in sessions
            if entry.started_at.strftime("%Y-%m-%d") == date_str
        ]

        if not filtered:
            typer.echo(
                f"No recordings found for date {date_str} in {recordings_dir}", err=True
            )
            raise typer.Exit(code=1)

        prompt = f"Select recording ({date_str}):"
        return _select_session_from_rows(filtered, prompt)

    return _select_session_from_rows(sessions, "Select recording:")


def resolve_recording_path(
    file: Path | None,
    date: str | None,
    settings: recording.RecordingSettings,
) -> Path:
    """Return the selected recording path from --file or fzf picker."""
    if file:
        target = Path(file).expanduser()
        if not target.exists():
            raise typer.BadParameter(f"File does not exist: {target}")
        return target

    return pick_recording_interactive(date, settings)


def default_export_destination(target: Path, extension: str) -> Path:
    """Derive a friendly filename for exports (hostname_date_session.*)."""
    hostname = target.parent.parent.name
    date = target.parent.name
    session = target.stem
    base_name = f"{hostname}_{date}_{session}"
    return Path.cwd() / f"{base_name}.{extension}"


def run_command(cmd: Sequence[str], dry_run: bool) -> None:
    """Execute a subprocess, honoring --dry-run."""
    if dry_run:
        typer.echo("[dry-run] " + " ".join(cmd))
        return
    subprocess.run(cmd, check=True)


def filter_sessions_by_host(
    sessions: List[recording.SessionRecord],
    host: str,
    date_filter: str,
) -> List[recording.SessionRecord]:
    """Filter sessions by hostname and optional date.

    Args:
        sessions: List of session records to filter.
        host: Hostname to match (case-insensitive substring match).
        date_filter: Date string (YYYY-MM-DD) or '*' for all dates.

    Returns:
        Filtered list of session records.
    """
    result = []
    host_lower = host.lower()

    for session in sessions:
        if host_lower not in session.host.lower():
            continue

        if date_filter != "*":
            session_date = session.started_at.strftime("%Y-%m-%d")
            if session_date != date_filter:
                continue

        result.append(session)

    return result


def _cleanup_empty_dirs(cast_path: Path, base_dir: Path) -> None:
    """Remove empty date and host directories after deletion."""
    date_dir = cast_path.parent
    host_dir = date_dir.parent

    # Remove empty date directory
    try:
        if date_dir.exists() and not any(date_dir.iterdir()):
            date_dir.rmdir()
    except OSError:
        pass

    # Remove empty host directory
    try:
        if host_dir.exists() and host_dir != base_dir and not any(host_dir.iterdir()):
            host_dir.rmdir()
    except OSError:
        pass


def delete_recording(
    cast_path: Path,
    base_dir: Path,
    dry_run: bool,
    console,
) -> None:
    """Delete a recording file and its sidecar index, then cleanup empty dirs.

    Args:
        cast_path: Path to the .cast file.
        base_dir: Base recording directory (for empty dir cleanup boundary).
        dry_run: If True, only print what would be deleted.
        console: Rich console for output.
    """
    index_path = cast_path.with_suffix(".index.json")
    home = str(Path.home())

    cast_display = str(cast_path).replace(home, "~", 1)

    if dry_run:
        console.print(f"[yellow]Would delete:[/yellow] {cast_display}")
        if index_path.exists():
            index_display = str(index_path).replace(home, "~", 1)
            console.print(f"[yellow]Would delete:[/yellow] {index_display}")
        return

    # Delete cast file
    try:
        cast_path.unlink()
        console.print(f"[green]Deleted:[/green] {cast_display}")
    except OSError as exc:
        console.print(f"[red]Failed to delete {cast_display}: {exc}[/red]")
        return

    # Delete sidecar index if it exists
    if index_path.exists():
        try:
            index_path.unlink()
        except OSError:
            pass

    # Cleanup empty directories
    _cleanup_empty_dirs(cast_path, base_dir.expanduser())
