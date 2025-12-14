"""Prompt helpers built on Click/Rich primitives."""

from __future__ import annotations

import sys
from typing import Callable, Optional

from nssh.cli import Prompt
from nssh.core.ui.console import get_console

__all__ = [
    "ask_text",
    "ask_with_fzf",
    "confirm",
    "is_interactive",
    "prompt_required",
    "prompt_password_with_confirmation",
]


def is_interactive() -> bool:
    """Check if stdin is connected to a TTY (interactive terminal).

    Returns:
        True if running interactively, False if stdin is piped/redirected.
    """
    return sys.stdin.isatty()


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
        raw_value = Prompt.ask(f"[cyan]?[/cyan] {message}", default=default)
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


def ask_with_fzf(
    message: str,
    *,
    default: Optional[str] = None,
    fzf_choices: Optional[list[str]] = None,
    fzf_prompt: str = "Select:",
    fzf_callback: Optional[Callable[[], list[str]]] = None,
) -> str:
    """Prompt with optional Tab-triggered fzf selection.

    Displays a prompt with default value. User can:
    - Press Enter to accept default
    - Type a value and press Enter
    - Press Tab to launch fzf browser (if choices provided)

    Args:
        message: Prompt message (default shown in parentheses).
        default: Default value if user presses Enter.
        fzf_choices: List of choices for fzf (enables Tab behavior).
        fzf_prompt: Prompt shown in fzf.
        fzf_callback: Optional callable that returns fzf choices dynamically.

    Returns:
        Selected or entered value.
    """
    # If no fzf choices and no callback, fall back to simple prompt
    if not fzf_choices and not fzf_callback:
        return ask_text(message, default=default)

    # If not interactive (e.g., in tests or piped input), use default
    if not is_interactive():
        return default or ""

    # Tab-browsing requires Unix TTY (tty/termios) - fall back on Windows
    import platform

    if platform.system() == "Windows":
        return ask_text(message, default=default)

    import tty
    import termios

    console = get_console()

    # Build prompt text
    if default:
        prompt_text = f"[cyan]?[/cyan] {message} ({default}) [Tab=browse]: "
    else:
        prompt_text = f"[cyan]?[/cyan] {message}: "

    # Print prompt without newline
    console.print(prompt_text, end="")

    # Read input character by character
    fd = sys.stdin.fileno()
    old_settings = termios.tcgetattr(fd)

    buffer = ""
    try:
        tty.setraw(fd)

        while True:
            char = sys.stdin.read(1)

            # Tab key (ASCII 9)
            if char == "\t":
                # Restore terminal settings before fzf
                termios.tcsetattr(fd, termios.TCSADRAIN, old_settings)

                # Clear the line and show fzf selection
                sys.stdout.write("\r\033[K")
                sys.stdout.flush()

                # Get choices (from callback or list)
                choices = fzf_callback() if fzf_callback else fzf_choices

                if choices:
                    from nssh.core.ui.fzf import fzf_select

                    selected = fzf_select(choices, fzf_prompt)
                    if selected:
                        # Re-print prompt with selection
                        console.print(f"[cyan]?[/cyan] {message}: {selected}")
                        return selected
                    else:
                        # User cancelled fzf, re-show prompt
                        console.print(prompt_text, end="")
                        tty.setraw(fd)
                        continue
                else:
                    # No choices, just continue
                    tty.setraw(fd)
                    continue

            # Enter key
            elif char in ("\r", "\n"):
                termios.tcsetattr(fd, termios.TCSADRAIN, old_settings)
                sys.stdout.write("\n")
                sys.stdout.flush()

                result = buffer.strip() if buffer else (default or "")
                return result

            # Backspace
            elif char in ("\x7f", "\x08"):
                if buffer:
                    buffer = buffer[:-1]
                    sys.stdout.write("\b \b")
                    sys.stdout.flush()

            # Ctrl+C
            elif char == "\x03":
                termios.tcsetattr(fd, termios.TCSADRAIN, old_settings)
                raise KeyboardInterrupt

            # Printable characters
            elif char.isprintable():
                buffer += char
                sys.stdout.write(char)
                sys.stdout.flush()

    finally:
        termios.tcsetattr(fd, termios.TCSADRAIN, old_settings)


def confirm(message: str, *, default: bool = True) -> bool:
    """Consistent confirmation prompt that returns ``True``/``False``."""
    default_hint = "Y/n" if default else "y/N"
    response = Prompt.ask(f"[cyan]?[/cyan] {message}", default=default_hint)
    if response == default_hint:
        return default
    return response.lower() in ("y", "yes")


def prompt_password_with_confirmation(
    prompt_text: str, *, allow_empty: bool = False
) -> str:
    """
    Prompt for a password twice and ensure both entries match.

    Args:
        prompt_text: Message displayed for the first password prompt.
        allow_empty: If True, allow empty password (skips confirmation).

    Raises:
        ValueError: If the entered passwords do not match.
    """
    password = Prompt.ask(f"[cyan]?[/cyan] {prompt_text}", password=True)
    if allow_empty and not password:
        return ""
    password_confirm = Prompt.ask("[cyan]?[/cyan] Confirm password", password=True)
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
        final_value = Prompt.ask(f"[cyan]?[/cyan] {prompt_text}").strip()

    if not final_value:
        console = get_console()
        error = error_msg or f"{prompt_text} is required"
        console.print(f"[red]Error: {error}[/red]")
        raise SystemExit(1)

    return final_value
