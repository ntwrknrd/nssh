"""Context remove command for nssh ctx."""

from __future__ import annotations

from typing import Any, Dict, Optional

from nssh.cli import click
from nssh.cli.common.banner import ABORT, FAIL, OK, banner
from nssh.cli.common.credentials import complete_context, get_manager
from nssh.cli.common.prompt import confirm, prompt_required
from nssh.core.ssh.config import SSHConfigParser
from nssh.core.ui.console import get_console

console = get_console()


def _find_context_by_name(cm, name: str) -> Optional[Dict[str, Any]]:
    """Find a context entry by name from list_contexts()."""
    for entry in cm.list_contexts():
        if entry.get("name") == name:
            return entry
    return None


@click.command(short_help="Delete a context")
@click.argument("name", required=False, default=None, shell_complete=complete_context)
@click.pass_context
def remove_command(ctx: click.Context, name: Optional[str]) -> None:
    """Delete a credential context.

    Shows dependent hosts and warns before removal.
    """
    cm = get_manager(ctx)

    with banner("DELETE SSH CONTEXT", OK) as set_outcome:
        _remove_context(cm, name, set_outcome)


def _remove_context(cm, name, set_outcome) -> None:
    """Internal implementation for removing a context."""
    final_name = prompt_required("Context name to delete", name)

    # Check if context exists and get details
    context_entry = _find_context_by_name(cm, final_name)
    if context_entry is None:
        console.print(f"[red]Error: Context '{final_name}' not found[/red]")
        set_outcome(FAIL)
        raise SystemExit(1)

    # Find hosts using this context and the config file path
    parser = SSHConfigParser()
    git_include_file = context_entry["git_include_file"]
    dependent_hosts = []
    config_file_path = None

    for include_path in parser.find_include_files():
        if include_path.name == git_include_file:
            config_file_path = include_path
            _, hosts = parser.parse_ssh_config(include_path)
            dependent_hosts = [hostname for hostname, _ in hosts]
            break

    # Show warning about dependent hosts
    if dependent_hosts:
        console.print(
            f"\n[yellow]![/yellow] Context '{final_name}' is used by {len(dependent_hosts)} host(s)"
        )
        for hostname in dependent_hosts[:10]:  # Show first 10
            console.print(f"  [dim]- {hostname}[/dim]")
        if len(dependent_hosts) > 10:
            console.print(f"  [dim]... and {len(dependent_hosts) - 10} more[/dim]")

    # Confirm deletion
    if not confirm(f"Delete context '{final_name}'?", default=False):
        console.print("[dim]Cancelled[/dim]")
        set_outcome(ABORT)
        raise SystemExit(0)

    # Ask about deleting related SSH config file
    delete_config_file = False
    if config_file_path and config_file_path.exists():
        delete_config_file = confirm(
            f"Delete related file: '{config_file_path}'?",
            default=False,
        )

    try:
        success = cm.delete_context(final_name)
        if success:
            console.print(f"\n[green]\u2713[/green] Context '{final_name}' deleted")

            # Delete config file if requested
            if delete_config_file and config_file_path:
                config_file_path.unlink()
                console.print(
                    f"[green]\u2713[/green] SSH config file '{config_file_path.name}' deleted"
                )
            # Default footer "Context deleted" will be used
        else:
            console.print(f"[red]Context '{final_name}' not found[/red]")
            set_outcome(FAIL)
            raise SystemExit(1)
    except Exception as exc:
        console.print(f"[red]Error: {exc}[/red]")
        set_outcome(FAIL)
        raise SystemExit(1)
