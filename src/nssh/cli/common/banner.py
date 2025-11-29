"""CLI banner context manager for consistent header/footer styling.

This module provides a context manager and decorator for wrapping CLI commands
with consistent header and footer banners. Use this to enforce uniform styling
across all commands.

Examples:
    # Context manager for conditional footers
    with banner("BATCH IMPORT", OK) as set_outcome:
        if dry_run:
            set_outcome(NOOP)
        elif failed:
            set_outcome(FAIL)

    # Decorator for simple commands
    @with_banner("LIST HOSTS")
    def list_command(): ...
"""

from __future__ import annotations

import functools
from contextlib import contextmanager
from dataclasses import dataclass
from typing import TYPE_CHECKING, Callable, Iterator, Literal, TypeVar

if TYPE_CHECKING:
    from typing import ParamSpec

    P = ParamSpec("P")
    R = TypeVar("R")

import click

from nssh.cli.common.ui import show_banner as _show_banner
from nssh.core.ui.console import get_console

BannerStyle = Literal["cyan", "green", "yellow", "red"]


class Styles:
    """Centralized banner styles for bulk changes."""

    HEADER: BannerStyle = "cyan"
    SUCCESS: BannerStyle = "green"
    WARNING: BannerStyle = "yellow"
    ERROR: BannerStyle = "red"


@dataclass(frozen=True, slots=True)
class Outcome:
    """Footer outcome with message and style."""

    message: str
    style: BannerStyle = "green"


# Pre-defined outcomes (Unix-style terse status codes)
OK = Outcome("OK", Styles.SUCCESS)
NOOP = Outcome("NO-OP", Styles.SUCCESS)
ABORT = Outcome("ABORT", Styles.WARNING)
FAIL = Outcome("FAIL", Styles.ERROR)
WARN = Outcome("WARN", Styles.WARNING)


@contextmanager
def banner(
    header: str,
    footer: str | Outcome | None = None,
) -> Iterator[Callable[[Outcome], None]]:
    """Context manager for command banners with header/footer.

    Args:
        header: Header title text displayed at command start.
        footer: Default footer (string or Outcome). None = no footer.

    Yields:
        set_outcome: Function to override footer dynamically based on result.

    Examples:
        # Simple: header + default footer
        with banner("ADD SSH HOST", OK):
            ...

        # Read-only: header only, no footer
        with banner("LIST SSH HOSTS", footer=None):
            ...

        # Conditional: set footer based on result
        with banner("BATCH IMPORT", OK) as set_outcome:
            if dry_run:
                set_outcome(NOOP)
            elif failed:
                set_outcome(FAIL)
    """
    _show_banner(header, style=Styles.HEADER)

    outcome: Outcome | None = None

    def set_outcome(new: Outcome) -> None:
        nonlocal outcome
        outcome = new

    try:
        yield set_outcome
    except KeyboardInterrupt:
        get_console().print()  # Push ^C onto its own line
        outcome = ABORT
        # Don't re-raise - exit in finally to avoid Click's exception handling
    except click.Abort:
        outcome = ABORT
        # Don't re-raise - exit in finally to avoid Click's exception handling
    except click.ClickException:
        outcome = FAIL
        raise
    finally:
        final = outcome or (
            footer
            if isinstance(footer, Outcome)
            else Outcome(footer, Styles.SUCCESS) if footer else None
        )
        if final:
            _show_banner(final.message, style=final.style, is_footer=True)
        # Exit directly for abort cases to avoid Click adding trailing newlines
        if outcome is ABORT:
            raise SystemExit(130)


def with_banner(
    header: str,
    footer: str | Outcome | None = None,
) -> Callable[[Callable[P, R]], Callable[P, R]]:
    """Decorator for simple commands with fixed header/footer.

    Use this for commands that don't need conditional footers.
    For commands with conditional logic, use the banner() context manager.

    Args:
        header: Header title text displayed at command start.
        footer: Default footer (string or Outcome). None = no footer.

    Returns:
        Decorator that wraps the function with banner context.

    Examples:
        @click.command()
        @with_banner("LIST SSH HOSTS")  # header only
        def list_command(...): ...

        @click.command()
        @with_banner("ADD CONTEXT", OK)  # header + footer
        def add_command(...): ...
    """

    def decorator(func: Callable[P, R]) -> Callable[P, R]:
        @functools.wraps(func)
        def wrapper(*args: P.args, **kwargs: P.kwargs) -> R:
            with banner(header, footer):
                return func(*args, **kwargs)

        return wrapper

    return decorator


__all__ = [
    "BannerStyle",
    "Styles",
    "Outcome",
    "OK",
    "NOOP",
    "ABORT",
    "FAIL",
    "WARN",
    "banner",
    "with_banner",
]
