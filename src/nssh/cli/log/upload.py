"""Upload command for nssh log."""

from __future__ import annotations

import os
from pathlib import Path
from typing import Optional

from nssh.cli import typer

from nssh.core.recording import manager as recording

from . import common


def upload_command(
    file: Optional[Path] = common.RECORDING_FILE_OPTION,
    date: Optional[str] = common.RECORDING_DATE_OPTION,
    server: Optional[str] = typer.Option(
        None, "--server", help="Asciinema server URL (overrides ASCIINEMA_SERVER_URL)"
    ),
    dry_run: bool = common.DRY_RUN_OPTION,
) -> None:
    """Upload a recording to an asciinema server."""
    settings = recording.load_recording_settings()
    target = common.resolve_recording_path(file, date, settings)

    server_url = server or os.environ.get("ASCIINEMA_SERVER_URL")
    if not server_url:
        typer.echo(
            "Error: No asciinema server configured.\n\n"
            "Set ASCIINEMA_SERVER_URL environment variable:\n"
            "  export ASCIINEMA_SERVER_URL=https://asciinema.example.com\n\n"
            "Or use --server option:\n"
            "  nssh log upload --server https://asciinema.example.com",
            err=True,
        )
        raise typer.Exit(code=1)

    asciinema_bin = common.require_binary("asciinema")
    cmd = [asciinema_bin, "upload", "--server-url", server_url, str(target)]
    common.run_command(cmd, dry_run)
