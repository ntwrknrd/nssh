"""Prompt helpers built on Typer/Rich primitives."""

from __future__ import annotations

from typing import Optional

import typer

from nssh.cli import Confirm, Prompt
from nssh.core.ui.console import get_console

__all__ = [
    "ask_text",
    "confirm",
    "prompt_required",
    "prompt_password_with_confirmation",
]


def ask_text(
    message: str,
    *,
    default: Optional[str] = None,
    allow_empty: bool = False,
    strip: bool = True,
) -> str:
    """Re-prompt for text until a non-empty value is supplied.

    Example:
        >>> project = ask_text("Project slug", default="demo")
    """
    while True:
        raw_value = Prompt.ask(message, default=default)
        if isinstance(raw_value, str):
            value = raw_value
        elif raw_value is None:
            value = ""
        else:
            value = str(raw_value)

        if strip:
            value = value.strip()
        if value or allow_empty:
            return value


def confirm(message: str, *, default: bool = True) -> bool:
    """Consistent confirmation prompt that returns ``True``/``False``."""
    return bool(Confirm.ask(message, default=default))


def prompt_password_with_confirmation(prompt_text: str) -> str:
    """
    Prompt for a password twice and ensure both entries match.

    Args:
        prompt_text: Message displayed for the first password prompt.

    Raises:
        ValueError: If the entered passwords do not match.
    """
    password = Prompt.ask(prompt_text, password=True)
    password_confirm = Prompt.ask("[cyan]Confirm password[/cyan]", password=True)
    if password != password_confirm:
        raise ValueError("Passwords do not match")
    return password


def prompt_required(
    prompt_text: str, value: Optional[str] = None, error_msg: Optional[str] = None
) -> str:
    """
    Prompt for a non-empty value and exit with an error if validation fails.

    Args:
        prompt_text: Message displayed to the user.
        value: Optional existing value to validate instead of prompting.
        error_msg: Optional override for the displayed error message.

    Returns:
        The validated value.
    """
    final_value = value
    if not final_value:
        final_value = Prompt.ask(f"[cyan]{prompt_text}[/cyan]").strip()

    if not final_value:
        console = get_console()
        error = error_msg or f"{prompt_text} is required"
        console.print(f"[red]Error: {error}[/red]")
        raise typer.Exit(1)

    return final_value
