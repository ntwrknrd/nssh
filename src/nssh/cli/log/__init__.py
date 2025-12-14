"""nssh log CLI package wiring."""

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

from .delete import delete_command
from .export import export_command
from .listing import list_sessions
from .play import play_command
from .upload import upload_command

APP_TITLE = "nssh log"
APP_SUBTITLE = "Inspect, play, and manage nssh recording files"


@styled_group(
    invoke_without_command=True,
    styled_title=APP_TITLE,
    styled_subtitle=APP_SUBTITLE,
)
@click.pass_context
def app(ctx: click.Context) -> None:
    """Log CLI group."""
    pass


app.add_command(list_sessions, name="list")
app.add_command(play_command, name="play")
app.add_command(delete_command, name="delete")
app.add_command(upload_command, name="upload")
app.add_command(export_command, name="export")


def _usage_sections():
    return build_usage_sections(app, "nssh log")


def print_usage() -> None:
    render_usage(
        APP_TITLE,
        APP_SUBTITLE,
        _usage_sections(),
        options_panel=build_options_panel(app),
        show_banner=False,
    )


def main(argv: Sequence[str] | None = None) -> None:
    run_cli(
        app,
        cli_name=APP_TITLE,
        usage_cb=print_usage,
        argv=argv,
    )


def cli_main() -> None:
    """Entry point for running as a module (python -m nssh.cli.log)."""
    main()


if __name__ == "__main__":
    cli_main()
