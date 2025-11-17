"""Higher level prompt workflows shared across CLI packages."""

from __future__ import annotations

from typing import Literal

from nssh.cli import typer
from nssh.cli.common.prompt import confirm
from nssh.cli.common.selectors import select_via_fzf
from nssh.core.ui.console import get_console


def confirm_or_exit(
    message: str,
    *,
    default: bool = True,
    cancel_message: str = "[yellow]Cancelled[/yellow]",
) -> None:
    """Standard confirmation gate that exits early when declined."""
    if confirm(message, default=default):
        return
    console = get_console()
    console.print(cancel_message)
    raise typer.Exit(0)


def choose_password_source(
    *,
    context_name: str | None,
    has_context_credentials: bool,
    skip_prompt: bool,
) -> Literal["context", "custom", "skip"]:
    """Return how password credentials should be sourced."""
    if skip_prompt:
        return "context" if has_context_credentials else "skip"

    option_map: dict[str, Literal["context", "custom"]] = {}
    if has_context_credentials and context_name:
        context_option = f"context - Use context '{context_name}' credentials"
        option_map[context_option] = "context"
    custom_option = "custom - Custom password (prompt and store)"
    option_map[custom_option] = "custom"

    selected = select_via_fzf(list(option_map.keys()), "Select password option:")
    return option_map[selected]
