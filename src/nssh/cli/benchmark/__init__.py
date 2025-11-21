"""nssh benchmark CLI package wiring."""

from __future__ import annotations

from typing import Sequence

from nssh.cli import typer
from nssh.cli.common.app import run_cli
from nssh.cli.common.help import UsageRow, UsageSection, render_usage

from .capture import capture_command
from .common import APP_TITLE

APP_SUBTITLE = "Measure nssh overhead on your system"

app = typer.Typer(add_help_option=False, rich_markup_mode=None)

# Register as a regular command with a short, intuitive name
app.command("run")(capture_command)


def _usage_sections() -> list[UsageSection]:
    return [
        UsageSection(
            "Usage",
            rows=[
                UsageRow(
                    "nssh benchmark run HOST [OPTIONS]",
                    "Measure timing and archive results in benchmark/{timestamp}/",
                ),
            ],
        ),
        UsageSection(
            "Options",
            rows=[
                UsageRow(
                    "--warmups N",
                    "Number of warmup runs ignored in summary (default: 1)",
                ),
                UsageRow(
                    "--samples N",
                    "Number of measured runs (default: 3)",
                ),
                UsageRow(
                    "--simple-only",
                    "Disable instrumentation; report totals only",
                ),
                UsageRow(
                    "--no-record",
                    "Force disable session recording (overrides env/config)",
                ),
            ],
        ),
    ]


def print_usage() -> None:
    """Display usage instructions consistent with other CLIs."""
    render_usage(APP_TITLE, APP_SUBTITLE, _usage_sections())


def main(argv: Sequence[str] | None = None):
    """Entry point mirroring help UX used by other CLIs."""
    run_cli(
        app,
        cli_name=APP_TITLE,
        usage_cb=print_usage,
        completion_prefix="BENCHMARK",
        argv=argv,
    )


def cli_main() -> None:
    """Entry point for python -m usage."""
    main()


if __name__ == "__main__":
    cli_main()
