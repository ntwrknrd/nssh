from __future__ import annotations

import re
from typing import Optional

from nssh.cli import click
from nssh.cli.common import ui
from nssh.cli.common.banner import FAIL, OK, banner
from nssh.core.ssh.fixer import detect_auth_type, extract_ssh_fields

from nssh.cli.common.credentials import console, get_parser, get_manager


def _check_credential_status(cm, hostname: str, config_file: str) -> str:
    """Return credential type indicator: [H]=host, [C]=context, [ ]=none."""
    # Check host-specific credentials first
    host_creds = cm.get_host_credentials(hostname)
    if host_creds:
        return "[H]"

    # Check context credentials as fallback
    context = cm.get_context(config_file)
    if context and context.get("credential"):
        return "[C]"

    return "[ ]"


def _matches_pattern(pattern: re.Pattern, *fields: str) -> bool:
    """Return True if pattern matches any of the fields."""
    return any(pattern.search(f) for f in fields if f)


@click.command(short_help="List all hosts")
@click.option("--select", "-s", default=None, help="Filter by regex pattern")
@click.pass_context
def list_hosts_command(ctx: click.Context, select: Optional[str]) -> None:
    """List SSH hosts from config files"""
    parser = get_parser(ctx)
    cm = get_manager(ctx)

    with banner("LIST SSH HOSTS", OK) as set_outcome:
        _list_hosts(parser, cm, select, set_outcome)


def _list_hosts(parser, cm, select: Optional[str], set_outcome) -> None:
    """Internal implementation for listing hosts."""
    # List all Include files
    files_to_list = parser.find_include_files()

    if not files_to_list:
        console.print("[yellow]No config files found[/yellow]")
        raise SystemExit(0)

    # Collect all hosts
    all_hosts = []

    for file_path in files_to_list:
        header_lines, hosts = parser.parse_ssh_config(file_path)

        for hostname, host_lines in hosts:
            # Parse host configuration using utility function
            fields = extract_ssh_fields(host_lines)
            auth = detect_auth_type(host_lines)
            cred_status = _check_credential_status(cm, hostname, file_path.name)

            all_hosts.append(
                {
                    "hostname": hostname,
                    "target": fields["hostname"],
                    "user": fields["user"],
                    "port": fields["port"],
                    "file": file_path.name,
                    "auth": auth,
                    "cred": cred_status,
                }
            )

    if not all_hosts:
        console.print("\n[yellow]No hosts found[/yellow]")
        raise SystemExit(0)

    # Filter by regex pattern if provided
    if select:
        try:
            pattern = re.compile(select, re.IGNORECASE)
        except re.error as e:
            console.print(f"[red]Invalid regex pattern: {e}[/red]")
            set_outcome(FAIL)
            raise SystemExit(1)

        all_hosts = [
            h
            for h in all_hosts
            if _matches_pattern(
                pattern,
                h["hostname"],
                h["target"],
                h["user"],
                h["port"],
                h["file"],
                h["auth"],
                h["cred"],
            )
        ]

        if not all_hosts:
            console.print(f"\n[yellow]No hosts matching pattern: {select}[/yellow]")
            raise SystemExit(0)

    # Sort hosts alphabetically by hostname
    all_hosts.sort(key=lambda h: h["hostname"].lower())

    # Display table
    rows = [
        (
            h["hostname"],
            h["target"],
            h["user"],
            h["port"],
            h["file"],
            h["auth"],
            h["cred"],
        )
        for h in all_hosts
    ]

    ui.print_table(
        (
            ("Host", "cyan"),
            ("HostName", ""),
            ("User", "dim"),
            ("Port", "dim"),
            ("Include File", "dim"),
            ("Preferred Auth", "green"),
            ("Cred", "yellow"),
        ),
        rows,
    )

    # Add legend after table
    console.print(
        "\n[dim]Legend: [H] = host credential, [C] = context credential, [ ] = no credential[/dim]"
    )
