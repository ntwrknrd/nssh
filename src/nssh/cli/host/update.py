"""Update command wiring for nssh host."""

from __future__ import annotations

from typing import List, Optional

from typer import Exit as TyperExit

from nssh.cli import Syntax, typer
from nssh.cli.common import ui
from nssh.cli.common.selectors import select_via_fzf
from nssh.cli.common.workflows import confirm_or_exit
from nssh.core.ssh.mutations import apply_host_update, render_host_update
from nssh.core.ssh.fixer import (
    AUTH_CONFIGS,
    COMPAT_CONFIGS,
    detect_auth_type,
)

from .compat import apply_and_display_compat_fixes
from .context import complete_hostname, console, get_parser


def cmd_update(
    parser,
    *,
    hostname: Optional[str] = None,
    auth_type: Optional[str] = None,
    compat_types: Optional[List[str]] = None,
    skip_prompts: bool = False,
) -> None:
    """Update SSH host configuration for authentication/compat tweaks."""

    if not auth_type and not compat_types:
        console.print("[red]Error: Must specify --auth or --compat option[/red]")
        console.print("[dim]Use 'nssh host update --help' for usage information[/dim]")
        raise TyperExit(1)

    operations: List[str] = []
    if auth_type:
        operations.append("authentication")
    if compat_types:
        operations.append("compatibility")
    op_desc = " and ".join(operations)

    if not skip_prompts:
        ui.show_panel(
            "Update SSH Host Configuration",
            f"Modify {op_desc} settings",
            style="cyan",
        )

    if not hostname:
        include_files = parser.find_include_files()
        all_hosts: List[str] = []

        for file_path in include_files:
            _, hosts = parser.parse_ssh_config(file_path)
            for host_name, _ in hosts:
                all_hosts.append(host_name)

        if not all_hosts:
            console.print("[red]Error: No hosts found in Include files[/red]")
            raise TyperExit(1)

        try:
            hostname = select_via_fzf(sorted(all_hosts), "Select host to update:")
        except TyperExit as exc:
            if exc.exit_code == 1:
                console.print("[dim]Or provide hostname as argument[/dim]")
            else:
                console.print("[yellow]Cancelled[/yellow]")
            raise

    if not skip_prompts:
        console.print(f"\n[bold]Step 1: Finding host '{hostname}'...[/bold]")

    result = parser.find_host_in_files(hostname)

    if not result:
        console.print(
            f"[red]Error: Host '{hostname}' not found in any Include file[/red]"
        )
        raise TyperExit(1)

    target_file, target_host_lines = result
    if not skip_prompts:
        console.print(f"[green]✓ Found in {target_file.name}[/green]")

    current_type: Optional[str] = None
    if auth_type:
        if not skip_prompts:
            console.print(
                "\n[bold]Step 2: Detecting current authentication type...[/bold]"
            )
        current_type = detect_auth_type(target_host_lines)

        if current_type == "unknown":
            console.print("[red]Error: Could not detect authentication type[/red]")
            console.print(
                "[dim]Host configuration may be malformed or non-standard[/dim]"
            )
            raise TyperExit(1)

        if not skip_prompts:
            console.print(
                f"[dim]Current type: {AUTH_CONFIGS[current_type]['name']}[/dim]"
            )
            console.print(f"[dim]  {AUTH_CONFIGS[current_type]['description']}[/dim]")

        if auth_type not in AUTH_CONFIGS:
            console.print(f"[red]Error: Invalid auth type '{auth_type}'[/red]")
            console.print(
                "[dim]Valid types: password, keyboard-interactive, publickey[/dim]"
            )
            raise TyperExit(1)

        if auth_type == current_type:
            console.print(
                f"\n[yellow]Host '{hostname}' is already using {AUTH_CONFIGS[auth_type]['name']}[/yellow]"
            )
            if not compat_types:
                console.print("[dim]Nothing to do[/dim]")
                raise TyperExit(0)

    if compat_types:
        step_num = 3 if auth_type else 2
        if not skip_prompts:
            console.print(
                f"\n[bold]Step {step_num}: Validating compatibility options...[/bold]"
            )
        invalid_types = [ct for ct in compat_types if ct not in COMPAT_CONFIGS]
        if invalid_types:
            console.print(
                f"[red]Error: Invalid compatibility types: {', '.join(invalid_types)}[/red]"
            )
            console.print("[dim]Valid types: kex, macs, ciphers, hostkey[/dim]")
            raise TyperExit(1)

        if not skip_prompts:
            for compat_type in compat_types:
                console.print(f"[dim]  • {COMPAT_CONFIGS[compat_type]['name']}[/dim]")

    step_num = 3 if not auth_type else (4 if compat_types else 3)
    if not skip_prompts:
        console.print(f"\n[bold]Step {step_num}: Updating configuration...[/bold]")
    new_host_lines = render_host_update(
        target_host_lines,
        auth_type=auth_type,
        compat_types=compat_types,
    )

    if not skip_prompts:
        console.print("\n[bold]Preview:[/bold]")

        console.print("\n[red]- Old configuration:[/red]")
        old_syntax = Syntax(
            "".join(target_host_lines),
            "ssh-config",
            theme="monokai",
            line_numbers=False,
        )
        ui.show_panel(f"{hostname} (current)", old_syntax, style="red")

        console.print("\n[green]+ New configuration:[/green]")
        new_syntax = Syntax(
            "".join(new_host_lines), "ssh-config", theme="monokai", line_numbers=False
        )
        ui.show_panel(f"{hostname} (updated)", new_syntax, style="green")

        confirm_or_exit("\n[cyan]Apply this update?[/cyan]")

    step_num += 1
    if not skip_prompts:
        console.print(f"\n[bold]Step {step_num}: Applying changes...[/bold]")
    updated_file, backup_path = apply_host_update(
        parser,
        hostname,
        auth_type=auth_type,
        compat_types=compat_types,
    )
    if not skip_prompts:
        console.print(f"[dim]Backup created: {backup_path}[/dim]")

    if not skip_prompts:
        console.print("\n[bold green]✓ Success![/bold green]")
        console.print(f"Host '{hostname}' updated:")
        if auth_type and current_type:
            console.print(
                f"  Authentication: {AUTH_CONFIGS[current_type]['name']} → {AUTH_CONFIGS[auth_type]['name']}"
            )
        if compat_types:
            console.print(
                f"  Compatibility: {', '.join(COMPAT_CONFIGS[ct]['name'] for ct in compat_types)}"
            )
        console.print(f"\nUpdated: {updated_file.name}")
        console.print(f"Backup:  {backup_path.name}")


def update_command(
    ctx: typer.Context,
    hostname: Optional[str] = typer.Argument(
        None, help="Hostname to update", autocompletion=complete_hostname
    ),
    auth: Optional[str] = typer.Option(
        None,
        "--auth",
        help="Update authentication type (password, keyboard-interactive, publickey)",
    ),
    compat: bool = typer.Option(
        False, "--compat", help="Auto-detect and fix SSH compatibility issues"
    ),
    max_iterations: int = typer.Option(
        5, "--max-iterations", help="Maximum attempts for compatibility fixing"
    ),
) -> None:
    """Update host authentication and/or auto-fix compatibility issues."""

    parser = get_parser(ctx)

    if compat:
        if not hostname:
            include_files = parser.find_include_files()
            all_hosts: list[str] = []
            for file_path in include_files:
                _, hosts = parser.parse_ssh_config(file_path)
                for host_name, _ in hosts:
                    all_hosts.append(host_name)

            if not all_hosts:
                console.print("[red]Error: No hosts found in Include files[/red]")
                raise typer.Exit(1)

            hostname = select_via_fzf(sorted(all_hosts), "Select host to update:")

        if auth:
            cmd_update(parser, hostname=hostname, auth_type=auth, compat_types=None)
            console.print("[green]✓ Authentication updated[/green]\n")

        apply_and_display_compat_fixes(
            parser,
            hostname,
            max_iterations=max_iterations,
            show_header=True,
            auth_changed=auth,
        )
        return

    if auth:
        cmd_update(parser, hostname=hostname, auth_type=auth, compat_types=None)
        return

    console.print("[red]Error: Must specify --auth or --compat[/red]")
    console.print("[dim]Use 'nssh host update --help' for usage information[/dim]")
    raise typer.Exit(1)
