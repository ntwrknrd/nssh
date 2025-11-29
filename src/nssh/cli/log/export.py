"""Export command for nssh log."""

from __future__ import annotations

from pathlib import Path

from nssh.cli import click
from nssh.cli.common.banner import NOOP, OK, banner
from nssh.core.recording import manager as recording
from nssh.core.ui.console import get_console

from . import common

console = get_console()


def _resolve_export_format(destination: Path) -> str:
    """Resolve export format from file extension.

    Returns:
        Validated format: 'txt' or 'gif'

    Raises:
        click.BadParameter: On invalid or missing extension
    """
    suffix = destination.suffix.lower()

    # Infer from extension
    if suffix == ".gif":
        return "gif"
    elif suffix == ".txt":
        return "txt"
    elif suffix == "":
        # Lenient: default to txt for backward compatibility
        return "txt"
    else:
        raise click.BadParameter(f"Unsupported extension '{suffix}'. Use .txt or .gif")


@click.command(short_help="Export to text or GIF")
@click.option("-y", "--yes", is_flag=True, default=False, help="Skip confirmation")
@common.dry_run_option
def export_command(yes: bool, dry_run: bool) -> None:
    """Export recording to text or GIF format.

    Format is automatically inferred from the output file extension:
    - .txt for text export
    - .gif for animated GIF
    """
    with banner("EXPORT RECORDING", OK) as set_outcome:
        settings = recording.load_recording_settings()

        # Select recording via fzf
        target = common.resolve_recording_path(settings)

        # Generate smart default path
        default_destination = common.default_export_destination(target, "txt")

        # Prompt for output path
        if yes:
            destination = default_destination
        else:
            output_str = click.prompt(
                "Output path (.txt or .gif)", default=str(default_destination)
            )
            destination = Path(output_str)

        # Resolve format from extension
        try:
            export_format = _resolve_export_format(destination)
        except click.BadParameter as e:
            console.print(f"[red]{e}[/red]")
            raise SystemExit(1)

        if export_format == "gif":
            tool = common.require_binary("asciicast2gif")
            cmd = [tool, str(target), str(destination)]
        else:
            asciinema_bin = common.require_binary("asciinema")
            cmd = [asciinema_bin, "convert", str(target), str(destination)]

        common.run_command(cmd, dry_run)

        if dry_run:
            set_outcome(NOOP)
