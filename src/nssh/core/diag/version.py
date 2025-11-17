"""Shared helpers for reporting the package version on CLI entrypoints."""

from __future__ import annotations

import sys
from pathlib import Path
from typing import Iterable, Sequence

from nssh import __version__


def version_string(cli_name: str | None = None) -> str:
    """Return a formatted ``<command> <version>`` string."""

    if cli_name:
        name = cli_name
    else:
        raw = sys.argv[0] if sys.argv else ""
        name = Path(raw).name or "nssh"
    return f"{name} {__version__}"


def _normalize_argv(argv: Sequence[str] | None) -> Sequence[str]:
    if argv is None:
        return sys.argv[1:]
    return argv


def maybe_print_version_and_exit(
    argv: Sequence[str] | None = None,
    *,
    cli_name: str | None = None,
    flags: Iterable[str] = ("--version", "-V", "-v"),
) -> None:
    """Print the version string and exit if any flag is present."""

    args = list(_normalize_argv(argv))
    for flag in flags:
        if flag in args:
            print(version_string(cli_name))
            raise SystemExit(0)
