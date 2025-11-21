"""Listing helpers for nssh cred."""

from __future__ import annotations

from typing import List, Optional

from nssh.cli import typer
from nssh.cli.common import ui

from .common import complete_hostname, console, get_manager


def list_hosts_command(
    ctx: typer.Context,
    hostname: Optional[str] = typer.Argument(
        None,
        help="Optional hostname to list credentials for",
        autocompletion=complete_hostname,
    ),
    search: List[str] = typer.Option(
        [],
        "--search",
        "-s",
        help="Filter hosts by keyword (repeatable for AND logic)",
    ),
) -> None:
    """List all hosts with stored credentials, or credentials for a specific host."""

    cm = get_manager(ctx)

    # If hostname provided, list credentials for that specific host
    if hostname:
        credentials = cm.get_host_credentials(hostname)

        if not credentials:
            console.print(f"\n[yellow]No credentials found for '{hostname}'[/yellow]")
            return

        console.print(f"\n[bold cyan]Credentials for {hostname}:[/bold cyan]")

        cred_rows = [
            (str(i), cred["username"], "(default)" if i == 1 else "")
            for i, cred in enumerate(credentials, 1)
        ]

        ui.print_table(
            (("#", "dim"), ("Username", "green"), ("Note", "dim")), cred_rows
        )

        console.print()
        console.print(f"\n[dim]Total: {len(credentials)} credentials[/dim]")
        return

    # List all hosts
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

    host_rows: list[tuple[str, str]] = []
    for host in hosts:
        usernames = ", ".join([c["username"] for c in host["credentials"]])
        if host["credential_count"] > 1:
            usernames = f"{usernames} [dim]({host['credential_count']})[/dim]"
        host_rows.append((host["hostname"], usernames))

    ui.print_table((("Hostname", "cyan"), ("Credential", "green")), host_rows)

    console.print()
    console.print(f"\n[dim]Total: {len(hosts)} hosts[/dim]")
    console.print(
        "[dim]Usernames only; run 'nssh cred get HOSTNAME [--username USER]' for a password[/dim]"
    )
