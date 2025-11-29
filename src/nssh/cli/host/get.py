"""Get command for nssh host - show details for a specific host."""

from __future__ import annotations

import hashlib

from nssh.cli import click
from nssh.cli.common.banner import FAIL, OK, banner
from nssh.cli.common.credentials import (
    complete_hostname,
    console,
    get_manager,
    get_parser,
)
from nssh.core.ssh.fixer import detect_auth_type, extract_ssh_fields


def _mask_password(password: str) -> str:
    """Return deterministic asterisk mask based on password hash.

    Length is 6-13 asterisks based on hash, not actual password length.
    """
    hash_val = int(hashlib.sha256(password.encode()).hexdigest()[:8], 16)
    length = (hash_val % 8) + 6
    return "*" * length


@click.command(short_help="Show host details")
@click.argument(
    "hostname", metavar="ALIAS", required=True, shell_complete=complete_hostname
)
@click.pass_context
def get_command(ctx: click.Context, hostname: str) -> None:
    """Show details for a specific SSH host including stored credentials."""
    with banner("HOST DETAILS", OK) as set_outcome:
        _get_impl(ctx, hostname, set_outcome)


def _get_impl(ctx: click.Context, hostname: str, set_outcome) -> None:
    """Internal implementation for get command."""
    parser = get_parser(ctx)
    cm = get_manager(ctx)

    # Find the host in SSH config
    host_config = None
    host_file = None

    for file_path in parser.find_include_files():
        _, hosts = parser.parse_ssh_config(file_path)
        for host, lines in hosts:
            if host.lower() == hostname.lower():
                host_config = lines
                host_file = file_path.name
                hostname = host  # Use exact case from config
                break
        if host_config:
            break

    if not host_config:
        console.print(f"[red]Error:[/red] Host '{hostname}' not found in SSH config")
        set_outcome(FAIL)
        raise SystemExit(1)

    # Extract SSH config fields
    fields = extract_ssh_fields(host_config)
    auth = detect_auth_type(host_config)

    # Display host details
    console.print(f"[bold]Host:[/bold]     {hostname}")
    console.print(f"[bold]HostName:[/bold] {fields['hostname']}")
    console.print(f"[bold]User:[/bold]     {fields['user']}")
    console.print(f"[bold]Port:[/bold]     {fields['port']}")
    console.print(f"[bold]File:[/bold]     {host_file}")
    console.print(f"[bold]Auth:[/bold]     {auth}")

    # Collect all credentials
    console.print()
    context = cm.get_context(host_file) if host_file else None
    host_creds = cm.get_host_credentials(hostname)

    has_context_cred = context is not None and context.get("credential")
    has_host_creds = bool(host_creds)

    if not has_context_cred and not has_host_creds:
        console.print("[bold]Credentials:[/bold] [dim](none)[/dim]")
        return

    console.print("[bold]Credentials:[/bold]")

    # Show context credential first (fallback)
    if has_context_cred and context is not None:
        ctx_cred = context["credential"]
        ctx_user = ctx_cred.get("username", "")
        ctx_pass = ctx_cred.get("password", "")
        ctx_masked = (
            _mask_password(ctx_pass) if ctx_pass else "[dim](no password)[/dim]"
        )
        console.print(
            f"  {ctx_user:<12} {ctx_masked:<14} [dim](context: {context['name']})[/dim]"
        )

    # Show host-specific credentials
    for cred in host_creds or []:
        username = cred.get("username", "")
        password = cred.get("password", "")
        masked = _mask_password(password) if password else "[dim](no password)[/dim]"
        console.print(f"  {username:<12} {masked}")
