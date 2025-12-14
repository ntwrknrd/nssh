"""Upload command for nssh log."""

from __future__ import annotations

import os

from nssh.cli import click
from nssh.cli.common.banner import NOOP, OK, banner
from nssh.core.recording import manager as recording

from . import common

DEFAULT_SERVER = "https://asciinema.org"


@click.command(short_help="Upload to asciinema server")
@click.option("-y", "--yes", is_flag=True, default=False, help="Skip confirmation")
@common.dry_run_option
def upload_command(yes: bool, dry_run: bool) -> None:
    """Upload a recording to an asciinema server."""
    with banner("UPLOAD RECORDING", OK) as set_outcome:
        settings = recording.load_recording_settings()
        target = common.resolve_recording_path(settings)

        # Get server URL from config/env or prompt
        configured_url = (
            os.environ.get("ASCIINEMA_SERVER_URL") or settings.asciinema_server_url
        )
        default_url = configured_url or DEFAULT_SERVER

        if yes:
            server_url = default_url
        else:
            server_url = click.prompt("Server URL", default=default_url)

        asciinema_bin = common.require_binary("asciinema")
        cmd = [asciinema_bin, "upload", "--server-url", server_url, str(target)]
        common.run_command(cmd, dry_run)

        if dry_run:
            set_outcome(NOOP)
