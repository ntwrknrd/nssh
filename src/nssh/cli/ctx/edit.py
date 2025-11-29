"""Context edit command for nssh ctx."""

from __future__ import annotations

from typing import Any, Dict, Optional

from nssh.cli import click
from nssh.cli.common.banner import FAIL, NOOP, OK, banner
from nssh.cli.common.credentials import complete_context, get_manager
from nssh.cli.common.prompt import (
    ask_text,
    prompt_password_with_confirmation,
    prompt_required,
)
from nssh.core.ui.console import get_console

console = get_console()


def _find_context_by_name(cm, name: str) -> Optional[Dict[str, Any]]:
    """Find a context entry by name from list_contexts()."""
    for entry in cm.list_contexts():
        if entry.get("name") == name:
            return entry
    return None


@click.command(short_help="Edit context credentials")
@click.argument("name", required=False, default=None, shell_complete=complete_context)
@click.option(
    "--domain", "-d", default=None, help="Set domain suffix for auto-selection"
)
@click.option("--ssh-config", "-s", default=None, help="Set SSH config include file")
@click.option("--username", "-u", default=None, help="Set fallback username")
@click.option("--password", "-p", default=None, help="Set fallback password")
@click.pass_context
def edit_command(
    ctx: click.Context,
    name: Optional[str],
    domain: Optional[str],
    ssh_config: Optional[str],
    username: Optional[str],
    password: Optional[str],
) -> None:
    """Edit context credentials and settings.

    Sets or replaces the fallback username, password, domain, and SSH config file.
    Use flags for non-interactive updates (empty string clears domain).
    """
    cm = get_manager(ctx)

    with banner("EDIT SSH CONTEXT", OK) as set_outcome:
        _edit_context(cm, name, domain, ssh_config, username, password, set_outcome)


def _edit_context(
    cm, name, domain, ssh_config, username, password, set_outcome
) -> None:
    """Internal implementation for editing a context."""
    # Determine if running non-interactively (any flag provided)
    non_interactive = (
        domain is not None
        or ssh_config is not None
        or username is not None
        or password is not None
    )

    final_name = prompt_required("Context name", name)

    # Check if context exists
    context_entry = _find_context_by_name(cm, final_name)
    if context_entry is None:
        console.print(f"[red]Error: Context '{final_name}' not found[/red]")
        console.print("\nCreate it with:")
        console.print(f"  [cyan]nssh ctx add {final_name}[/cyan]")
        set_outcome(FAIL)
        raise SystemExit(1)

    current_cred = context_entry.get("credential")
    current_domain = context_entry.get("domain", "")
    current_ssh_config = context_entry.get("git_include_file", "")

    # Non-interactive mode: only update what was specified
    if non_interactive:
        # Update domain if specified
        if domain is not None and domain != current_domain:
            try:
                cm.update_context_domain(final_name, domain or None)
                if domain:
                    console.print(f"[green]✓[/green] Domain set to '{domain}'")
                else:
                    console.print("[yellow]![/yellow] [dim]Domain cleared[/dim]")
            except Exception as exc:
                console.print(f"[red]Error updating domain: {exc}[/red]")
                set_outcome(FAIL)
                raise SystemExit(1)

        # Update SSH config file if specified
        if ssh_config is not None and ssh_config != current_ssh_config:
            if not ssh_config:
                console.print("[red]Error: SSH config file cannot be empty[/red]")
                set_outcome(FAIL)
                raise SystemExit(1)
            try:
                cm.update_context_include_file(final_name, ssh_config)
                console.print(f"SSH config set to '{ssh_config}'")
            except Exception as exc:
                console.print(f"[red]Error updating SSH config: {exc}[/red]")
                set_outcome(FAIL)
                raise SystemExit(1)

        # Update credential if username or password specified
        if username is not None or password is not None:
            final_username = username or (
                current_cred["username"] if current_cred else None
            )
            final_password = password

            if not final_username:
                console.print("[red]Error: Username required[/red]")
                set_outcome(FAIL)
                raise SystemExit(1)
            if not final_password:
                console.print(
                    "[red]Error: Password required when setting credentials[/red]"
                )
                set_outcome(FAIL)
                raise SystemExit(1)

            try:
                cm.add_context_credential(
                    final_name, final_username, final_password, overwrite=True
                )
                console.print(f"Credential set for '{final_username}'")
            except Exception as exc:
                console.print(f"[red]Error: {exc}[/red]")
                set_outcome(FAIL)
                raise SystemExit(1)

        console.print(f"[green]✓[/green] Context '{final_name}' updated")
        return

    # Interactive mode
    if current_cred:
        console.print(f"[dim]Current username: {current_cred['username']}[/dim]")
    else:
        console.print("[dim]No credential currently set[/dim]")

    if current_domain:
        console.print(f"[dim]Current domain: {current_domain}[/dim]")
    else:
        console.print("[dim]No domain currently set[/dim]")

    if current_ssh_config:
        console.print(f"[dim]Current SSH config: {current_ssh_config}[/dim]")

    console.print()

    changes_made = False

    # SSH config prompt
    final_ssh_config = ask_text(
        "SSH config file",
        default=current_ssh_config,
        allow_empty=True,
    )

    if final_ssh_config != current_ssh_config and final_ssh_config:
        try:
            cm.update_context_include_file(final_name, final_ssh_config)
            console.print(f"[green]✓[/green] SSH config set to '{final_ssh_config}'")
            changes_made = True
        except Exception as exc:
            console.print(f"[red]Error updating SSH config: {exc}[/red]")
            set_outcome(FAIL)
            raise SystemExit(1)

    # Domain prompt (- to clear)
    final_domain = ask_text(
        "Domain suffix (- to clear)",
        default=current_domain or "",
        allow_empty=True,
    )
    if final_domain == "-":
        final_domain = ""

    if final_domain != current_domain:
        try:
            cm.update_context_domain(final_name, final_domain or None)
            changes_made = True
            if final_domain:
                console.print(f"[green]✓[/green] Domain set to '{final_domain}'")
            else:
                console.print("[yellow]![/yellow] [dim]Domain cleared[/dim]")
        except Exception as exc:
            console.print(f"[red]Error updating domain: {exc}[/red]")
            set_outcome(FAIL)
            raise SystemExit(1)

    # Username prompt (- to skip)
    default_username = current_cred["username"] if current_cred else ""
    final_username = ask_text(
        "Username (- to skip)",
        default=default_username,
        allow_empty=True,
    )
    if final_username == "-":
        final_username = ""

    if not final_username:
        if not changes_made:
            set_outcome(NOOP)
        return

    # Password prompt - empty keeps existing if username unchanged
    try:
        final_password = prompt_password_with_confirmation(
            f"Password for {final_username}",
            allow_empty=True,
        )
    except ValueError as exc:
        console.print(f"[red]{exc}[/red]")
        set_outcome(FAIL)
        raise SystemExit(1)

    # Skip credential update if password empty and username unchanged
    if not final_password:
        if final_username == default_username and current_cred:
            console.print("[dim]Keeping existing credential[/dim]")
            if not changes_made:
                set_outcome(NOOP)
            return
        else:
            console.print("[red]Error: Password required for new username[/red]")
            set_outcome(FAIL)
            raise SystemExit(1)

    try:
        cm.add_context_credential(
            final_name, final_username, final_password, overwrite=True
        )
        console.print(f"[green]✓[/green] Credential updated for '{final_username}'")
    except Exception as exc:
        console.print(f"[red]Error: {exc}[/red]")
        set_outcome(FAIL)
        raise SystemExit(1)
