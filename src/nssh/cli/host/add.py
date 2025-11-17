from __future__ import annotations

import os
from pathlib import Path
from typing import Optional

from nssh.cli import Syntax, typer
from nssh.cli.common.prompt import (
    ask_text,
    confirm,
    prompt_password_with_confirmation,
)
from nssh.cli.common.selectors import select_include_file, select_via_fzf
from nssh.cli.common.ui import show_panel
from nssh.cli.common.workflows import choose_password_source, confirm_or_exit
from nssh.core.ssh.fixer import (
    COMPAT_CONFIGS,
    generate_ssh_config,
    parse_ssh_compatibility_error,
    test_ssh_connection_via_wrapper,
)

from .compat import apply_and_display_compat_fixes
from .context import complete_config_file, console, get_manager, get_parser


def add_command(
    ctx: typer.Context,
    fqdn: Optional[str] = typer.Argument(None, help="Fully qualified domain name"),
    hostname: Optional[str] = typer.Option(
        None, "--hostname", help="Custom hostname (default: first part of FQDN)"
    ),
    user: Optional[str] = typer.Option(None, "--user", help="SSH username"),
    port: int = typer.Option(22, "--port", help="SSH port"),
    password: bool = typer.Option(False, "--password", help="Use password auth"),
    key: bool = typer.Option(False, "--key", help="Use key-based auth"),
    file: Optional[str] = typer.Option(
        None,
        "--file",
        help="SSH config file name in ~/.ssh/ (skips selection)",
        autocompletion=complete_config_file,
    ),
    no_test: bool = typer.Option(
        False,
        "--no-test",
        help="Skip connection test after adding host",
    ),
    skip_password_prompt: bool = typer.Option(
        False,
        "--skip-password-prompt",
        help="Skip password prompt and use context fallback credentials",
    ),
) -> None:
    """Add SSH host to configuration"""
    cm = get_manager(ctx)
    parser = get_parser(ctx)

    show_panel("Add SSH Host", "Add new host to SSH configuration")

    # Step 1: Get FQDN
    console.print("\n[bold]Step 1: Fully Qualified Domain Name[/bold]")

    final_fqdn = fqdn
    if not final_fqdn:
        final_fqdn = ask_text("[cyan]Enter FQDN[/cyan]")

    if not final_fqdn or "." not in final_fqdn:
        console.print("[red]Error: Invalid FQDN (must contain at least one '.')[/red]")
        raise typer.Exit(1)

    console.print(f"[dim]FQDN: {final_fqdn}[/dim]")

    # Step 2: Determine hostname
    console.print("\n[bold]Step 2: SSH Alias (hostname)[/bold]")

    default_hostname = hostname or final_fqdn.split(".")[0]
    final_hostname = ask_text("[cyan]Enter hostname[/cyan]", default=default_hostname)

    console.print(f"[dim]Hostname: {final_hostname}[/dim]")

    # Step 3: Select target config file
    console.print("\n[bold]Step 3: Select target config file[/bold]")

    target_selection = select_include_file(parser, file, "Select config file:")
    if isinstance(target_selection, list):
        console.print(
            "[red]Error: Multiple files selected; --all not supported here[/red]"
        )
        raise typer.Exit(1)
    target_file = target_selection
    console.print(f"[dim]Target file: {target_file}[/dim]")

    # Step 4: Check for duplicates
    console.print("\n[bold]Step 4: Checking for duplicates[/bold]")

    if parser.host_exists(target_file, final_hostname):
        console.print(
            f"[red]Error: Host '{final_hostname}' already exists in {target_file.name}[/red]"
        )
        raise typer.Exit(1)

    console.print("[green]✓ No duplicate found[/green]")

    # Step 5: Username
    console.print("\n[bold]Step 5: SSH Username[/bold]")

    # Try to get default from context if available
    default_user: Optional[str] = None
    if user:
        default_user = user
    else:
        # Check if there's a context for this file
        git_include_file = target_file.name
        context = cm.get_context(git_include_file)
        if context and context.get("credential"):
            default_user = context["credential"]["username"]
            console.print(f"[dim]Using default from context '{context['name']}'[/dim]")
        else:
            # Fall back to environment variables
            default_user = os.getenv("NSSH_DEFAULT_USER", os.getenv("USER", "admin"))

    username = ask_text("[cyan]Enter username[/cyan]", default=default_user)

    console.print(f"[dim]Username: {username}[/dim]")

    # Step 6: Port
    console.print("\n[bold]Step 6: SSH Port[/bold]")

    final_port_input = ask_text("[cyan]Enter port[/cyan]", default=str(port))

    try:
        final_port = int(final_port_input)
    except ValueError:
        console.print("[red]Error: Invalid port number[/red]")
        raise typer.Exit(1)

    console.print(f"[dim]Port: {final_port}[/dim]")

    # Step 7: Authentication type
    console.print("\n[bold]Step 7: Authentication Type[/bold]")

    auth_type = None
    if password:
        auth_type = "password"
    elif key:
        auth_type = "key"
    else:
        # Use fzf to select
        auth_options = [
            "password - Password-based authentication",
            "key - Key-based authentication",
        ]
        selected = select_via_fzf(auth_options, "Select auth type:")

        auth_type = selected.split(" - ")[0]

    console.print(f"[dim]Auth type: {auth_type}[/dim]")

    # Step 8: Handle password if needed
    if auth_type == "password":
        console.print("\n[bold]Step 8: Password Configuration[/bold]")

        # Check if context has credentials
        git_include_file = target_file.name
        context = cm.get_context(git_include_file)
        has_context_creds = bool(context and context.get("credential"))

        password_choice = choose_password_source(
            context_name=context["name"] if context else None,
            has_context_credentials=has_context_creds,
            skip_prompt=skip_password_prompt,
        )

        if password_choice == "custom":
            try:
                pwd = prompt_password_with_confirmation(
                    "[cyan]Enter password for {hostname}[/cyan]".format(
                        hostname=final_hostname
                    )
                )
            except ValueError as e:
                console.print(f"[red]{e}[/red]")
                raise typer.Exit(1)

            try:
                cm.add_host_credential(final_hostname, username, pwd)
                console.print("[green]✓ Custom password stored[/green]")
            except Exception as e:
                console.print(f"[red]Error storing password: {e}[/red]")
                raise typer.Exit(1)
        elif password_choice == "context":
            if has_context_creds and context:
                extra = " (--skip-password-prompt)" if skip_password_prompt else ""
                console.print(
                    f"[dim]Will use context '{context['name']}' credentials{extra}[/dim]"
                )
            else:
                console.print(
                    "[dim]No credentials stored (connection may fail without context)[/dim]"
                )
        else:
            console.print(
                "[yellow]Warning: --skip-password-prompt specified but no context credentials found[/yellow]"
            )
            console.print(
                "[dim]No credentials stored (connection may fail without context)[/dim]"
            )

    # Step 9: Generate config
    step_num = 9 if auth_type == "password" else 8
    console.print(f"\n[bold]Step {step_num}: Preview Configuration[/bold]")

    config_block = generate_ssh_config(
        final_hostname, final_fqdn, username, final_port, auth_type
    )

    syntax = Syntax(config_block, "ssh-config", theme="monokai", line_numbers=False)
    show_panel("New Host Configuration", syntax, style="green")

    # Step 10: Show insertion preview
    step_num += 1
    console.print(f"\n[bold]Step {step_num}: Insertion Preview[/bold]")

    header_lines, hosts = parser.parse_ssh_config(target_file)
    insert_index = parser.find_insertion_index(hosts, final_hostname)

    before, after = parser.get_surrounding_hosts(hosts, insert_index, context=2)

    if before:
        for h in before:
            console.print(f"[dim]  {h}[/dim]")

    console.print(f"[cyan]  → {final_hostname}[/cyan]  [green](new)[/green]")

    if after:
        for h in after:
            console.print(f"[dim]  {h}[/dim]")
    else:
        console.print("[dim]  (end of file)[/dim]")

    # Step 11: Confirm
    step_num += 1
    console.print(f"\n[bold]Step {step_num}: Confirm[/bold]")

    confirm_or_exit("[cyan]Add this host to the config?[/cyan]")

    # Step 12: Write config
    try:
        # Create backup
        backup_path = parser.create_backup(target_file)
        console.print(f"[dim]Backup created: {backup_path}[/dim]")

        # Insert host - convert config block to lines with newlines
        lines = config_block.split("\n")
        config_lines = [
            (line + "\n") if i < len(lines) - 1 or line else "\n"
            for i, line in enumerate(lines)
            if line or i == len(lines) - 1
        ]

        hosts.insert(insert_index, (final_hostname, config_lines))

        # Write back
        parser.write_ssh_config(target_file, header_lines, hosts)

        # Rebuild host index for fast lookups
        parser.rebuild_index()

        # Test connection unless --no-test was specified
        test_passed = False
        if not no_test:
            step_num += 1
            console.print(f"\n[bold]Step {step_num}: Testing Connection[/bold]")
            console.print("[dim]Testing SSH connection with verbose output...[/dim]")

            test_result = test_ssh_connection_via_wrapper(final_hostname, timeout=10)

            if test_result["success"]:
                console.print("[green]✓ Connection test passed![/green]")
                test_passed = True
            else:
                # Check if compatibility issues detected
                console.print(
                    f"\n[yellow]⚠ Connection test failed (exit code {test_result['exit_code']})[/yellow]"
                )

                # Write debug info
                import tempfile

                debug_dir = Path("/tmp/nssh")
                debug_dir.mkdir(exist_ok=True)
                debug_file = tempfile.mktemp(
                    suffix=".txt", prefix="nssh-debug-", dir=str(debug_dir)
                )
                with open(debug_file, "w") as f:
                    f.write(f"Exit code: {test_result['exit_code']}\n")
                    f.write(f"Stderr length: {len(test_result['stderr'])}\n")
                    f.write(f"Stdout length: {len(test_result['stdout'])}\n")
                    f.write(f"\n=== STDERR ===\n{test_result['stderr']}\n")
                    f.write(f"\n=== STDOUT ===\n{test_result['stdout']}\n")
                console.print(f"[dim]Debug info written to: {debug_file}[/dim]")

                # Only attempt auto-fix for SSH protocol errors (exit code 255)
                if test_result["exit_code"] == 255:
                    # Extract raw SSH output
                    raw_ssh_output = ""
                    stderr = test_result["stderr"]
                    if "=== RAW SSH OUTPUT ===" in stderr:
                        parts = stderr.split("=== RAW SSH OUTPUT ===")
                        if len(parts) > 1:
                            raw_part = parts[1].split("=== END RAW OUTPUT ===")[0]
                            raw_ssh_output = raw_part.strip()

                    # Detect compatibility issues
                    compat_types = (
                        parse_ssh_compatibility_error(raw_ssh_output)
                        if raw_ssh_output
                        else []
                    )

                    if compat_types:
                        # Display compatibility issue details
                        console.print(
                            "\n[bold]Legacy SSH compatibility issue detected:[/bold]"
                        )
                        for compat_type in compat_types:
                            console.print(f"  • {COMPAT_CONFIGS[compat_type]['name']}")

                        # Extract error message
                        error_line = ""
                        if raw_ssh_output:
                            for line in raw_ssh_output.split("\n"):
                                if not line.startswith("debug") and (
                                    "Unable to negotiate" in line
                                    or "no matching" in line
                                    or any(
                                        COMPAT_CONFIGS[ct].get("error_pattern", "")
                                        in line.lower()
                                        for ct in compat_types
                                    )
                                ):
                                    error_line = line.strip()
                                    break

                        if error_line:
                            console.print(f"\n[dim]Error: {error_line}[/dim]")

                        # Offer to auto-fix (prompt once, then iterate automatically)
                        if confirm(
                            "\n[cyan]Apply compatibility fix now?[/cyan]", default=True
                        ):
                            success, _ = apply_and_display_compat_fixes(
                                parser,
                                final_hostname,
                                max_iterations=5,
                                show_header=False,
                            )
                            test_passed = success
                        else:
                            # User declined - show auto-fix command
                            console.print("\n[dim]You can auto-fix with:[/dim]")
                            console.print(
                                f"[dim]  nssh host update {final_hostname} --compat[/dim]"
                            )
                    else:
                        # Exit code 255 but no compatibility patterns
                        console.print(
                            "\n[dim]This appears to be an SSH protocol or connection error.[/dim]"
                        )
                        console.print(
                            f"[dim]Try running: ssh -v {final_hostname}[/dim]"
                        )
                        console.print(
                            f"[dim]Or run: nssh host update {final_hostname} --compat[/dim]"
                        )
                elif test_result["exit_code"] == 124:
                    # Timeout
                    console.print(f"\n[dim]{test_result['stderr']}[/dim]")
                    console.print(
                        "[dim]The host may be unreachable or behind a firewall.[/dim]"
                    )
                    console.print(
                        f"[dim]You can test manually with: nssh {final_hostname}[/dim]"
                    )
                else:
                    # Other error (auth failure, etc.)
                    error_msg = (
                        test_result["stderr"].split("\n")[0]
                        if test_result["stderr"]
                        else "Unknown error"
                    )
                    console.print(f"\n[dim]{error_msg}[/dim]")
                    console.print(
                        f"[dim]You can test manually with: nssh {final_hostname}[/dim]"
                    )

        # Show success message based on test status
        if not no_test and test_passed:
            console.print("\n[bold green]✓ Success![/bold green]")
            console.print(f"Host '{final_hostname}' added to {target_file.name}")
            console.print(
                f"Connection test passed! You can connect with: [cyan]nssh {final_hostname}[/cyan]"
            )
        elif not no_test and not test_passed:
            console.print("\n[bold yellow]✓ Host added to config[/bold yellow]")
            console.print(f"Host '{final_hostname}' added to {target_file.name}")
            console.print(
                "\n[yellow]⚠ Connection test failed - verify host is reachable[/yellow]"
            )
            console.print(f"Try: [cyan]nssh {final_hostname}[/cyan]")
        else:
            console.print("\n[bold green]✓ Host added to config[/bold green]")
            console.print(f"Host '{final_hostname}' added to {target_file.name}")
            console.print(f"\nYou can connect with: [cyan]nssh {final_hostname}[/cyan]")

    except Exception as e:
        console.print(f"[red]Error: {e}[/red]")
        import traceback

        traceback.print_exc()
        raise typer.Exit(1)
