#!/usr/bin/env python3
"""System utilities and path helpers for self."""

from __future__ import annotations

import os
import shutil
import sys
from pathlib import Path

from nssh.core.ui.console import get_console

console = get_console()


def xdg_config_home() -> Path:
    """Get XDG_CONFIG_HOME or default to ~/.config."""
    raw = os.environ.get("XDG_CONFIG_HOME")
    return Path(raw) if raw else Path.home() / ".config"


def rel_home(path: Path) -> str:
    """Convert absolute path to relative path from home directory using ~."""
    text = str(path)
    home = str(Path.home())
    if text.startswith(home):
        return text.replace(home, "~", 1)
    return text


def check_nssh_on_path() -> bool:
    """Check if nssh is available on PATH."""
    return shutil.which("nssh") is not None


def check_system_dependencies() -> None:
    """Verify required system packages are installed.

    Exits with error message if any required dependencies are missing.
    """
    missing = []

    # Check for age
    if shutil.which("age") is None:
        missing.append("age")

    # Check for fzf
    if shutil.which("fzf") is None:
        missing.append("fzf")

    # Check for Python 3.14+
    python_ok = False
    if sys.version_info >= (3, 14):
        python_ok = True
    else:
        # Check if python3.14 or higher is available on PATH
        for minor in range(14, 20):  # Check up to Python 3.19
            if shutil.which(f"python3.{minor}") is not None:
                python_ok = True
                break

    if not python_ok:
        missing.append("python3.14+")

    if missing:
        console.print(
            "[red]✗[/red] Missing required system dependencies:", style="bold"
        )
        console.print()
        for dep in missing:
            console.print(f"  [red]✗[/red] {dep}")
        console.print()
        console.print("[yellow]Installation instructions:[/yellow]")
        console.print()

        if "age" in missing:
            console.print("  age: https://age-encryption.org")
        if "fzf" in missing:
            console.print("  fzf: https://github.com/junegunn/fzf")
        if "python3.14+" in missing:
            console.print("  Python 3.14+: uv python install 3.14")

        console.print()
        console.print("[red]Please install missing dependencies and try again.[/red]")
        sys.exit(1)
