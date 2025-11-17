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
    complete_config_file,
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
    file: Optional[str] = typer.Option(
        None,
        "--file",
        help="SSH config file name in ~/.ssh/",
        autocompletion=complete_config_file,
    ),
    all: bool = typer.Option(False, "--all", help="Sort all Include files"),
):
    """Sort SSH config files alphabetically"""
    parser = _get_parser(ctx)
    cmd_sort(parser, file=file, all=all)


def _usage_sections() -> list[UsageSection]:
    return [
        UsageSection(
            "Commands",
            rows=[
                UsageRow(
                    "nssh host add [FQDN] [OPTIONS]",
                    "Add new SSH host to config file (interactive or with options)",
                    examples=["nssh host add server.example.com --password"],
                ),
                UsageRow(
                    "nssh host rm [HOSTNAME]",
                    "Remove host from config and optionally delete stored credentials",
                ),
                UsageRow(
                    "nssh host list [SEARCH]",
                    "List all hosts from SSH configs (optionally filter by keyword)",
                ),
                UsageRow(
                    "nssh host sort [OPTIONS]",
                    "Sort hosts alphabetically and remove duplicates",
                ),
                UsageRow(
                    "nssh host update [HOSTNAME] [OPTIONS]",
                    "Update authentication and/or auto-fix compatibility issues",
                    examples=["nssh host update myhost --auth password --compat"],
                ),
            ],
        ),
        UsageSection(
            "Add Options",
            rows=[
                UsageRow(
                    "--hostname NAME",
                    "Custom SSH alias (default: first part of FQDN)",
                ),
                UsageRow(
                    "--user USERNAME",
                    "SSH username (default: context, $NSSH_DEFAULT_USER, or $USER)",
                ),
                UsageRow("--port PORT", "SSH port (default: 22)"),
                UsageRow(
                    "--password / --key",
                    "Choose password auth (managed via nssh cred) or key-only auth",
                ),
                UsageRow(
                    "--file NAME",
                    "Target SSH config file in ~/.ssh/ (skips interactive selection)",
                ),
                UsageRow("--no-test", "Skip connection test after adding host"),
                UsageRow(
                    "--skip-password-prompt",
                    "Skip password prompt and use context fallback credentials",
                ),
            ],
        ),
        UsageSection(
            "Sort Options",
            rows=[
                UsageRow("--file NAME", "Sort specific file in ~/.ssh/"),
                UsageRow("--all", "Sort all Include files found in ~/.ssh/config"),
            ],
        ),
        UsageSection(
            "Update Options",
            rows=[
                UsageRow(
                    "--auth TYPE",
                    "Set authentication: password, keyboard-interactive, publickey",
                ),
                UsageRow(
                    "--compat",
                    "Auto-detect and fix SSH compatibility issues",
                ),
                UsageRow(
                    "--max-iterations N",
                    "Maximum attempts for compatibility fixing (default: 5)",
                ),
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
