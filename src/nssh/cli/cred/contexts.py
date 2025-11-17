"""Context management commands for nssh cred."""

from __future__ import annotations

from typing import List, Optional

from nssh.cli import typer
from nssh.cli.common import ui
from nssh.cli.common.prompt import (
    prompt_password_with_confirmation,
    prompt_required,
)
from nssh.cli.common.workflows import confirm_or_exit

from .common import complete_context, console, get_manager


def create_context_command(
    ctx: typer.Context,
    name: Optional[str] = typer.Argument(None, help="Context name"),
    file: Optional[str] = typer.Option(
        None, "--file", help="SSH config file name (in ~/.ssh/)"
    ),
) -> None:
    """Create a new credential context."""

    cm = get_manager(ctx)

    ui.show_panel(
        "Create Context", "Create a new credential context for SSH config file"
    )

    final_name = prompt_required("Context name", name)
    git_include_file = prompt_required(
        "SSH config file (in ~/.ssh/)", file, "File name required"
    )

    try:
        cm.create_context(final_name, git_include_file)
        console.print("\n[bold green]✓ Success![/bold green]")
        console.print(f"Context '{final_name}' created for file '{git_include_file}'")
        console.print("\nNext: Add credentials with:")
        console.print(
            f"  [cyan]nssh cred add-context-cred {final_name} --username USER[/cyan]"
        )
    except Exception as exc:
        console.print(f"[red]Error: {exc}[/red]")
        raise typer.Exit(1)


def add_context_cred_command(
    ctx: typer.Context,
    name: Optional[str] = typer.Argument(
        None, help="Context name", autocompletion=complete_context
    ),
    username: Optional[str] = typer.Option(None, "--username", help="Username"),
    overwrite: bool = typer.Option(
        False,
        "--overwrite",
        help="Replace the existing fallback credential if present",
    ),
) -> None:
    """Set or replace the fallback credential for a context."""

    cm = get_manager(ctx)

    ui.show_panel("Add Context Credential", "Add username/password to context")

    final_name = prompt_required("Context name", name)
    final_username = prompt_required("Username", username)

    try:
        password = prompt_password_with_confirmation(
            f"[cyan]Enter password for {final_username} in context '{final_name}'[/cyan]"
        )
    except ValueError as exc:
        console.print(f"[red]{exc}[/red]")
        raise typer.Exit(1)

    try:
        cm.add_context_credential(
            final_name, final_username, password, overwrite=overwrite
        )
        console.print("\n[bold green]✓ Success![/bold green]")
        if overwrite:
            console.print(
                f"Fallback credential for context '{final_name}' was replaced"
            )
        else:
            console.print(f"Fallback credential set for context '{final_name}'")
    except Exception as exc:
        console.print(f"[red]Error: {exc}[/red]")
        raise typer.Exit(1)


def list_contexts_command(
    ctx: typer.Context,
    search: List[str] = typer.Option(
        [],
        "--search",
        "-s",
        help="Filter contexts by keyword (repeatable for AND logic)",
    ),
) -> None:
    """List all credential contexts."""

    cm = get_manager(ctx)

    ui.show_panel("Context List", "All credential contexts")

    contexts = cm.list_contexts()

    if not contexts:
        console.print("\n[yellow]No contexts configured[/yellow]")
        console.print("\nCreate one with:")
        console.print("  [cyan]nssh cred create-context NAME --file NAME[/cyan]")
        return

    if search:
        for term in search:
            term_lower = term.lower()
            contexts = [
                entry
                for entry in contexts
                if term_lower in entry["name"].lower()
                or term_lower in entry["git_include_file"].lower()
                or (
                    entry["credential"]
                    and term_lower in entry["credential"]["username"].lower()
                )
            ]

    if not contexts:
        console.print(
            f"\n[yellow]No contexts found matching all terms: {' '.join(search)}[/yellow]"
        )
        return

    rows = []
    for entry in contexts:
        credential = entry["credential"]
        rows.append(
            (
                entry["name"],
                entry["git_include_file"],
                credential["username"] if credential else "-",
            )
        )

    ui.print_table(
        (
            ("Context", "cyan"),
            ("SSH Config File", "green"),
            ("Fallback Credential", "yellow"),
        ),
        rows,
    )


def delete_context_command(
    ctx: typer.Context,
    name: Optional[str] = typer.Argument(
        None, help="Context name", autocompletion=complete_context
    ),
) -> None:
    """Delete a credential context."""

    cm = get_manager(ctx)

    final_name = prompt_required("Context name to delete", name)

    console.print(f"\n[yellow]Delete context '{final_name}'?[/yellow]")
    confirm_or_exit("This cannot be undone", default=False)

    try:
        success = cm.delete_context(final_name)
        if success:
            console.print("\n[bold green]✓ Success![/bold green]")
            console.print(f"Context '{final_name}' deleted")
        else:
            console.print(f"[red]Context '{final_name}' not found[/red]")
            raise typer.Exit(1)
    except Exception as exc:
        console.print(f"[red]Error: {exc}[/red]")
        raise typer.Exit(1)
