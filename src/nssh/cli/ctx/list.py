"""Context list command for nssh ctx."""

from __future__ import annotations

import re
from pathlib import Path
from typing import Dict, List, Optional

from nssh.cli import click
from nssh.cli.common import ui
from nssh.cli.common.banner import FAIL, OK, banner
from nssh.cli.common.credentials import get_manager
from nssh.core.env.paths import ssh_include_dir
from nssh.core.ssh.config import SSHConfigParser
from nssh.core.ui.console import get_console

console = get_console()


def _matches_pattern(pattern: re.Pattern, *fields: str) -> bool:
    """Return True if pattern matches any of the fields."""
    return any(pattern.search(f) for f in fields if f)


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


@click.command(short_help="List all contexts")
@click.option("--select", "-s", default=None, help="Filter by regex pattern")
@click.pass_context
def list_command(ctx: click.Context, select: Optional[str]) -> None:
    """List all credential contexts."""
    cm = get_manager(ctx)

    with banner("LIST SSH CONTEXTS", OK) as set_outcome:
        _list_contexts(cm, select, set_outcome)


def _list_contexts(cm, select: Optional[str], set_outcome) -> None:
    """Internal implementation for listing contexts."""
    contexts = cm.list_contexts()
    include_lookup = _include_lookup()

    if not contexts:
        console.print("\n[yellow]No contexts configured[/yellow]")
        console.print("\nCreate one with:")
        console.print("  [cyan]nssh ctx add NAME[/cyan]")
        return

    # Apply regex filter
    if select:
        try:
            pattern = re.compile(select, re.IGNORECASE)
        except re.error as e:
            console.print(f"[red]Invalid regex pattern: {e}[/red]")
            set_outcome(FAIL)
            raise SystemExit(1)

        contexts = [
            entry
            for entry in contexts
            if _matches_pattern(
                pattern,
                entry["name"],
                entry["git_include_file"],
                entry["credential"]["username"] if entry["credential"] else "",
            )
        ]

    if not contexts:
        console.print(f"\n[yellow]No contexts matching pattern: {select}[/yellow]")
        return

    rows = []
    for entry in contexts:
        credential = entry["credential"]
        include_display = _display_include_path(
            entry["git_include_file"], include_lookup
        )
        domain = entry.get("domain", "")
        rows.append(
            (
                entry["name"],
                include_display,
                domain if domain else "-",
                credential["username"] if credential else "-",
            )
        )

    ui.print_table(
        (
            ("Context", "cyan"),
            ("SSH Config File", "green"),
            ("Domain", "magenta"),
            ("Fallback Credential", "yellow"),
        ),
        rows,
    )
