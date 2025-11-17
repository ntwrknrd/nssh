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
                    "List all recorded sessions with filtering options",
                    examples=[
                        "nssh log list --search lab-switch",
                        "nssh log list --search 2025-11-15",
                    ],
                ),
                UsageRow(
                    "nssh log [bold]play[/bold] [OPTIONS]",
                    "Play a recorded session using asciinema",
                    examples=[
                        "nssh log play",
                        "nssh log play --date 2025-11-14",
                        "nssh log play --file session-001.cast",
                    ],
                ),
                UsageRow(
                    "nssh log [bold]upload[/bold] [OPTIONS]",
                    "Upload a recording to an asciinema server",
                    examples=[
                        "nssh log upload  # requires ASCIINEMA_SERVER_URL",
                        "nssh log upload --server https://asciinema.example.com",
                    ],
                ),
                UsageRow(
                    "nssh log [bold]export[/bold] [OPTIONS]",
                    "Export recording to text or GIF format",
                    examples=[
                        "nssh log export",
                        "nssh log export --gif",
                        "nssh log export --output demo.txt",
                    ],
                ),
                UsageRow(
                    "nssh log [bold]cleanup[/bold] [OPTIONS]",
                    "Remove old recordings based on max_age_days configuration",
                    examples=["nssh log cleanup --dry-run"],
                ),
            ],
        ),
        UsageSection(
            "Common Options",
            rows=[
                UsageRow("--file FILE, -f", "Direct path to recording file"),
                UsageRow(
                    "--date YYYY-MM-DD",
                    "Filter interactive picker by date (default: today)",
                ),
                UsageRow("--dry-run", "Preview actions without executing"),
            ],
        ),
        UsageSection(
            "List Options",
            rows=[
                UsageRow(
                    "--search TERM, -s",
                    "Filter by keyword (repeatable for AND logic)",
                )
            ],
        ),
        UsageSection(
            "Export Options",
            rows=[
                UsageRow(
                    "--output FILE, -o",
                    "Output path (default: ./{host}_{date}_{session}.{format})",
                ),
                UsageRow("--txt", "Export as text (default format)"),
                UsageRow("--gif", "Export as animated GIF (requires asciicast2gif)"),
            ],
        ),
        UsageSection(
            "Interactive Mode",
            body="\n".join(
                [
                    "When --file is not specified, commands launch an fzf picker to select a recording.",
                    "Use --date to filter by a specific date (defaults to today).",
                ]
            ),
            body_style="dim",
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
