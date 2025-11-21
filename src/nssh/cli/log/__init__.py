"""nssh log CLI package wiring."""

from __future__ import annotations

from typing import Sequence

from nssh.cli import typer
from nssh.cli.common.app import run_cli
from nssh.cli.common.help import UsageRow, UsageSection, render_usage

from .cleanup import cleanup_command
from .export import export_command
from .listing import list_sessions
from .play import play_command
from .upload import upload_command

APP_TITLE = "nssh log"
APP_SUBTITLE = "Inspect, play, and manage nssh recording files"

app = typer.Typer(
    help=APP_SUBTITLE,
    add_help_option=False,
    rich_markup_mode=None,
)

app.command("list")(list_sessions)
app.command("play")(play_command)
app.command("upload")(upload_command)
app.command("export")(export_command)
app.command("cleanup")(cleanup_command)


def _usage_sections() -> list[UsageSection]:
    return [
        UsageSection(
            "Commands",
            rows=[
                UsageRow(
                    "nssh log [bold]list[/bold] [OPTIONS]",
                    "List recorded sessions",
                ),
                UsageRow(
                    "nssh log [bold]play[/bold] [OPTIONS]",
                    "Play a recorded session",
                ),
                UsageRow(
                    "nssh log [bold]upload[/bold] [OPTIONS]",
                    "Upload recording to asciinema server",
                ),
                UsageRow(
                    "nssh log [bold]export[/bold] [OPTIONS]",
                    "Export to text or GIF format",
                ),
                UsageRow(
                    "nssh log [bold]cleanup[/bold] [OPTIONS]",
                    "Remove old recordings",
                ),
            ],
        ),
        UsageSection(
            "Options",
            rows=[
                UsageRow("--file FILE, -f", "Path to recording file"),
                UsageRow("--date YYYY-MM-DD", "Filter by date"),
                UsageRow("--search TERM, -s", "Filter by keyword"),
                UsageRow("--output FILE, -o", "Output path for export"),
                UsageRow("--format txt|gif", "Export format (default: txt)"),
                UsageRow("--dry-run", "Preview without executing"),
            ],
        ),
    ]


def print_usage() -> None:
    render_usage(APP_TITLE, APP_SUBTITLE, _usage_sections())


def main(argv: Sequence[str] | None = None) -> None:
    run_cli(
        app,
        cli_name=APP_TITLE,
        usage_cb=print_usage,
        completion_prefix="LOG",
        argv=argv,
    )


def cli_main() -> None:
    """Entry point for running as a module (python -m nssh.cli.log)."""
    main()


if __name__ == "__main__":
    cli_main()
