from __future__ import annotations

from typing import List

from nssh.cli import typer
from nssh.cli.common import ui
from nssh.core.ssh.fixer import detect_auth_type, extract_ssh_fields

from .context import console, get_parser


def list_hosts_command(
    ctx: typer.Context,
    search: List[str] = typer.Option(
        [],
        "--search",
        "-s",
        help="Filter results by keyword (repeatable for AND logic)",
    ),
) -> None:
    """List SSH hosts from config files"""
    parser = get_parser(ctx)

    ui.show_panel("SSH Host List", "Hosts from SSH configuration files")

    # List all Include files
    files_to_list = parser.find_include_files()

    if not files_to_list:
        console.print("[yellow]No config files found[/yellow]")
        raise typer.Exit(0)

    # Collect all hosts
    all_hosts = []

    for file_path in files_to_list:
        header_lines, hosts = parser.parse_ssh_config(file_path)

        for hostname, host_lines in hosts:
            # Parse host configuration using utility function
            fields = extract_ssh_fields(host_lines)
            auth = detect_auth_type(host_lines)

            all_hosts.append(
                {
                    "hostname": hostname,
                    "target": fields["hostname"],
                    "user": fields["user"],
                    "port": fields["port"],
                    "file": file_path.name,
                    "auth": auth,
                }
            )

    if not all_hosts:
        console.print("\n[yellow]No hosts found[/yellow]")
        raise typer.Exit(0)

    # Filter by search keywords if provided (AND logic)
    if search:
        for term in search:
            term_lower = term.lower()
            all_hosts = [
                h
                for h in all_hosts
                if term_lower in h["hostname"].lower()
                or term_lower in h["target"].lower()
                or term_lower in h["user"].lower()
                or term_lower in h["port"].lower()
                or term_lower in h["file"].lower()
                or term_lower in h["auth"].lower()
            ]

        if not all_hosts:
            console.print(
                f"\n[yellow]No hosts found matching all terms: {' '.join(search)}[/yellow]"
            )
            raise typer.Exit(0)

    # Sort hosts alphabetically by hostname
    all_hosts.sort(key=lambda h: h["hostname"].lower())

    # Display table
    console.print()
    rows = [
        (h["hostname"], h["target"], h["user"], h["port"], h["file"], h["auth"])
        for h in all_hosts
    ]

    ui.print_table(
        (
            ("Host", "cyan"),
            ("HostName", ""),
            ("User", "dim"),
            ("Port", "dim"),
            ("File", "dim"),
            ("Auth", "green"),
        ),
        rows,
    )
