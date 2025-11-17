"""Host credential management commands for nssh cred."""

from __future__ import annotations

from typing import Optional

from nssh.cli import typer
from nssh.cli.common import ui
from nssh.cli.common.prompt import (
    prompt_password_with_confirmation,
    prompt_required,
)
from nssh.cli.common.workflows import confirm_or_exit

from .common import complete_hostname, console, get_manager


def add_host_command(
    ctx: typer.Context,
    hostname: Optional[str] = typer.Argument(
        None, help="Hostname", autocompletion=complete_hostname
    ),
    username: Optional[str] = typer.Option(None, "--username", help="Username"),
) -> None:
    """Add a credential entry to a host."""

    cm = get_manager(ctx)

    ui.show_panel("Add Host Credential", "Add username/password for specific host")

    final_hostname = prompt_required("Hostname", hostname)
    final_username = prompt_required("Username", username)

    try:
        password = prompt_password_with_confirmation(
            f"[cyan]Enter password for {final_username}@{final_hostname}[/cyan]"
        )
    except ValueError as exc:
        console.print(f"[red]{exc}[/red]")
        raise typer.Exit(1)

    try:
        cm.add_host_credential(final_hostname, final_username, password)
        console.print("\n[bold green]✓ Success![/bold green]")
        console.print(f"Credential added for '{final_hostname}'")
    except Exception as exc:
        console.print(f"[red]Error: {exc}[/red]")
        raise typer.Exit(1)


def list_host_command(
    ctx: typer.Context,
    hostname: Optional[str] = typer.Argument(
        None, help="Hostname", autocompletion=complete_hostname
    ),
) -> None:
    """List stored credentials for a host."""

    cm = get_manager(ctx)

    final_hostname = prompt_required("Hostname", hostname)

    credentials = cm.get_host_credentials(final_hostname)

    if not credentials:
        console.print(f"\n[yellow]No credentials found for '{final_hostname}'[/yellow]")
        return

    console.print(f"\n[bold cyan]Credentials for {final_hostname}:[/bold cyan]")

    rows = [
        (str(i), cred["username"], "(default)" if i == 1 else "")
        for i, cred in enumerate(credentials, 1)
    ]

    ui.print_table((("#", "dim"), ("Username", "green"), ("Note", "dim")), rows)

    console.print()
    console.print(f"\n[dim]Total: {len(credentials)} credentials[/dim]")


def delete_host_command(
    ctx: typer.Context,
    hostname: Optional[str] = typer.Argument(
        None, help="Hostname", autocompletion=complete_hostname
    ),
    username: Optional[str] = typer.Option(
        None, "--username", help="Username to delete"
    ),
    all: bool = typer.Option(False, "--all", help="Delete all credentials for host"),
) -> None:
    """Delete credential(s) attached to a host."""

    cm = get_manager(ctx)

    final_hostname = prompt_required("Hostname", hostname)

    if all:
        console.print(
            f"\n[yellow]Delete ALL credentials for '{final_hostname}'?[/yellow]"
        )
        confirm_or_exit("This cannot be undone", default=False)

        try:
            success = cm.delete_host_all_credentials(final_hostname)
            if success:
                console.print("\n[bold green]✓ Success![/bold green]")
                console.print(f"All credentials deleted for '{final_hostname}'")
            else:
                console.print(f"[red]No credentials found for '{final_hostname}'[/red]")
                raise typer.Exit(1)
        except Exception as exc:
            console.print(f"[red]Error: {exc}[/red]")
            raise typer.Exit(1)
        return

    final_username = prompt_required("Username", username)

    console.print(
        f"\n[yellow]Delete credential for {final_username}@{final_hostname}?[/yellow]"
    )
    confirm_or_exit("This cannot be undone", default=False)

    try:
        success = cm.delete_host_credential(final_hostname, final_username)
        if success:
            console.print("\n[bold green]✓ Success![/bold green]")
            console.print(f"Credential for {final_username}@{final_hostname} deleted")
        else:
            console.print(
                f"[red]No credential found for {final_username}@{final_hostname}[/red]"
            )
            raise typer.Exit(1)
    except Exception as exc:
        console.print(f"[red]Error: {exc}[/red]")
        raise typer.Exit(1)


def show_host_command(
    ctx: typer.Context,
    hostname: Optional[str] = typer.Argument(
        None, help="Hostname", autocompletion=complete_hostname
    ),
    username: Optional[str] = typer.Option(
        None, "--username", help="Specific username"
    ),
) -> None:
    """Print the decrypted password for a host/username pair."""

    cm = get_manager(ctx)

    final_hostname = prompt_required("Hostname", hostname)

    credentials = cm.get_host_credentials(final_hostname)

    if not credentials:
        console.print(f"[red]No credentials found for '{final_hostname}'[/red]")
        raise typer.Exit(1)

    if username:
        credential = next(
            (entry for entry in credentials if entry["username"] == username), None
        )
        if not credential:
            console.print(
                f"[red]No credential found for {username}@{final_hostname}[/red]"
            )
            raise typer.Exit(1)

        ui.show_panel(
            f"Credential for {username}@{final_hostname}",
            (
                f"[bold]Host:[/bold] {final_hostname}\n"
                f"[bold]Username:[/bold] {username}\n\n"
                f"[bold]Password:[/bold]\n[cyan]{credential['password']}[/cyan]"
            ),
            style="green",
        )
        return

    credential = credentials[0]
    ui.show_panel(
        f"Credential for {final_hostname}",
        (
            f"[bold]Host:[/bold] {final_hostname}\n"
            f"[bold]Username:[/bold] {credential['username']}\n\n"
            f"[bold]Password:[/bold]\n[cyan]{credential['password']}[/cyan]"
        ),
        style="green",
    )
