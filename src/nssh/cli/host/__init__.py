#!/usr/bin/env python3
"""
nssh-manage-hosts - Manage SSH hosts in config files
"""

from __future__ import annotations

from typing import Optional, Sequence

from nssh.cli import typer
from nssh.cli.common.app import run_cli
from nssh.cli.common.help import UsageRow, UsageSection, render_usage
from nssh.core.ui.console import get_console
from nssh.core.ssh.config import SSHConfigParser
from .add import add_command
from .listing import list_hosts_command
from .remove import remove_command
from .update import update_command
from .context import (
    complete_context,
    get_parser as _get_parser,
)
from .sort import cmd_sort

console = get_console()
APP_TITLE = "nssh host"
APP_SUBTITLE = "Manage SSH hosts in configuration files"

app = typer.Typer(add_help_option=False, rich_markup_mode=None)

app.command("add")(add_command)
app.command("list")(list_hosts_command)
app.command("rm")(remove_command)
app.command("update")(update_command)


@app.command("sort")
def sort(
    ctx: typer.Context,
    context: Optional[str] = typer.Option(
        None,
        "--context",
        help="Context name (default: sort all Include files)",
        autocompletion=complete_context,
    ),
):
    """Sort SSH config files alphabetically"""
    parser = _get_parser(ctx)
    cmd_sort(parser, context_arg=context)


def _usage_sections() -> list[UsageSection]:
    return [
        UsageSection(
            "Commands",
            rows=[
                UsageRow(
                    "nssh host [bold]add[/bold] [FQDN] [OPTIONS]",
                    "Add SSH host to config file",
                ),
                UsageRow(
                    "nssh host [bold]rm[/bold] [HOSTNAME] [OPTIONS]",
                    "Remove host from config",
                ),
                UsageRow(
                    "nssh host [bold]list[/bold] [OPTIONS]",
                    "List all hosts from SSH configs",
                ),
                UsageRow(
                    "nssh host [bold]sort[/bold] [OPTIONS]",
                    "Sort hosts alphabetically in config files",
                ),
                UsageRow(
                    "nssh host [bold]update[/bold] [HOSTNAME]",
                    "Auto-detect auth requirements and compatibility",
                ),
            ],
        ),
        UsageSection(
            "Options",
            rows=[
                UsageRow(
                    "--auth password|key",
                    "Authentication type (default: password)",
                ),
                UsageRow(
                    "--context NAME",
                    "Specify context name (default: interactive selection)",
                ),
                UsageRow("--search TERM, -s", "Filter by keyword (repeatable)"),
                UsageRow("--force, -f", "Skip all prompts and use defaults"),
            ],
        ),
    ]


def print_usage():
    render_usage(APP_TITLE, APP_SUBTITLE, _usage_sections())


@app.callback()
def setup_context(ctx: typer.Context):
    """Initialize shared managers"""
    ctx.obj = {"cm": None, "parser": SSHConfigParser()}


def main(argv: Sequence[str] | None = None):
    """Main entry point"""
    run_cli(
        app,
        cli_name=APP_TITLE,
        usage_cb=print_usage,
        completion_prefix="HOST",
        argv=argv,
    )


if __name__ == "__main__":
    main()
