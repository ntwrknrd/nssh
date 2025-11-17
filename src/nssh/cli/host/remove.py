from __future__ import annotations

from typing import Optional

from nssh.cli import Syntax, typer
from nssh.cli.common.prompt import confirm, prompt_required
from nssh.cli.common.ui import show_panel
from nssh.cli.common.workflows import confirm_or_exit

from .context import complete_hostname, console, get_manager, get_parser


def remove_command(
    ctx: typer.Context,
    hostname: Optional[str] = typer.Argument(
        None, help="Hostname to remove", autocompletion=complete_hostname
    ),
    force: bool = typer.Option(
        False, "-f", "--force", help="Skip confirmation prompts and delete credentials"
    ),
) -> None:
    """Remove SSH host from configuration"""
    parser = get_parser(ctx)
    cm = get_manager(ctx)

    show_panel("Remove SSH Host", "Remove host from SSH configuration")

    # Get hostname
    final_hostname = prompt_required("Enter hostname to remove", hostname)

    # Find which file contains this host
    console.print(f"\n[bold]Searching for host '{final_hostname}'...[/bold]")

    result = parser.find_host_in_files(final_hostname)

    if not result:
        console.print(
            f"[red]Error: Host '{final_hostname}' not found in any Include file[/red]"
        )
        raise typer.Exit(1)

    target_file, host_lines = result
    console.print(f"[dim]Found in: {target_file}[/dim]")

    # Show host configuration preview
    console.print("\n[bold]Host Configuration:[/bold]")

    config_text = "".join(host_lines)
    syntax = Syntax(config_text, "ssh-config", theme="monokai", line_numbers=False)
    show_panel(f"Will Remove: {final_hostname}", syntax, style="red")

    # Check if credentials exist
    has_credentials = cm.get_host_credentials(final_hostname) is not None

    if has_credentials:
        console.print(
            f"\n[yellow]Note: Host '{final_hostname}' has stored credentials[/yellow]"
        )

    # Confirm deletion (skip if force flag is used)
    if not force:
        console.print("\n[bold]Confirm Removal[/bold]")

        confirm_or_exit(
            f"[red]Remove host '{final_hostname}' from {target_file.name}?[/red]",
            default=False,
        )

    # Ask about credential deletion if exists (auto-delete if force flag is used)
    delete_credentials = False
    if has_credentials:
        if force:
            delete_credentials = True
        else:
            delete_credentials = confirm(
                f"[yellow]Also delete stored credentials for '{final_hostname}'?[/yellow]",
                default=True,
            )

    # Remove host from config file
    try:
        # Create backup
        backup_path = parser.create_backup(target_file)
        console.print(f"[dim]Backup created: {backup_path}[/dim]")

        # Parse and remove host
        header_lines, hosts = parser.parse_ssh_config(target_file)
        hosts = [(h, lines) for h, lines in hosts if h != final_hostname]

        # Write back
        parser.write_ssh_config(target_file, header_lines, hosts)

        # Rebuild host index for fast lookups
        parser.rebuild_index()

        console.print(
            f"[green]✓ Host '{final_hostname}' removed from {target_file.name}[/green]"
        )

        # Delete credentials if requested
        if delete_credentials:
            cm.delete_host_all_credentials(final_hostname)
            console.print(
                f"[green]✓ Credentials deleted for '{final_hostname}'[/green]"
            )

        console.print("\n[bold green]✓ Success![/bold green]")

    except Exception as e:
        console.print(f"[red]Error: {e}[/red]")
        import traceback

        traceback.print_exc()
        raise typer.Exit(1)
