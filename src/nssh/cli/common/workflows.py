"""Higher level prompt workflows shared across CLI packages."""

from __future__ import annotations

from typing import Literal

from nssh.cli.common.prompt import confirm
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
    raise SystemExit(0)


def choose_password_source(
    *,
    context_name: str | None,
    has_context_credentials: bool,
    skip_prompt: bool,
) -> Literal["context", "custom", "skip"]:
    """Return how password credentials should be sourced."""
    if skip_prompt:
        return "context" if has_context_credentials else "skip"

    if has_context_credentials and context_name:
        if confirm("Use context credentials?", default=True):
            return "context"
        return "custom"

    # No context credentials - ask if user wants to enter password or skip
    if confirm("Store password? (no = key-based auth)", default=True):
        return "custom"
    return "skip"
