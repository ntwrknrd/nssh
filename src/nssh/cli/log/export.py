"""Export command for nssh log."""

from __future__ import annotations

from pathlib import Path
from typing import Literal, Optional

from nssh.cli import typer

from nssh.core.recording import manager as recording

from . import common


def export_command(
    file: Optional[Path] = common.RECORDING_FILE_OPTION,
    date: Optional[str] = common.RECORDING_DATE_OPTION,
    output: Optional[Path] = typer.Option(
        None, "--output", "-o", help="Output file path"
    ),
    format: Literal["txt", "gif"] = typer.Option(
        "txt",
        "--format",
        help="Export format: txt or gif",
    ),
    dry_run: bool = common.DRY_RUN_OPTION,
) -> None:
    """Export recording to text or GIF format."""
    export_format = format

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
