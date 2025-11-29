"""List command for nssh log."""

from __future__ import annotations

import re
from typing import Optional

from nssh.cli import click
from nssh.cli.common.banner import FAIL, OK, banner

from nssh.core.ui.console import get_console

from . import common


def _matches_pattern(pattern: re.Pattern, *fields: str) -> bool:
    """Return True if pattern matches any of the fields."""
    return any(pattern.search(f) for f in fields if f)


@click.command(short_help="List recorded sessions")
@click.option(
    "--select",
    "-s",
    default=None,
    help="Filter by regex pattern",
)
def list_sessions(select: Optional[str]) -> None:
    """Render recorded sessions, optionally filtering by regex pattern."""
    with banner("LIST RECORDED SESSIONS", OK) as set_outcome:
        _list_sessions(select, set_outcome)


def _list_sessions(select: Optional[str], set_outcome) -> None:
    """Internal implementation for listing sessions."""
    sessions = common.load_sessions()
    rows = list(sessions)

    if select:
        console = get_console()
        try:
            pattern = re.compile(select, re.IGNORECASE)
        except re.error as e:
            console.print(f"[red]Invalid regex pattern: {e}[/red]")
            set_outcome(FAIL)
            raise SystemExit(1)

        rows = [
            r
            for r in rows
            if _matches_pattern(
                pattern,
                r.host,
                r.session_label or "",
                r.started_at.strftime("%Y-%m-%d"),
            )
        ]

        if not rows:
            console.print(f"\n[yellow]No sessions matching pattern: {select}[/yellow]")
            raise SystemExit(0)

    common.print_sessions(rows)
