"""Selection helpers (fzf, include files, etc.)."""

from __future__ import annotations

from pathlib import Path
from typing import Iterable, List

from typer import Exit

from nssh.core.ssh.config import SSHConfigParser
from nssh.core.ui.console import get_console
from nssh.core.ui.fzf import check_fzf, fzf_select


def require_fzf() -> None:
    """Ensure ``fzf`` is installed before continuing."""
    if not check_fzf():
        console = get_console()
        console.print(
            "[red]Error: 'fzf' not found. Install with: brew install fzf[/red]"
        )
        raise Exit(1)


def select_via_fzf(options: Iterable[str], prompt: str) -> str:
    """Launch ``fzf`` with ``options`` and return the selected string.

    Example:
        >>> hostname = select_via_fzf(sorted(hosts), "Select host:")
    """
    require_fzf()
    values: List[str] = list(options)
    result = fzf_select(values, prompt)
    if not result:
        raise Exit(0)
    return result


def select_include_file(
    parser: SSHConfigParser,
    file_arg: str | None = None,
    prompt: str = "Select config file:",
    *,
    allow_all: bool = False,
) -> Path | List[Path]:
    """Select an Include file from ~/.ssh using fzf or a provided name."""

    console = get_console()

    if file_arg:
        file_path = Path.home() / ".ssh" / file_arg
        if not file_path.exists():
            console.print(f"[red]Error: File not found: {file_path}[/red]")
            raise Exit(1)
        return [file_path] if allow_all else file_path

    include_files = parser.find_include_files()
    if not include_files:
        console.print("[red]Error: No Include files found in ~/.ssh/config[/red]")
        raise Exit(1)

    require_fzf()

    if allow_all:
        options = ["[All files]"] + [str(path) for path in include_files]
    else:
        options = [str(path) for path in include_files]

    selected = fzf_select(options, prompt)
    if not selected:
        console.print("[yellow]Cancelled[/yellow]")
        raise Exit(0)

    if allow_all and selected == "[All files]":
        return include_files

    chosen_path = Path(selected)
    return [chosen_path] if allow_all else chosen_path
