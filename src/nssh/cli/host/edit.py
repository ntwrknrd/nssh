"""Edit command for nssh host."""

from __future__ import annotations

from typing import Dict, List, Optional

from nssh.cli import Syntax, click
from nssh.cli.common.prompt import (
    ask_text,
    confirm,
    prompt_password_with_confirmation,
)
from nssh.cli.common.selectors import FzfCancelled, fzf_select
from nssh.cli.common.banner import FAIL, NOOP, OK, banner
from nssh.cli.common.ui import show_panel
from nssh.cli.common.workflows import confirm_or_exit
from nssh.core.ssh.fixer import (
    COMPAT_CONFIGS,
    detect_auth_type,
    parse_ssh_compatibility_error,
    test_ssh_connection_via_cli,
)
from nssh.core.ssh.mutations import apply_host_update

from .compat import apply_and_display_compat_fixes
from nssh.cli.common.credentials import (
    complete_hostname,
    console,
    get_manager,
    get_parser,
)


def _select_hostname(parser) -> str:
    """Select a hostname via fzf from all available hosts."""
    include_files = parser.find_include_files()
    all_hosts: List[str] = []

    for file_path in include_files:
        _, hosts = parser.parse_ssh_config(file_path)
        for host_name, _ in hosts:
            all_hosts.append(host_name)

    if not all_hosts:
        console.print("[red]Error: No hosts found in Include files[/red]")
        raise SystemExit(1)

    try:
        [hostname] = fzf_select(
            sorted(all_hosts), "Select host to edit:", exit_on_cancel=False
        )
        return hostname
    except FzfCancelled:
        console.print("[yellow]Cancelled[/yellow]")
        raise SystemExit(0)
    except SystemExit as exc:
        if exc.code == 1:
            console.print("[dim]Or provide hostname as argument[/dim]")
        raise


def _parse_host_config(host_lines: List[str]) -> Dict[str, str]:
    """Parse host configuration lines into a dictionary."""
    config: Dict[str, str] = {}
    for line in host_lines:
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        if line.lower().startswith("host "):
            continue
        parts = line.split(None, 1)
        if len(parts) == 2:
            key, value = parts
            config[key] = value
    return config


def _build_updated_config(
    original_lines: List[str],
    changes: Dict[str, str],
) -> List[str]:
    """Build updated config lines with changes applied."""
    result = []
    changed_keys = set()

    for line in original_lines:
        stripped = line.strip()

        # Keep empty lines and comments
        if not stripped or stripped.startswith("#"):
            result.append(line)
            continue

        # Keep Host line
        if stripped.lower().startswith("host "):
            result.append(line)
            continue

        # Check if this key is being changed
        parts = stripped.split(None, 1)
        if len(parts) >= 1:
            key = parts[0]
            if key in changes:
                # Replace with new value
                result.append(f"  {key} {changes[key]}\n")
                changed_keys.add(key)
                continue

        # Keep unchanged lines
        result.append(line)

    # Add any new keys that weren't in the original
    for key, value in changes.items():
        if key not in changed_keys:
            # Insert before the last line if it's empty, otherwise append
            if result and result[-1].strip() == "":
                result.insert(-1, f"  {key} {value}\n")
            else:
                result.append(f"  {key} {value}\n")

    return result


def _interactive_edit(
    ctx: click.Context,
    hostname: Optional[str],
    yes: bool,
    dry_run: bool,
    auth: Optional[str],
    set_outcome,
) -> None:
    """Interactive flow for editing a single host."""
    parser = get_parser(ctx)
    cm = get_manager(ctx)

    # Get hostname
    if hostname:
        final_hostname = hostname
    else:
        final_hostname = _select_hostname(parser)

    # Find which file contains this host
    console.print(f"[dim]Locating host '{final_hostname}'...[/dim]")

    found = parser.find_host_in_files(final_hostname)

    if not found:
        console.print(
            f"[red]Error: Host '{final_hostname}' not found in any Include file[/red]"
        )
        set_outcome(FAIL)
        raise SystemExit(1)

    target_file, host_lines = found
    console.print(f"[green]✓[/green] Found in {target_file.name}")

    # Parse current config
    current_config = _parse_host_config(host_lines)

    # Show current configuration
    console.print()
    config_text = "".join(host_lines)
    syntax = Syntax(config_text, "ssh-config", theme="monokai", line_numbers=False)
    show_panel("Current Configuration", syntax, style="cyan")

    # Collect changes
    changes: Dict[str, str] = {}

    # HostName (target)
    current_hostname = current_config.get("HostName", "")
    if yes:
        new_hostname = current_hostname
    else:
        new_hostname = ask_text("HostName", default=current_hostname)
    if new_hostname != current_hostname:
        changes["HostName"] = new_hostname

    # User
    current_user = current_config.get("User", "")
    if yes:
        new_user = current_user
    else:
        new_user = ask_text("User", default=current_user)
    if new_user != current_user:
        changes["User"] = new_user

    # Port
    current_port = current_config.get("Port", "22")
    if yes:
        new_port = current_port
    else:
        new_port = ask_text("Port", default=current_port)
    if new_port != current_port:
        changes["Port"] = new_port

    # IdentityFile
    current_identity = current_config.get("IdentityFile", "")
    if current_identity and not yes:
        new_identity = ask_text("IdentityFile", default=current_identity)
        if new_identity != current_identity:
            changes["IdentityFile"] = new_identity

    # Auth type and credential editing

    current_auth = detect_auth_type(host_lines)
    new_auth_type: Optional[str] = None
    new_password = None

    # Handle --auth flag or interactive auth type change
    if auth:
        # CLI flag specified
        target_auth = "publickey" if auth.lower() == "key" else "password"
        if target_auth != current_auth:
            new_auth_type = target_auth
            console.print(f"Auth type: {current_auth} -> {target_auth}")
        else:
            console.print(f"[dim]Auth type already set to {current_auth}[/dim]")
    elif not yes:
        # Interactive mode: offer to change auth type
        console.print(f"Current auth type: [cyan]{current_auth}[/cyan]")
        if confirm("Change auth type?", default=False):
            target = "key" if current_auth == "password" else "password"
            if confirm(f"Switch to {target}-based authentication?", default=True):
                new_auth_type = "publickey" if target == "key" else "password"

    # Handle password update (always prompt in interactive mode)
    remove_password = False
    if not yes:
        console.print("[dim]Enter '-' to remove stored password[/dim]")
        update_password = confirm("Update password?", default=bool(new_auth_type))
        if update_password:
            try:
                new_password = prompt_password_with_confirmation(
                    f"Password for {final_hostname}"
                )
                if new_password == "-":
                    remove_password = True
                    new_password = None
            except ValueError as e:
                console.print(f"[red]{e}[/red]")
                set_outcome(FAIL)
                raise SystemExit(1)

    # Summary
    if changes or new_password or new_auth_type or remove_password:
        console.print("\n[bold]Changes to apply:[/bold]")
        for key, value in changes.items():
            old_value = current_config.get(key, "(none)")
            console.print(f"  {key}: {old_value} -> {value}")
        if new_auth_type:
            console.print(f"  Auth type: {current_auth} -> {new_auth_type}")
        if new_password:
            console.print("  Password: (will be updated)")
        if remove_password:
            console.print("  Password: (will be removed)")
    else:
        set_outcome(NOOP)
        return

    if dry_run:
        console.print("\n[dim]Dry-run: would apply changes[/dim]")
        set_outcome(NOOP)
        return

    # Confirm
    if not yes:
        confirm_or_exit("Apply changes?")

    # Apply changes
    try:
        if changes:
            # Create backup
            backup_path = parser.create_backup(target_file)
            console.print(f"[dim]Backup created: {backup_path}[/dim]")

            # Update config
            header_lines, hosts = parser.parse_ssh_config(target_file)
            updated_hosts = []
            for h, lines in hosts:
                if h == final_hostname:
                    updated_hosts.append((h, _build_updated_config(lines, changes)))
                else:
                    updated_hosts.append((h, lines))

            parser.write_ssh_config(target_file, header_lines, updated_hosts)
            parser.rebuild_index()

            console.print("[green]✓[/green] SSH config updated")

        if new_password:
            username = new_user or current_user or "admin"
            cm.add_host_credential(final_hostname, username, new_password)
            console.print("[green]✓[/green] Password updated")

        if remove_password:
            try:
                cm.delete_host_all_credentials(final_hostname)
                console.print("[green]✓[/green] Password removed")
            except Exception:
                console.print("[dim]No stored password to remove[/dim]")

        if new_auth_type:
            apply_host_update(
                parser,
                final_hostname,
                auth_type=new_auth_type,
                create_backup=not changes,  # Create backup if not already created
            )
            console.print(f"[green]✓[/green] Auth type updated to {new_auth_type}")

        # Test connection if changes were made
        console.print("\n[bold]Testing connection...[/bold]")
        test_result = test_ssh_connection_via_cli(
            final_hostname,
            timeout=10,
            parser=parser,
        )

        if test_result["success"]:
            console.print("[green]✓[/green] Connection test passed")

            # Check if auth type needs updating (skip if already changed via --auth)
            if not new_auth_type:
                detected_method = test_result.get("auth_method")
                if detected_method and detected_method != current_auth:
                    console.print(
                        f"[dim]Detected {detected_method} authentication[/dim]"
                    )
                    if confirm(
                        f"[cyan]Update auth type to {detected_method}?[/cyan]",
                        default=True,
                    ):
                        apply_host_update(
                            parser,
                            final_hostname,
                            auth_type=detected_method,
                            create_backup=False,
                        )
                        console.print(
                            f"[green]✓[/green] Authentication updated to {detected_method}"
                        )
        else:
            # Handle test failure with auto-fix
            console.print(
                f"[yellow]! Connection test failed (exit code {test_result['exit_code']})[/yellow]"
            )

            if test_result["exit_code"] == 255:
                # Check for compatibility issues
                compat_types = parse_ssh_compatibility_error(
                    test_result.get("stderr", "") or test_result.get("stdout", "")
                )

                if compat_types:
                    console.print(
                        "\n[bold]Legacy SSH compatibility issue detected:[/bold]"
                    )
                    for compat_type in compat_types:
                        console.print(f"  * {COMPAT_CONFIGS[compat_type]['name']}")

                    if confirm(
                        "\n[cyan]Apply compatibility fix now?[/cyan]", default=True
                    ):
                        apply_and_display_compat_fixes(
                            parser,
                            final_hostname,
                            max_iterations=5,
                            show_header=False,
                        )
                else:
                    console.print(
                        "\n[dim]This may be a connection or authentication issue.[/dim]"
                    )
                    console.print(f"[dim]Try: ssh -v {final_hostname}[/dim]")

        # Default "Edit complete" footer will be used

    except Exception as e:
        console.print(f"[red]Error: {e}[/red]")
        import traceback

        traceback.print_exc()
        set_outcome(FAIL)
        raise SystemExit(1)


@click.command(short_help="Edit host config/credentials")
@click.argument(
    "hostname",
    metavar="ALIAS",
    required=False,
    default=None,
    shell_complete=complete_hostname,
)
@click.option("-y", "--yes", is_flag=True, default=False, help="Skip confirmations")
@click.option("--dry-run", is_flag=True, default=False, help="Preview only")
@click.option(
    "--auth",
    type=click.Choice(["password", "key"], case_sensitive=False),
    default=None,
    help="Change auth type: password or key",
)
@click.pass_context
def edit_command(
    ctx: click.Context,
    hostname: Optional[str],
    yes: bool,
    dry_run: bool,
    auth: Optional[str],
) -> None:
    """Edit SSH host configuration and credentials.

    \b
    Examples:
        nssh host edit server       # step through fields
        nssh host edit              # select host via fzf
        nssh host edit server --auth key  # switch to key-based auth

    For batch changes, use 'nssh host rm' + 'nssh host add'.
    """
    with banner("EDIT SSH HOST", OK) as set_outcome:
        _interactive_edit(ctx, hostname, yes, dry_run, auth, set_outcome)
