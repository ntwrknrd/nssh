"""Context add command for nssh ctx."""

from __future__ import annotations

from typing import Optional

from nssh.cli import click
from nssh.cli.common.banner import FAIL, NOOP, OK, banner
from nssh.cli.common.credentials import get_manager
from nssh.cli.common.prompt import (
    ask_text,
    prompt_password_with_confirmation,
    prompt_required,
)
from nssh.cli.common.ssh_include import ensure_conf_d_include
from nssh.core.env.paths import ssh_include_dir
from nssh.core.ui.console import get_console

console = get_console()


@click.command(short_help="Create a new context")
@click.argument("name", required=False, default=None)
@click.option("--domain", "-d", default=None, help="Domain suffix for auto-selection")
@click.option("--dry-run", is_flag=True, default=False, help="Preview only")
@click.pass_context
def add_command(
    ctx: click.Context, name: Optional[str], domain: Optional[str], dry_run: bool
) -> None:
    """Create new SSH context.

    Prompts for context name and SSH config file interactively.

    The --domain option enables automatic context selection when connecting
    to hosts matching the domain suffix (e.g., --domain example.com will
    auto-select this context for server.example.com).
    """
    cm = get_manager(ctx)

    # Ensure the include wiring exists so the context file will be loaded
    ensure_conf_d_include(
        create_if_missing=True,
        abort_on_decline=True,
        preview_title="SSH config change preview",
        dry_run=dry_run,
    )

    with banner("CREATE NEW SSH CONTEXT", OK) as set_outcome:
        _add_context(cm, name, domain, dry_run, set_outcome)


def _add_context(cm, name, domain, dry_run, set_outcome) -> None:
    """Internal implementation for adding a context."""
    # Interactive prompts
    if dry_run:
        final_name = name or "<dry-run-context>"
        git_include_file = "<dry-run-file>"
    else:
        final_name = prompt_required("Context name", name)

        # Check if context already exists
        existing = [c.get("name") for c in cm.list_contexts()]
        if final_name in existing:
            console.print(f"[red]Error: Context '{final_name}' already exists[/red]")
            set_outcome(FAIL)
            raise SystemExit(1)

        conf_d = ssh_include_dir()
        default_filename = f"{final_name}_hosts"
        git_include_file = ask_text(
            f"SSH config filename (in {conf_d}/)",
            default=default_filename,
            allow_empty=False,
        )

    # Prompt for optional credentials (before any mutations)
    username = ask_text(
        "Username (optional)",
        default="",
        allow_empty=True,
    )
    password = None
    if username:
        try:
            password = prompt_password_with_confirmation(
                "Password",
                allow_empty=False,
            )
        except ValueError as exc:
            console.print(f"[red]Error: {exc}[/red]")
            set_outcome(FAIL)
            raise SystemExit(1)

    if dry_run:
        console.print()
        console.print(
            f"[dim]Would create context '{final_name}' for file '{git_include_file}'[/dim]"
        )
        if domain:
            console.print(f"[dim]Domain auto-select: {domain}[/dim]")
        if username:
            console.print(f"[dim]Would add credential for '{username}'[/dim]")
        console.print("[dim]Dry-run: no changes written[/dim]")
        set_outcome(NOOP)
        return

    try:
        # Check if the SSH config file exists, create if needed
        conf_d = ssh_include_dir()
        config_file_path = conf_d / git_include_file
        file_created = False
        if not config_file_path.exists():
            conf_d.mkdir(parents=True, exist_ok=True)
            config_file_path.touch()
            file_created = True

        cm.create_context(final_name, git_include_file, domain=domain)

        # Add credential if provided
        if username and password:
            cm.add_context_credential(final_name, username, password, overwrite=True)

        # Show all results together
        console.print()
        if file_created:
            console.print(
                f"[green]+[/green] Created SSH config file: '{config_file_path}'"
            )
        console.print(f"[green]+[/green] Context '{final_name}' created")
        if domain:
            console.print(f"  Auto-selects for hosts matching: *.{domain}")
        if username:
            console.print(f"[green]+[/green] Credential added for '{username}'")
        # Default footer "Context created" will be used
    except Exception as exc:
        console.print(f"[red]Error: {exc}[/red]")
        set_outcome(FAIL)
        raise SystemExit(1)
