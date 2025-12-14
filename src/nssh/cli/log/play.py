"""Play command for nssh log."""

from __future__ import annotations

from nssh.cli import click
from nssh.core.recording import manager as recording

from . import common


@click.command(short_help="Play a recorded session")
@common.dry_run_option
def play_command(dry_run: bool) -> None:
    """Play a recorded session using asciinema."""
    settings = recording.load_recording_settings()
    target = common.resolve_recording_path(settings)
    asciinema_bin = common.require_binary("asciinema")
    common.run_command([asciinema_bin, "play", str(target)], dry_run)
