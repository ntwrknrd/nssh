"""Rich/console helpers shared across CLI commands."""

from __future__ import annotations

from typing import Iterable, Sequence, Tuple

from rich.console import RenderableType
from rich.panel import Panel
from rich.text import Text

from nssh.core.ui.console import create_standard_table, get_console


def show_panel(
    title: str,
    body: RenderableType,
    *,
    style: str = "cyan",
    subtitle: str | None = None,
) -> None:
    """Render a bordered Rich panel around ``body``.

    Example:
        >>> show_panel("Create Context", "Add credentials for ~/.ssh/work")
    """
    console = get_console()
    if isinstance(body, str):
        panel_body = f"[bold {style}]{title}[/bold {style}]\n{body}"
        panel = Panel.fit(
            panel_body,
            border_style=style,
            subtitle=subtitle,
        )
    else:
        panel = Panel.fit(
            body,
            border_style=style,
            title=f"[bold]{title}[/bold]",
            subtitle=subtitle,
        )
    console.print(panel)


def print_table(
    columns: Sequence[Tuple[str, str]],
    rows: Iterable[Sequence[object]],
    *,
    footer: Sequence[object] | None = None,
) -> None:
    """Render a Rich table using the shared table factory."""
    console = get_console()
    row_list = [tuple(row) for row in rows]
    footer_tuple = tuple(footer) if footer is not None else None
    table = create_standard_table(columns, row_list, footer_tuple)
    console.print(table)


def info(message: str) -> None:
    """Print low-emphasis diagnostic text."""
    get_console().print(Text(message, style="dim"))


def success(message: str) -> None:
    """Print success text in green."""
    get_console().print(Text(message, style="green"))


def warning(message: str) -> None:
    """Print warning text in yellow."""
    get_console().print(Text(message, style="yellow"))


def error(message: str) -> None:
    """Print error text in red."""
    get_console().print(Text(message, style="red"))
