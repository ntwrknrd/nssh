#!/usr/bin/env python3
"""nssh ctx - Manage credential contexts (promoted from nssh cred ctx)."""

from __future__ import annotations

from typing import Optional, Sequence

from nssh.cli import click
from nssh.cli.common.app import run_cli
from nssh.cli.common.credentials import complete_context as complete_context
from nssh.cli.common.help import (
    build_options_panel,
    build_usage_sections,
    render_usage,
    styled_group,
)
from nssh.core.ui.console import get_console

from .add import add_command
from .edit import edit_command
from .get import get_command
from .list import list_command
from .remove import remove_command

console = get_console()
APP_TITLE = "nssh ctx"
APP_SUBTITLE = "Manage credential contexts"


@styled_group(
    invoke_without_command=True,
    styled_title=APP_TITLE,
    styled_subtitle=APP_SUBTITLE,
)
@click.option(
    "--select",
    "-s",
    default=None,
    help="Match by regex pattern",
)
@click.pass_context
def app(ctx: click.Context, select: Optional[str]) -> None:
    """Manage credential contexts."""
    ctx.ensure_object(dict)
    ctx.obj["cm"] = None

    # If --select provided without subcommand, forward to list
    if select and ctx.invoked_subcommand is None:
        ctx.invoke(list_command, select=select)
        raise SystemExit(0)


app.add_command(add_command, name="add")
app.add_command(edit_command, name="edit")
app.add_command(get_command, name="get")
app.add_command(list_command, name="list")
app.add_command(remove_command, name="rm")


def _usage_sections():
    return build_usage_sections(app, "nssh ctx")


def print_usage() -> None:
    render_usage(
        APP_TITLE,
        APP_SUBTITLE,
        _usage_sections(),
        options_panel=build_options_panel(app),
        show_banner=False,
    )


def main(argv: Sequence[str] | None = None) -> None:
    """Main entry point."""
    run_cli(
        app,
        cli_name=APP_TITLE,
        usage_cb=print_usage,
        argv=argv,
    )


if __name__ == "__main__":
    main()
