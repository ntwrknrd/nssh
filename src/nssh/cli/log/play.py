"""Play command for nssh log."""

from __future__ import annotations

from pathlib import Path
from typing import Optional

from nssh.core.recording import manager as recording

from . import common


def play_command(
    file: Optional[Path] = common.RECORDING_FILE_OPTION,
    date: Optional[str] = common.RECORDING_DATE_OPTION,
    dry_run: bool = common.DRY_RUN_OPTION,
) -> None:
    """Play a recorded session using asciinema."""
    settings = recording.load_recording_settings()
    target = common.resolve_recording_path(file, date, settings)
    asciinema_bin = common.require_binary("asciinema")
    common.run_command([asciinema_bin, "play", str(target)], dry_run)
