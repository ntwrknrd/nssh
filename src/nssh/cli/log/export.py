"""Export command for nssh log."""

from __future__ import annotations

from pathlib import Path
from typing import Optional

from nssh.cli import typer

from nssh.core.recording import manager as recording

from . import common


def export_command(
    file: Optional[Path] = common.RECORDING_FILE_OPTION,
    date: Optional[str] = common.RECORDING_DATE_OPTION,
    output: Optional[Path] = typer.Option(
        None, "--output", "-o", help="Output file path"
    ),
    txt: bool = typer.Option(False, "--txt", help="Export as text (default format)"),
    gif: bool = typer.Option(
        False,
        "--gif",
        help="Export as animated GIF (requires asciicast2gif)",
    ),
    dry_run: bool = common.DRY_RUN_OPTION,
) -> None:
    """Export recording to text or GIF format."""
    if txt and gif:
        raise typer.BadParameter("Cannot specify both --txt and --gif")

    export_format = "gif" if gif else "txt"

    settings = recording.load_recording_settings()
    target = common.resolve_recording_path(file, date, settings)
    destination = output or common.default_export_destination(target, export_format)

    if export_format == "gif":
        tool = common.require_binary("asciicast2gif")
        cmd = [tool, str(target), str(destination)]
    else:
        asciinema_bin = common.require_binary("asciinema")
        cmd = [asciinema_bin, "convert", str(target), str(destination)]

    common.run_command(cmd, dry_run)
