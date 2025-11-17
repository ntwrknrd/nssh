"""List command for nssh log."""

from __future__ import annotations

from typing import List

from nssh.cli import typer

from nssh.core.ui.console import get_console

from . import common


def list_sessions(
    search: List[str] = typer.Option(
        [],
        "--search",
        "-s",
        help="Filter results by keyword (repeatable for AND logic)",
    ),
) -> None:
    """Render recorded sessions, optionally filtering by keywords."""
    sessions = common.load_sessions()
    rows = list(sessions)

    if search:
        console = get_console()
        for term in search:
            term_lower = term.lower()
            rows = [
                r
                for r in rows
                if term_lower in r.host.lower()
                or (r.session_label and term_lower in r.session_label.lower())
                or term_lower in r.started_at.strftime("%Y-%m-%d").lower()
            ]

        if not rows:
            console.print(
                f"\n[yellow]No sessions found matching all terms: {' '.join(search)}[/yellow]"
            )
            raise typer.Exit(0)

    common.print_sessions(rows)
