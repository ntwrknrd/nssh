#!/usr/bin/env python3
"""Self CLI package wiring for installing nssh and optional shell helpers."""

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

from .cleanup import cleanup_command as uninstall_command
from .init import init_command
from .reinstall import reinstall_command
from .status import status_command

APP_TITLE = "nssh self"
APP_SUBTITLE = "Install nssh CLI + optional shell helpers"


@styled_group(
    invoke_without_command=True,
    styled_title=APP_TITLE,
    styled_subtitle=APP_SUBTITLE,
)
@click.pass_context
def app(ctx: click.Context) -> None:
    """Install nssh CLI + optional shell helpers."""
    ctx.ensure_object(dict)


app.add_command(init_command, name="init")
app.add_command(status_command, name="status")
app.add_command(reinstall_command, name="reinstall")
app.add_command(uninstall_command, name="uninstall")


def _usage_sections():
    return build_usage_sections(app, "nssh self")


def print_usage() -> None:
    """Print usage information"""
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
    """Entry point for python -m usage."""
    main()


if __name__ == "__main__":  # pragma: no cover
    cli_main()
