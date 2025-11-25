"""nssh benchmark CLI package wiring."""

from __future__ import annotations

from typing import Sequence

from nssh.cli import typer
from nssh.cli.common.app import run_cli
from nssh.cli.common.help import UsageRow, UsageSection, render_usage

from .capture import capture_command as capture_ssh_command
from .capture_scp import capture_scp_command
from .common import APP_TITLE

APP_SUBTITLE = "Measure nssh overhead on your system"

app = typer.Typer(add_help_option=False, rich_markup_mode=None)

# Register SSH and SCP benchmark commands
app.command("ssh")(capture_ssh_command)
app.command("scp")(capture_scp_command)


def _usage_sections() -> list[UsageSection]:
    return [
        UsageSection(
            "Commands",
            rows=[
                UsageRow(
                    "nssh benchmark ssh HOST",
                    "Benchmark SSH connection overhead",
                ),
                UsageRow(
                    "nssh benchmark scp HOST",
                    "Benchmark SCP file transfer performance",
                ),
            ],
        ),
        UsageSection(
            "Common Options",
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
            ],
        ),
        UsageSection(
            "Command-Specific Options",
            rows=[
                UsageRow(
                    "ssh: --no-record",
                    "Force disable session recording",
                ),
                UsageRow(
                    "scp: --size KB",
                    "Test file size in KB (default: 100)",
                ),
            ],
        ),
        UsageSection(
            "Timing Stages",
            rows=[
                UsageRow("cli-startup", "CLI init (imports, parsing)"),
                UsageRow("config-parse", "SSH config parsing"),
                UsageRow("host-selection", "Host matching and resolution"),
                UsageRow("credential-vault", "Credential decryption"),
                UsageRow("ssh-connection", "Actual SSH connection time"),
                UsageRow("scp-transfer", "File transfer with network I/O"),
            ],
        ),
        UsageSection(
            "Diagnostic Metrics",
            rows=[
                UsageRow("asciinema-overhead", "Recording wrapper overhead"),
                UsageRow("unaccounted-time", "Gap: total - sum(stages)"),
            ],
        ),
        UsageSection(
            "Performance Profiling",
            rows=[
                UsageRow(
                    "python -X importtime -m nssh.cli.main HOST",
                    "Profile Python import overhead",
                ),
                UsageRow(
                    "NSSH_DEBUG=1 nssh cp HOST:file ./",
                    "Enable timing instrumentation",
                ),
                UsageRow(
                    "time nssh --help",
                    "Measure cold-start overhead",
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
