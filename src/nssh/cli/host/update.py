"""Update command wiring for nssh host."""

from __future__ import annotations

from typing import Any, Dict, List, Optional

from typer import Exit as TyperExit

from nssh.cli import typer
from nssh.cli.common import ui
from nssh.cli.common.selectors import select_via_fzf
from nssh.core.ssh.fixer import AUTH_CONFIGS, detect_auth_type
from nssh.core.ssh.mutations import apply_host_update

from .compat import apply_and_display_compat_fixes
from .context import complete_hostname, console, get_parser


def _select_hostname(parser) -> str:
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
        return select_via_fzf(sorted(all_hosts), "Select host to update:")
    except TyperExit as exc:
        if exc.exit_code == 1:
            console.print("[dim]Or provide hostname as argument[/dim]")
        else:
            console.print("[yellow]Cancelled[/yellow]")
        raise


def _describe_auth_type(auth_type: Optional[str]) -> str:
    if auth_type in AUTH_CONFIGS:
        return AUTH_CONFIGS[auth_type]["name"]
    if auth_type == "unknown":
        return "Unknown (custom configuration)"
    return "Not configured"


def _derive_auth_update(
    current_type: Optional[str], final_test_result: Dict[str, Any] | None
) -> Optional[str]:
    if not final_test_result:
        return None

    auth_method = final_test_result.get("auth_method")
    if not auth_method:
        return None

    method_map = {
        "password": "password",
        "keyboard-interactive": "keyboard-interactive",
        "publickey": "publickey",
    }
    desired = (
        method_map.get(auth_method.lower()) if isinstance(auth_method, str) else None
    )
    if not desired or desired == current_type:
        return None
    return desired


def cmd_update(
    parser,
    *,
    hostname: Optional[str] = None,
    max_iterations: int = 5,
) -> None:
    """Automatically realign SSH host authentication and compatibility."""

    ui.show_panel(
        "Update SSH Host",
        "Auto-detect authentication requirements and legacy compatibility tweaks",
        style="cyan",
    )

    resolved_hostname = hostname or _select_hostname(parser)

    console.print(f"\n[bold]Step 1: Locating host '{resolved_hostname}'[/bold]")
    result = parser.find_host_in_files(resolved_hostname)
    if not result:
        console.print(
            f"[red]Error: Host '{resolved_hostname}' not found in any Include file[/red]"
        )
        raise TyperExit(1)

    target_file, target_host_lines = result
    console.print(f"[green]\u2713 Found in {target_file.name}[/green]")

    current_type = detect_auth_type(target_host_lines)
    console.print("\n[bold]Step 2: Checking authentication[/bold]")
    console.print(f"[dim]Current setting: {_describe_auth_type(current_type)}[/dim]")

    console.print("\n[bold]Step 3: Testing compatibility and connection[/bold]")
    compat_result = apply_and_display_compat_fixes(
        parser,
        resolved_hostname,
        max_iterations=max_iterations,
        show_header=False,
    )

    final_test_result = compat_result.get("final_test_result")
    desired_auth = _derive_auth_update(current_type, final_test_result)

    if desired_auth:
        console.print("\n[bold]Step 4: Aligning authentication[/bold]")
        console.print(
            f"[dim]Detected {AUTH_CONFIGS[desired_auth]['name']} from latest SSH test[/dim]"
        )
        updated_file, backup_path = apply_host_update(
            parser,
            resolved_hostname,
            auth_type=desired_auth,
            compat_types=None,
        )
        console.print(
            f"[green]\u2713 Authentication updated in {updated_file.name} (backup: {backup_path.name})[/green]"
        )
    else:
        console.print(
            "\n[dim]Authentication already matches the detected method or detection was inconclusive.[/dim]"
        )

    console.print("\n[bold green]\u2713 Update complete[/bold green]")


def update_command(
    ctx: typer.Context,
    hostname: Optional[str] = typer.Argument(
        None, help="Hostname to update", autocompletion=complete_hostname
    ),
) -> None:
    """Run automatic authentication + compatibility updates for a host."""

    parser = get_parser(ctx)
    cmd_update(parser, hostname=hostname)
