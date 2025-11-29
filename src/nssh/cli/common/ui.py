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
    console.print()


def show_banner(title: str, *, style: str = "cyan", is_footer: bool = False) -> None:
    """Render a horizontal rule banner with centered title.

    Args:
        title: The text to display centered in the banner.
        style: Color/style for the title text (default: cyan for headers,
               use green for completion banners).
        is_footer: If True, omit trailing blank line (for footer banners).

    Example:
        >>> show_banner("CREATE SSH HOST")                          # header
        >>> show_banner("OK", style="green", is_footer=True)        # footer
    """
    console = get_console()
    console.print()
    console.rule(f"[{style}]{title}[/{style}]", style="dim")
    if not is_footer:
        console.print()


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


def render_insertion_preview(
    new_config_block: str,
    before_host_lines: list[str] | None,
    after_host_lines: list[str] | None,
    target_filepath: str,
    *,
    max_context_lines: int = 3,
) -> None:
    """Render a diff-style preview panel showing host insertion context.

    Shows:
    - Previous host (truncated to max_context_lines)
    - New host config with green '+' prefix on each line
    - Next host (truncated to max_context_lines)

    Args:
        new_config_block: The SSH config block being added.
        before_host_lines: Config lines for the previous host (or None).
        after_host_lines: Config lines for the next host (or None).
        target_filepath: Path to the target config file.
        max_context_lines: Max lines to show for before/after hosts.
    """
    console = get_console()

    # Show filepath before panel
    console.print(f"[dim]Planning host entry addition to {target_filepath}...[/dim]")

    lines: list[str] = []

    # Before host (truncated)
    if before_host_lines:
        for i, line in enumerate(before_host_lines[:max_context_lines]):
            lines.append(f"[dim]{line.rstrip()}[/dim]")
        if len(before_host_lines) > max_context_lines:
            lines.append("[dim]  ...[/dim]")
        lines.append("")  # blank line

    # New host with green + prefix
    for line in new_config_block.split("\n"):
        if line.strip():
            lines.append(f"[green]+[/green] {line}")

    # After host (truncated)
    if after_host_lines:
        lines.append("")  # blank line
        for i, line in enumerate(after_host_lines[:max_context_lines]):
            lines.append(f"[dim]{line.rstrip()}[/dim]")
        if len(after_host_lines) > max_context_lines:
            lines.append("[dim]  ...[/dim]")

    # Build panel content
    content = "\n".join(lines)
    panel = Panel.fit(
        content,
        border_style="cyan",
        title="[bold]Host File Configuration[/bold]",
        title_align="left",
    )
    console.print(panel)


def render_removal_preview(host_lines: list[str]) -> None:
    """Render a diff-style preview panel for host removal.

    Shows the host config with red '-' prefix on each line.

    Args:
        host_lines: The SSH config lines for the host being removed.
    """
    console = get_console()

    config_lines = [line.rstrip() for line in host_lines if line.strip()]
    config_text = "\n".join(f"[red]-[/red] {line}" for line in config_lines)
    panel = Panel.fit(
        config_text,
        border_style="red",
        title="[bold]Host File Configuration[/bold]",
        title_align="left",
    )
    console.print(panel)
