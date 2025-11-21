#!/usr/bin/env python3
"""nssh cred - Credential management CLI for nssh."""

from __future__ import annotations

from typing import Sequence

from nssh.cli import typer
from nssh.cli.common.app import run_cli
from nssh.cli.common.help import UsageRow, UsageSection, render_usage
from nssh.core.auth.credentials import CredentialManager

from .common import console
from .contexts import (
    add_context_command,
    list_contexts_command,
    rm_context_command,
    update_context_command,
)
from .hosts import (
    add_host_command,
    get_host_command,
    rm_host_command,
)
from .listing import list_hosts_command

APP_TITLE = "nssh cred"
APP_SUBTITLE = "Manage age-encrypted credentials for nssh"

app = typer.Typer(add_help_option=False, rich_markup_mode=None)

# Context subcommands
ctx_app = typer.Typer(add_help_option=False, rich_markup_mode=None)
ctx_app.command("add")(add_context_command)
ctx_app.command("update")(update_context_command)
ctx_app.command("list")(list_contexts_command)
ctx_app.command("rm")(rm_context_command)
app.add_typer(ctx_app, name="ctx", help="Manage credential contexts")

# Host commands (top-level)
app.command("get")(get_host_command)
app.command("add")(add_host_command)
app.command("list")(list_hosts_command)
app.command("rm")(rm_host_command)


def _usage_sections() -> list[UsageSection]:
    return [
        UsageSection(
            "Host Commands",
            rows=[
                UsageRow(
                    "nssh cred get HOST [--username USER]",
                    "Show decrypted password for host",
                ),
                UsageRow(
                    "nssh cred add HOST --username USER",
                    "Add credential to host",
                ),
                UsageRow(
                    "nssh cred list [HOST] [OPTIONS]",
                    "List all hosts or credentials for specific host",
                ),
                UsageRow(
                    "nssh cred rm HOST [--username USER | --all]",
                    "Remove credential(s) from host",
                ),
            ],
        ),
        UsageSection(
            "Context Commands",
            rows=[
                UsageRow(
                    "nssh cred ctx add NAME --file FILE",
                    "Create context for SSH config file",
                ),
                UsageRow(
                    "nssh cred ctx update NAME --username USER",
                    "Set fallback credential for context",
                ),
                UsageRow(
                    "nssh cred ctx list [OPTIONS]",
                    "List all contexts",
                ),
                UsageRow(
                    "nssh cred ctx rm NAME",
                    "Remove context",
                ),
            ],
        ),
        UsageSection(
            "Options",
            rows=[
                UsageRow(
                    "--search TERM, -s",
                    "Filter by keyword (repeatable)",
                ),
            ],
        ),
    ]


def print_usage() -> None:
    render_usage(APP_TITLE, APP_SUBTITLE, _usage_sections())


@app.callback()
def setup_context(ctx: typer.Context) -> None:
    """Initialize the shared credential manager."""

    try:
        ctx.obj = {"cm": CredentialManager()}
    except RuntimeError as exc:
        console.print(f"[red]Error: {exc}[/red]")
        raise typer.Exit(1)


def main(argv: Sequence[str] | None = None) -> None:
    """Main entry point."""

    run_cli(
        app,
        cli_name=APP_TITLE,
        usage_cb=print_usage,
        completion_prefix="CRED",
        argv=argv,
    )


if __name__ == "__main__":
    main()
