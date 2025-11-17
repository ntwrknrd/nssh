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
    add_context_cred_command,
    create_context_command,
    delete_context_command,
    list_contexts_command,
)
from .hosts import (
    add_host_command,
    delete_host_command,
    list_host_command,
    show_host_command,
)
from .listing import list_hosts_command

APP_TITLE = "nssh cred"
APP_SUBTITLE = "Manage age-encrypted credentials for nssh"

app = typer.Typer(add_help_option=False, rich_markup_mode=None)

app.command("create-context")(create_context_command)
app.command("add-context-cred")(add_context_cred_command)
app.command("list-contexts")(list_contexts_command)
app.command("delete-context")(delete_context_command)

app.command("add")(add_host_command)
app.command("list-host")(list_host_command)
app.command("delete")(delete_host_command)
app.command("show")(show_host_command)

app.command("list")(list_hosts_command)


def _usage_sections() -> list[UsageSection]:
    return [
        UsageSection(
            "Context Management",
            rows=[
                UsageRow(
                    "nssh cred create-context NAME --file NAME",
                    "Create credential context for SSH config file in ~/.ssh/ (scope for fallback credentials)",
                ),
                UsageRow(
                    "nssh cred add-context-cred NAME --username USER",
                    "Set or replace fallback credential for a context (use --overwrite to replace)",
                ),
                UsageRow(
                    "nssh cred list-contexts",
                    "Show all contexts, their SSH config files, and fallback credential (if any)",
                ),
                UsageRow(
                    "nssh cred delete-context NAME",
                    "Remove context and its fallback credential",
                ),
            ],
        ),
        UsageSection(
            "Host Credentials",
            rows=[
                UsageRow(
                    "nssh cred add HOSTNAME --username USER",
                    "Add host-specific credential (overrides context fallback)",
                ),
                UsageRow(
                    "nssh cred list-host HOSTNAME",
                    "Show all credentials for a specific host",
                ),
                UsageRow(
                    "nssh cred delete HOSTNAME --username USER",
                    "Remove specific credential from host",
                ),
                UsageRow(
                    "nssh cred delete HOSTNAME --all",
                    "Remove all credentials for host",
                ),
                UsageRow(
                    "nssh cred show HOSTNAME [--username USER]",
                    "Print decrypted password to stdout (use trusted terminals only)",
                ),
            ],
        ),
        UsageSection(
            "Listing",
            rows=[
                UsageRow(
                    "nssh cred list [SEARCH]",
                    "Show host credentials (usernames only); SEARCH filters hosts/usernames. Use 'nssh cred show' for passwords.",
                )
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
