"""Shared Typer application bootstrapping helpers."""

from __future__ import annotations

import os
import sys
import traceback
from typing import Callable, Sequence

from typing import TYPE_CHECKING

from nssh.core.ui.console import get_console

if TYPE_CHECKING:  # pragma: no cover - typing only
    pass


def _first_positional_index(args: list[str]) -> int | None:
    """Return the index of the first positional argument, if any."""

    for idx, arg in enumerate(args):
        if arg == "--":
            # Everything after -- is positional even if it starts with -
            return idx + 1 if idx + 1 < len(args) else None
        if not arg.startswith("-") and arg:
            return idx
    return None


def _is_global_help_request(args: list[str]) -> bool:
    """True if -h/--help occurs before the first positional argument."""

    help_indices = [idx for idx, arg in enumerate(args) if arg in {"-h", "--help"}]
    if not help_indices:
        return False

    first_positional = _first_positional_index(args)
    if first_positional is None:
        # Only options were provided, so treat help as top-level.
        return True

    return any(idx < first_positional for idx in help_indices)


def _should_handle_completion(prefix: str, cli_name: str | None) -> bool:
    env_keys: list[str] = []
    if prefix:
        env_keys.append(f"_NSSH_{prefix.upper()}_COMPLETE")

    if cli_name:
        canonical = cli_name.upper().replace("-", "_")
        env_keys.append(f"_{canonical}_COMPLETE")

    env_keys.append("_TYPER_COMPLETE")

    return any(
        any(key.startswith(candidate) for key in os.environ) for candidate in env_keys
    )


def run_cli(
    app,
    *,
    cli_name: str,
    usage_cb: Callable[[], None],
    completion_prefix: str,
    show_usage_if_no_args: bool = True,
    argv: Sequence[str] | None = None,
) -> None:
    """Execute a Typer CLI with shared completion/help/version handling."""

    if _should_handle_completion(completion_prefix, cli_name):
        app()
        return

    args = list(sys.argv[1:] if argv is None else argv)
    if _is_global_help_request(args):
        usage_cb()
        raise SystemExit(0)

    if show_usage_if_no_args and not args:
        usage_cb()
        raise SystemExit(1)

    console = get_console()
    try:
        # Pass args explicitly so Typer doesn't use sys.argv
        if argv is not None:
            app(args=args, standalone_mode=False)
        else:
            app()
    except KeyboardInterrupt:
        console.print("\n[yellow]Cancelled by user[/yellow]")
        raise SystemExit(0)
    except Exception as exc:  # pragma: no cover - defensive
        console.print(f"\n[red]Error: {exc}[/red]")
        traceback.print_exc()
        raise SystemExit(1)
