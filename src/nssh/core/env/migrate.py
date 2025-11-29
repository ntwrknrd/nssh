"""Migrate files from legacy ~/.ssh/ locations to XDG-compliant paths."""

from __future__ import annotations

import shutil
from pathlib import Path
from typing import List, Tuple

from rich.prompt import Confirm

from nssh.core.env.settings import default_data_root, default_state_root
from nssh.core.ui.console import get_console

# Legacy paths -> (xdg_type, new_name)
# xdg_type: "state" = default_state_root(), "data" = default_data_root()
LEGACY_MAPPINGS: List[Tuple[str, str, str]] = [
    ("~/.ssh/.nssh_host_index", "state", "host_index"),
    ("~/.ssh/nssh_credentials.age", "data", "credentials.age"),
    ("~/.ssh/backups", "data", "backups"),
]


def migrate_legacy_files(*, prompt_overwrite: bool = True) -> List[Tuple[Path, Path]]:
    """Move files from legacy ~/.ssh/ locations to XDG paths.

    Checks each legacy location and moves to the new XDG-compliant path.
    If a file already exists at the new location, prompts the user to overwrite.

    Args:
        prompt_overwrite: If True, prompt user when new location exists.
                         If False, skip files that already exist at new location.

    Returns:
        List of (old_path, new_path) tuples for files that were migrated.
    """
    console = get_console()
    migrated: List[Tuple[Path, Path]] = []

    for legacy_path, xdg_type, new_name in LEGACY_MAPPINGS:
        old = Path(legacy_path).expanduser()
        if not old.exists():
            continue

        # Determine new path based on XDG type
        if xdg_type == "state":
            new = default_state_root() / new_name
        else:  # data
            new = default_data_root() / new_name

        # Handle existing file at new location
        if new.exists():
            if not prompt_overwrite:
                continue

            # Show file sizes to help user decide
            old_size = _get_size_str(old)
            new_size = _get_size_str(new)
            console.print(f"[yellow]![/yellow] '{new_name}' exists at both locations:")
            console.print(f"  [dim]Old:[/dim] {old} ({old_size})")
            console.print(f"  [dim]New:[/dim] {new} ({new_size})")

            if not Confirm.ask(
                "  Overwrite new location with legacy file?", default=False
            ):
                console.print("  [dim]Skipped[/dim]")
                continue

            # Remove existing file/directory at new location
            if new.is_dir():
                shutil.rmtree(new)
            else:
                new.unlink()

        # Create parent directory if needed
        new.parent.mkdir(parents=True, exist_ok=True)

        # Move file or directory
        shutil.move(str(old), str(new))
        migrated.append((old, new))

    return migrated


def _get_size_str(path: Path) -> str:
    """Get human-readable size string for a file or directory."""
    if path.is_dir():
        total = sum(f.stat().st_size for f in path.rglob("*") if f.is_file())
        count = sum(1 for f in path.rglob("*") if f.is_file())
        return f"{count} files, {_format_bytes(total)}"
    else:
        return _format_bytes(path.stat().st_size)


def _format_bytes(size: int) -> str:
    """Format bytes as human-readable string."""
    for unit in ["B", "KB", "MB", "GB"]:
        if size < 1024:
            return f"{size:.0f}{unit}" if unit == "B" else f"{size:.1f}{unit}"
        size /= 1024
    return f"{size:.1f}TB"
