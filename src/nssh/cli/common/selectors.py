"""Selection helpers (fzf, include files, etc.)."""

from __future__ import annotations

from pathlib import Path
from typing import Iterable, List

from nssh.core.auth.credentials import CredentialManager
from nssh.core.env.paths import ssh_include_dir
from nssh.core.ssh.config import SSHConfigParser
from nssh.core.ui.console import get_console
from nssh.core.ui.fzf import check_fzf
from nssh.core.ui.fzf import fzf_select as _fzf_select_single
from nssh.core.ui.fzf import fzf_select_multi as _fzf_select_multi


class FzfCancelled(Exception):
    """Raised when user cancels fzf selection (Escape/Ctrl-C)."""

    pass


def require_fzf() -> None:
    """Ensure ``fzf`` is installed before continuing."""
    if not check_fzf():
        console = get_console()
        console.print(
            "[red]Error: 'fzf' not found. Install with: brew install fzf[/red]"
        )
        raise SystemExit(1)


def fzf_select(
    options: Iterable[str],
    prompt: str = "Select:",
    *,
    multi: bool = False,
    exit_on_cancel: bool = True,
) -> List[str]:
    """Launch fzf and return selected options.

    Args:
        options: Items to display in fzf.
        prompt: Prompt text shown in fzf.
        multi: Enable multi-select mode (Tab to toggle selections).
        exit_on_cancel: If True (default), exit(0) on cancel; else raise FzfCancelled.

    Returns:
        List of selected strings. Single-select returns ``[item]``.

    Raises:
        SystemExit(1): If fzf is not installed.
        SystemExit(0): If cancelled and ``exit_on_cancel=True``.
        FzfCancelled: If cancelled and ``exit_on_cancel=False``.

    Example:
        >>> [host] = fzf_select(sorted(hosts), "Select host:")
        >>> paths = fzf_select(files, "Select files:", multi=True)
    """
    require_fzf()
    values: List[str] = list(options)

    if multi:
        result = _fzf_select_multi(values, prompt)
    else:
        single = _fzf_select_single(values, prompt)
        result = [single] if single else []

    if not result:
        if exit_on_cancel:
            raise SystemExit(0)
        raise FzfCancelled()

    return result


def select_include_file(
    parser: SSHConfigParser,
    context_arg: str | None = None,
    prompt: str = "Select config file:",
    *,
    allow_all: bool = False,
) -> Path | List[Path]:
    """Select an Include file using context-aware fzf selection.

    Prioritizes files in the configured include_dir, showing context names.
    Allows creating new context files.
    """

    console = get_console()

    if context_arg:
        # Resolve context name to SSH config file
        cm = CredentialManager()
        contexts = cm.list_contexts()

        # Find context by name
        context = None
        for ctx in contexts:
            if ctx.get("name") == context_arg:
                context = ctx
                break

        if not context:
            console.print(f"[red]Error: Context '{context_arg}' not found[/red]")
            raise SystemExit(1)

        git_include_file = context.get("git_include_file")
        if not git_include_file:
            console.print(
                f"[red]Error: Context '{context_arg}' has no git_include_file[/red]"
            )
            raise SystemExit(1)

        file_path = ssh_include_dir() / git_include_file
        if not file_path.exists():
            console.print(f"[red]Error: File not found: {file_path}[/red]")
            raise SystemExit(1)
        return [file_path] if allow_all else file_path

    include_files = parser.find_include_files()
    include_dir = ssh_include_dir()

    # Separate files into context files (in include_dir) and other files
    context_files: List[tuple[str, Path]] = []
    other_files: List[tuple[str, Path]] = []

    for file_path in include_files:
        if file_path.parent == include_dir:
            # This is a context-specific file - show friendly name
            context_name = file_path.stem  # e.g., "work_hosts" -> "work"
            context_files.append((context_name, file_path))
        else:
            # Non-context file - show filename
            other_files.append((file_path.name, file_path))

    # Build fzf options
    options_map: dict[str, Path] = {}
    fzf_options: List[str] = []

    if allow_all and include_files:
        fzf_options.append("[All files]")

    # Add context files first with "(context)" label
    for context_name, file_path in context_files:
        option_str = f"{context_name} (context)"
        fzf_options.append(option_str)
        options_map[option_str] = file_path

    # Add other files
    for filename, file_path in other_files:
        fzf_options.append(filename)
        options_map[filename] = file_path

    # Add option to create new context file
    if not allow_all:
        fzf_options.append("+ Create new context file")

    if not fzf_options:
        console.print("[red]Error: No Include files found in SSH config[/red]")
        raise SystemExit(1)

    try:
        [selected] = fzf_select(fzf_options, prompt, exit_on_cancel=False)
    except FzfCancelled:
        console.print("[yellow]Cancelled[/yellow]")
        raise SystemExit(0)

    if allow_all and selected == "[All files]":
        return include_files

    if selected == "+ Create new context file":
        from nssh.cli.common.prompt import ask_text

        context_name = ask_text("Context name (e.g., 'work', 'homelab')")
        new_file = include_dir / f"{context_name}_hosts"
        return new_file

    chosen_path = options_map.get(selected)
    if not chosen_path:
        console.print(f"[red]Error: Invalid selection: {selected}[/red]")
        raise SystemExit(1)

    return [chosen_path] if allow_all else chosen_path
