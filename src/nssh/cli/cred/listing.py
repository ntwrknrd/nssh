"""Listing helpers for nssh cred."""

from __future__ import annotations

from typing import List

from nssh.cli import typer
from nssh.cli.common import ui

from .common import console, get_manager


def list_hosts_command(
    ctx: typer.Context,
    search: List[str] = typer.Option(
        [],
        "--search",
        "-s",
        help="Filter hosts by keyword (repeatable for AND logic)",
    ),
) -> None:
    """List all hosts with stored credentials."""

    cm = get_manager(ctx)

    ui.show_panel("Host List", "All hosts with stored credentials")

    hosts = cm.list_hosts()

    if not hosts:
        console.print("\n[yellow]No host credentials stored[/yellow]")
        console.print("\nAdd one with:")
        console.print("  [cyan]nssh cred add HOSTNAME --username USER[/cyan]")
        return

    if search:
        for term in search:
            term_lower = term.lower()
            hosts = [
                host
                for host in hosts
                if term_lower in host["hostname"].lower()
                or any(
                    term_lower in credential["username"].lower()
                    for credential in host["credentials"]
                )
            ]

        if not hosts:
            console.print(
                f"\n[yellow]No hosts found matching all terms: {' '.join(search)}[/yellow]"
            )
            return

    rows = []
    for host in hosts:
        usernames = ", ".join([c["username"] for c in host["credentials"]])
        if host["credential_count"] > 1:
            usernames = f"{usernames} [dim]({host['credential_count']})[/dim]"
        rows.append((host["hostname"], usernames))

    ui.print_table((("Hostname", "cyan"), ("Credential", "green")), rows)

    console.print()
    console.print(f"\n[dim]Total: {len(hosts)} hosts[/dim]")
    console.print(
        "[dim]Usernames only; run 'nssh cred show HOSTNAME [--username USER]' for a password[/dim]"
    )
