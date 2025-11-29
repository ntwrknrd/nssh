"""Get command for nssh ctx - show details for a specific context."""

from __future__ import annotations

import hashlib
from pathlib import Path
from typing import Dict, List, Optional

from nssh.cli import click
from nssh.cli.common.credentials import complete_context, get_manager
from nssh.core.env.paths import ssh_include_dir
from nssh.core.ssh.config import SSHConfigParser
from nssh.core.ui.console import get_console

console = get_console()


def _mask_password(password: str) -> str:
    """Return deterministic asterisk mask based on password hash.

    Length is 6-13 asterisks based on hash, not actual password length.
    """
    hash_val = int(hashlib.sha256(password.encode()).hexdigest()[:8], 16)
    length = (hash_val % 8) + 6
    return "*" * length


def _abbreviate_home(path: Path) -> str:
    """Return path string with home directory replaced by ~ when possible."""
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


def _find_context_by_name(cm, name: str) -> Optional[Dict]:
    """Find a context entry by name from list_contexts()."""
    for entry in cm.list_contexts():
        if entry.get("name") == name:
            return entry
    return None


@click.command(short_help="Show context details")
@click.argument("name", required=True, shell_complete=complete_context)
@click.pass_context
def get_command(ctx: click.Context, name: str) -> None:
    """Show details for a specific credential context."""
    cm = get_manager(ctx)

    context_entry = _find_context_by_name(cm, name)

    if context_entry is None:
        console.print(f"[red]Context '{name}' not found[/red]")
        raise SystemExit(1)

    include_lookup = _include_lookup()
    include_display = _display_include_path(
        context_entry["git_include_file"], include_lookup
    )

    # Display context details
    console.print()
    console.print(f"[bold]Context:[/bold]  {name}")
    console.print(f"[bold]File:[/bold]     {include_display}")
    domain = context_entry.get("domain", "")
    if domain:
        console.print(f"[bold]Domain:[/bold]   {domain}")

    # Show credential with masked password
    console.print()
    credential = context_entry.get("credential")
    if credential:
        username = credential.get("username", "")
        password = credential.get("password", "")
        masked = _mask_password(password) if password else "[dim](no password)[/dim]"
        console.print("[bold]Credential:[/bold]")
        console.print(f"  {username:<12} {masked}")
    else:
        console.print("[bold]Credential:[/bold] [dim](none)[/dim]")

    # Show hosts using this context
    parser = SSHConfigParser()
    git_include_file = context_entry["git_include_file"]

    # Collect hosts with their FQDNs
    hosts_list: List[tuple[str, str]] = []
    for include_path in parser.find_include_files():
        if include_path.name == git_include_file:
            _, hosts = parser.parse_ssh_config(include_path)
            for hostname, lines in hosts:
                # Extract HostName from config lines
                fqdn = ""
                for line in lines:
                    stripped = line.strip().lower()
                    if stripped.startswith("hostname "):
                        fqdn = line.strip().split(None, 1)[1]
                        break
                hosts_list.append((hostname, fqdn))
            break

    total_hosts = len(hosts_list)
    console.print()
    console.print(f"[bold]Hosts:[/bold] ({total_hosts})")

    if total_hosts == 0:
        console.print("  [dim](none)[/dim]")
    else:
        max_display = 10
        for hostname, fqdn in hosts_list[:max_display]:
            if fqdn:
                console.print(f"  {hostname:<12} {fqdn}")
            else:
                console.print(f"  {hostname}")
        if total_hosts > max_display:
            remaining = total_hosts - max_display
            console.print(f"  [dim]... and {remaining} more[/dim]")
