"""nssh benchmark CLI package wiring."""

from __future__ import annotations

from typing import Sequence

from nssh.cli import click
from nssh.cli.common.app import run_cli
from nssh.cli.common.help import (
    build_options_panel,
    build_usage_sections,
    render_usage,
    styled_group,
)

from .capture import capture_command as capture_ssh_command
from .capture_scp import capture_scp_command
from .common import APP_TITLE

APP_SUBTITLE = "Measure nssh overhead on your system"


@styled_group(
    invoke_without_command=True,
    styled_title=APP_TITLE,
    styled_subtitle=APP_SUBTITLE,
)
@click.pass_context
def app(ctx: click.Context) -> None:
    """Benchmark CLI group."""
    pass


# Register SSH and SCP benchmark commands
app.add_command(capture_ssh_command, name="ssh")
app.add_command(capture_scp_command, name="scp")


def _usage_sections():
    return build_usage_sections(app, "nssh benchmark")


def print_usage() -> None:
    """Display usage instructions consistent with other CLIs."""
    render_usage(
        APP_TITLE,
        APP_SUBTITLE,
        _usage_sections(),
        options_panel=build_options_panel(app),
        show_banner=False,
    )


def main(argv: Sequence[str] | None = None) -> None:
    """Entry point mirroring help UX used by other CLIs."""
    run_cli(
        app,
        cli_name=APP_TITLE,
        usage_cb=print_usage,
        argv=argv,
    )


def cli_main() -> None:
    """Entry point for python -m usage."""
    main()


if __name__ == "__main__":
    cli_main()
