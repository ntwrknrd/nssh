"""Context management commands for nssh cred."""

from __future__ import annotations

from pathlib import Path
from typing import Dict, List, Optional

from nssh.cli import typer
from nssh.cli.common import ui
from nssh.cli.common.prompt import (
    prompt_password_with_confirmation,
    prompt_required,
)
from nssh.cli.common.ssh_include import ensure_conf_d_include
from nssh.cli.common.workflows import confirm_or_exit
from nssh.core.env.paths import ssh_include_dir
from nssh.core.ssh.config import SSHConfigParser

from .common import complete_context, console, get_manager


def _abbreviate_home(path: Path) -> str:
    """Return path string with home directory replaced by ``~`` when possible."""

    home = Path.home().resolve()
    try:
        candidate = path.expanduser().resolve()
    except OSError:
        candidate = path.expanduser()

    candidate_str = str(candidate)
    home_str = str(home)

    if candidate_str.startswith(home_str):
        remainder = candidate_str[len(home_str) :].lstrip("/")
        return f"~/{remainder}" if remainder else "~"

    return candidate_str


def _include_lookup() -> Dict[str, List[Path]]:
    """Return lookup of include file basenames to actual config paths."""

    parser = SSHConfigParser()
    lookup: Dict[str, List[Path]] = {}

    for include_path in parser.find_include_files():
        lookup.setdefault(include_path.name, []).append(include_path)

    return lookup


def _display_include_path(
    include_name: str, include_lookup: Dict[str, List[Path]]
) -> str:
    """Return preferred display string for an include file."""

    if not include_name:
        return "-"

    matches = include_lookup.get(include_name)
    if matches:
        display = _abbreviate_home(matches[0])
        extra = len(matches) - 1
        return f"{display} (+{extra} more)" if extra > 0 else display

    include_dir_candidate = ssh_include_dir() / include_name
    if include_dir_candidate.exists():
        return _abbreviate_home(include_dir_candidate)

    ssh_root_candidate = Path.home() / ".ssh" / include_name
    return _abbreviate_home(ssh_root_candidate)


def add_context_command(
    ctx: typer.Context,
    name: Optional[str] = typer.Argument(None, help="Context name"),
    file: Optional[str] = typer.Option(
        None, "--file", help="SSH config file name (in ~/.ssh/)"
    ),
    dry_run: bool = typer.Option(
        False,
        "--dry-run",
        help="Preview actions without writing (includes SSH config changes)",
    ),
) -> None:
    """Create a new credential context."""

    cm = get_manager(ctx)

    # Ensure the include wiring exists so the context file will be loaded
    ensure_conf_d_include(
        create_if_missing=True,
        abort_on_decline=True,
        preview_title="SSH config change preview",
        dry_run=dry_run,
    )

    ui.show_panel(
        "Create Context", "Create a new credential context for SSH config file"
    )

    # In dry-run, avoid prompting if values are missing; use placeholders instead
    if dry_run:
        final_name = name or "<dry-run-context>"
        git_include_file = file or "<dry-run-file>"
    else:
        final_name = prompt_required("Context name", name)
        git_include_file = prompt_required(
            "SSH config file (in ~/.ssh/)", file, "File name required"
        )

    if dry_run:
        console.print(
            f"[dim]Would create context '{final_name}' for file '{git_include_file}'[/dim]"
        )
        console.print("[dim]Dry-run: no changes written[/dim]")
        return

    try:
        cm.create_context(final_name, git_include_file)
        console.print("\n[bold green]✓ Success![/bold green]")
        console.print(f"Context '{final_name}' created for file '{git_include_file}'")
        console.print("\nNext: Add credentials with:")
        console.print(
            f"  [cyan]nssh cred ctx update {final_name} --username USER[/cyan]"
        )
    except Exception as exc:
        console.print(f"[red]Error: {exc}[/red]")
        raise typer.Exit(1)


def update_context_command(
    ctx: typer.Context,
    name: Optional[str] = typer.Argument(
        None, help="Context name", autocompletion=complete_context
    ),
    username: Optional[str] = typer.Option(None, "--username", help="Username"),
) -> None:
    """Set or replace the fallback credential for a context."""

    cm = get_manager(ctx)

    ui.show_panel("Update Context Credential", "Set username/password for context")

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
        cm.add_context_credential(final_name, final_username, password, overwrite=True)
        console.print("\n[bold green]✓ Success![/bold green]")
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
    include_lookup = _include_lookup()

    if not contexts:
        console.print("\n[yellow]No contexts configured[/yellow]")
        console.print("\nCreate one with:")
        console.print("  [cyan]nssh cred ctx add NAME --file NAME[/cyan]")
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
        include_display = _display_include_path(
            entry["git_include_file"], include_lookup
        )
        rows.append(
            (
                entry["name"],
                include_display,
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


def rm_context_command(
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
