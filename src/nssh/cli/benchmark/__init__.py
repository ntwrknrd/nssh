"""nssh benchmark CLI package wiring."""

from __future__ import annotations

from typing import Sequence

from nssh.cli import typer
from nssh.cli.common.app import run_cli
from nssh.cli.common.help import UsageRow, UsageSection, render_usage

from .analyze import analyze_command
from .capture import capture_command
from .common import APP_TITLE

APP_SUBTITLE = "Measure nssh overhead on your system"

app = typer.Typer(add_help_option=False, rich_markup_mode=None)

app.command("capture")(capture_command)
app.command("analyze")(analyze_command)


def _usage_sections() -> list[UsageSection]:
    return [
        UsageSection(
            "Commands",
            rows=[
                UsageRow(
                    "nssh benchmark capture HOST [OPTIONS]",
                    "Run nssh with NSSH_DEBUG timing enabled and save the log",
                ),
                UsageRow(
                    "nssh benchmark analyze [LOG] [OPTIONS]",
                    "Render or archive a saved timing log (defaults to stdin)",
                ),
            ],
        ),
        UsageSection(
            "Capture Options",
            rows=[
                UsageRow(
                    "-o, --output PATH",
                    "Write raw timing log to PATH (default: timing.log)",
                ),
                UsageRow(
                    "--dry-run",
                    "Replace SSH dial with a ProxyCommand stub",
                ),
                UsageRow(
                    "--no-session-exit",
                    "Keep the SSH session open instead of auto 'exit'",
                ),
                UsageRow(
                    "-a, --ssh-arg ARG",
                    "Forward extra args to nssh (repeatable)",
                ),
                UsageRow(
                    "--warmups N",
                    "Number of warmup runs ignored in summary",
                ),
                UsageRow(
                    "--samples N",
                    "Number of measured runs (default: 3)",
                ),
                UsageRow(
                    "--json-output PATH",
                    "Write benchmark summary JSON to PATH",
                ),
                UsageRow(
                    "--stage-budget KEY=MS",
                    "Enforce per-stage max (repeatable)",
                ),
                UsageRow(
                    "--total-budget MS",
                    "Enforce total max ms threshold",
                ),
                UsageRow(
                    "--budget-metric M",
                    "Metric for budgets: max, mean, median",
                ),
                UsageRow(
                    "--simple-only",
                    "Disable instrumentation; report totals only",
                ),
            ],
        ),
        UsageSection(
            "Analyze Options",
            rows=[
                UsageRow(
                    "--archive-dir DIR",
                    "Persist raw log + JSON summary",
                ),
                UsageRow(
                    "--label TEXT",
                    "Tag archived artifacts for later comparison",
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
