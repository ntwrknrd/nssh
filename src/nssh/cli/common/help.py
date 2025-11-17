"""Shared usage/help rendering utilities for nssh CLI packages."""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Sequence

from rich.console import RenderableType
from rich.padding import Padding
from rich.text import Text

from nssh.cli.common import ui
from nssh.core.ui.console import get_console


@dataclass(slots=True)
class UsageRow:
    """One labeled row inside a usage section."""

    label: str
    description: RenderableType | None = None
    examples: Sequence[str] = field(default_factory=list)
    description_style: str | None = "dim"
    example_prefix: str = "Example"


@dataclass(slots=True)
class UsageSection:
    """Container for a titled set of usage rows."""

    title: str
    rows: Sequence[UsageRow] = field(default_factory=list)
    body: RenderableType | None = None
    body_style: str | None = None


def _to_text(content: str, *, style: str | None = None) -> Text:
    text = Text.from_markup(content)
    if style:
        text.stylize(style)
    return text


def _print_description(text: RenderableType | None, *, style: str | None) -> None:
    if text is None:
        return

    console = get_console()
    if isinstance(text, str):
        console.print(Padding(_to_text(text, style=style), (0, 0, 0, 4)))
    else:
        console.print(Padding(text, (0, 0, 0, 4)))


def _print_examples(row: UsageRow) -> None:
    if not row.examples:
        return

    console = get_console()
    for example in row.examples:
        console.print(
            Padding(
                _to_text(f"{row.example_prefix}: {example}", style="dim"),
                (0, 0, 0, 4),
            )
        )


def render_usage(
    app_title: str,
    subtitle: str,
    sections: Sequence[UsageSection],
    *,
    footer: RenderableType | None = None,
) -> None:
    """Render the shared Rich layout for CLI help output."""

    ui.show_panel(app_title, subtitle, style="cyan")
    console = get_console()

    for section in sections:
        console.print(f"\n[bold]{section.title}:[/bold]")

        if section.body is not None:
            content: RenderableType
            if isinstance(section.body, str):
                content = _to_text(section.body, style=section.body_style)
            else:
                content = section.body
            console.print(Padding(content, (0, 0, 0, 2)))

        for row in section.rows:
            if row.label:
                console.print(f"  {row.label}")
            _print_description(row.description, style=row.description_style)
            _print_examples(row)

    if footer:
        console.print(f"\n{footer}")
