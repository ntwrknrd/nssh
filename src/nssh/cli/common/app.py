"""Shared Click application bootstrapping helpers."""

from __future__ import annotations

import os
import sys
import traceback
from typing import Callable, Sequence

from click.exceptions import Abort, ClickException

from nssh.core.ui.console import get_console


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


def _get_completion_prog_name(cli_name: str) -> str:
    """Derive the prog_name used for Click's completion env var.

    Click uses _{PROG_NAME}_COMPLETE where PROG_NAME is the uppercase
    program name with spaces/dashes replaced by underscores.
    """
    return cli_name.lower().replace(" ", "-")


def _should_handle_completion(cli_name: str) -> bool:
    """Check if shell completion is being requested.

    Click uses _{PROG_NAME}_COMPLETE environment variables for completion.
    The prog_name is derived from cli_name (e.g., "nssh benchmark" -> "nssh-benchmark").
    """
    prog_name = _get_completion_prog_name(cli_name)
    canonical = prog_name.upper().replace("-", "_")
    env_prefix = f"_{canonical}_COMPLETE"

    return any(key.startswith(env_prefix) for key in os.environ)


def run_cli(
    app,
    *,
    cli_name: str,
    usage_cb: Callable[[], None],
    show_usage_if_no_args: bool = True,
    argv: Sequence[str] | None = None,
) -> None:
    """Execute a Click CLI with shared completion/help/version handling.

    Args:
        app: Click group/command to execute
        cli_name: Display name (e.g., "nssh benchmark") - also used to derive
                  the prog_name for shell completion (e.g., "nssh-benchmark")
        usage_cb: Callback to display custom help/usage
        show_usage_if_no_args: Show usage when no args provided
        argv: Override sys.argv[1:] for testing
    """
    # Handle completion
    if _should_handle_completion(cli_name):
        prog_name = _get_completion_prog_name(cli_name)
        app(prog_name=prog_name, standalone_mode=True)
        return

    args = list(sys.argv[1:] if argv is None else argv)
    if _is_global_help_request(args):
        usage_cb()
        raise SystemExit(0)

    if show_usage_if_no_args and not args:
        usage_cb()
        raise SystemExit(1)

    try:
        prog_name = _get_completion_prog_name(cli_name)
        app(args=args, prog_name=prog_name, standalone_mode=False)
    except (KeyboardInterrupt, Abort):
        raise SystemExit(130)
    except SystemExit:
        raise
    except ClickException as exc:
        get_console().print(f"\n[red]Error: {exc.format_message()}[/red]")
        raise SystemExit(exc.exit_code)
    except Exception as exc:  # pragma: no cover - defensive
        get_console().print(f"\n[red]Error: {exc}[/red]")
        traceback.print_exc()
        raise SystemExit(1)
