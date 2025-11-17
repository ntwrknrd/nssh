"""Rich console helpers isolated from performance-sensitive modules."""

from __future__ import annotations

from typing import Iterable, Optional, Sequence, Tuple

from rich import box
from rich.console import Console
from rich.table import Table

_console: Optional[Console] = None


def get_console() -> Console:
    """Return a shared Console instance with emoji disabled."""

    global _console
    if _console is None:
        _console = Console(emoji=False)
    return _console


def create_standard_table(
    columns: Sequence[Tuple[str, str]],
    rows: Optional[Iterable[Sequence[object]]] = None,
    footer: Optional[Sequence[object]] = None,
) -> Table:
    """Create a Rich table matching the legacy benchmark styling."""

    table = Table(
        show_header=True,
        header_style="bold cyan",
        box=box.ROUNDED,
        show_lines=False,
    )
    for title, style in columns:
        table.add_column(title, style=style)

    if rows:
        for row in rows:
            table.add_row(*[str(cell) for cell in row])

    if footer is not None:
        table.add_section()
        table.add_row(*[str(cell) for cell in footer], style="bold")

    return table
