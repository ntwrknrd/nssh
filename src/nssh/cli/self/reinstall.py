#!/usr/bin/env python3
"""Reinstall command for self - clean reinstall and refresh files."""

from __future__ import annotations

import shutil
import subprocess
from datetime import datetime
from importlib import resources
from pathlib import Path
from typing import Optional

from nssh.cli import click
from nssh.cli.self.assets import get_asset, write_file
from nssh.cli.self.manifest import read_manifest, write_manifest
from nssh.cli.self.validation import validate_in_nssh_directory
from nssh.cli.common.banner import FAIL, OK, WARN, banner
from nssh.core.env.migrate import migrate_legacy_files
from nssh.core.env.paths import share_assets_dir
from nssh.core.ui.console import get_console

console = get_console()


def clear_python_cache() -> bool:
    """Clear Python __pycache__ directories and .pyc files.

    Returns:
        True if successful, False otherwise.
    """
    try:
        subprocess.run(
            [
                "find",
                ".",
                "-type",
                "d",
                "-name",
                "__pycache__",
                "-exec",
                "rm",
                "-rf",
                "{}",
                "+",
            ],
            cwd=Path.cwd(),
            capture_output=True,
            timeout=10,
        )
        subprocess.run(
            ["find", ".", "-type", "f", "-name", "*.pyc", "-delete"],
            cwd=Path.cwd(),
            capture_output=True,
            timeout=10,
        )
        return True
    except Exception:
        return False


def detect_dev_directory() -> Optional[Path]:
    """Detect if we're in a development environment.

    Returns:
        Path to dev directory if pyproject.toml found, None otherwise
    """
    check_path = Path.cwd()
    for _ in range(5):  # Check up to 5 levels up
        if (check_path / "pyproject.toml").exists():
            return check_path
        if check_path == check_path.parent:
            break
        check_path = check_path.parent
    return None


def bump_version(pyproject_path: Path) -> Optional[str]:
    """Bump the patch version in pyproject.toml.

    Args:
        pyproject_path: Path to pyproject.toml file

    Returns:
        Version change string (e.g., "1.9.64 -> 1.9.65") or None if failed.
    """
    content = pyproject_path.read_text()
    lines = content.splitlines()

    for i, line in enumerate(lines):
        if line.strip().startswith("version = "):
            current = line.split('"')[1]
            parts = current.split(".")
            if len(parts) == 3:
                major, minor, patch = parts
                new_patch = int(patch) + 1
                new_version = f"{major}.{minor}.{new_patch}"
                lines[i] = f'version = "{new_version}"'
                pyproject_path.write_text("\n".join(lines) + "\n")
                return f"{current} -> {new_version}"
    return None


def get_installed_version() -> Optional[str]:
    """Get the currently installed nssh version.

    Returns:
        Version string or None if not found.
    """
    result = subprocess.run(
        ["uv", "tool", "list"],
        capture_output=True,
        text=True,
    )
    if result.returncode == 0:
        for line in result.stdout.splitlines():
            if line.startswith("nssh "):
                # Format: "nssh v1.9.65"
                parts = line.split()
                if len(parts) >= 2:
                    return parts[1].lstrip("v")
    return None


def reinstall_package(dev_dir: Path) -> tuple[bool, str]:
    """Reinstall the nssh package using uv.

    Args:
        dev_dir: Development directory containing pyproject.toml

    Returns:
        Tuple of (success, message).
    """
    if not shutil.which("uv"):
        return False, "uv not found"

    result = subprocess.run(
        ["uv", "tool", "list"],
        capture_output=True,
        text=True,
    )

    if result.returncode == 0 and "nssh" in result.stdout:
        subprocess.run(["uv", "tool", "uninstall", "nssh"], capture_output=True)

        install_result = subprocess.run(
            ["uv", "tool", "install", ".", "--force", "--reinstall"],
            cwd=dev_dir,
            capture_output=True,
            text=True,
        )
        if install_result.returncode != 0:
            return False, install_result.stderr.strip()
        return True, "done"
    else:
        return False, "not installed via uv tool"


def refresh_tracked_files(manifest) -> tuple[int, list[str]]:
    """Refresh all tracked files from assets.

    Args:
        manifest: Install manifest with tracked files

    Returns:
        Tuple of (count refreshed, list of errors).
    """
    count = 0
    errors: list[str] = []

    for file_entry in manifest.files:
        file_path = Path(file_entry.path)
        if file_entry.source:
            category, name = file_entry.source.split("/", 1)
            try:
                with resources.as_file(get_asset(category, name)) as src_path:
                    write_file(src_path, file_path, executable=True)
                count += 1
            except FileNotFoundError:
                errors.append(f"Source not found: {file_entry.source}")

    return count, errors


@click.command(short_help="Re-install nssh")
@click.option("--dev", is_flag=True, default=False, help="Bump version first")
def reinstall_command(dev: bool) -> None:
    """Reinstall nssh - clear cache and reinstall package (if dev mode)."""
    share_dir = share_assets_dir()
    validate_in_nssh_directory()

    with banner("RE-INSTALL NSSH", OK) as set_outcome:
        _reinstall(share_dir, dev, set_outcome)


def _reinstall(share_dir, dev: bool, set_outcome) -> None:
    """Internal implementation for reinstall."""
    manifest = read_manifest(share_dir)
    if manifest is None:
        console.print("[red]Error:[/red] No manifest found")
        console.print("[dim]Run 'nssh self install' first[/dim]")
        set_outcome(FAIL)
        raise SystemExit(1)

    # Step 1: Clear cache
    success = clear_python_cache()
    if success:
        console.print("[green]✓[/green] Cache cleared")
    else:
        console.print("[yellow]![/yellow] Cache clear failed")

    # Step 2: Reinstall package (if dev environment)
    dev_dir = detect_dev_directory()

    if dev_dir:
        # Capture version before reinstall
        old_version = get_installed_version()

        if dev:
            pyproject_path = dev_dir / "pyproject.toml"
            bump_version(pyproject_path)

        success, msg = reinstall_package(dev_dir)
        if success:
            # Capture version after reinstall
            new_version = get_installed_version()
            if old_version and new_version and old_version != new_version:
                console.print(
                    f"[green]✓[/green] Package reinstalled ({old_version} -> {new_version})"
                )
            else:
                console.print("[green]✓[/green] Package reinstalled")
        else:
            console.print(f"[dim]-[/dim] Package reinstall: [dim]skipped ({msg})[/dim]")
    else:
        console.print(
            "[dim]-[/dim] Package reinstall: [dim]skipped (no dev environment)[/dim]"
        )

    # Step 3: Migrate legacy files to XDG locations
    migrated = migrate_legacy_files()
    if migrated:
        console.print(
            f"[green]+[/green] Migrated {len(migrated)} files to XDG locations:"
        )
        for old, new in migrated:
            console.print(f"  [dim]{old} -> {new}[/dim]")
    else:
        console.print("[dim]-[/dim] Migration: [dim]no legacy files to migrate[/dim]")

    # Step 4: Refresh tracked files
    count, errors = refresh_tracked_files(manifest)
    if errors:
        console.print(
            f"[yellow]![/yellow] Refreshed {count}/{len(manifest.files)} files"
        )
        for err in errors:
            console.print(f"  [red]![/red] {err}")
        set_outcome(WARN)
    else:
        console.print(f"[green]✓[/green] Refreshed {count} files")

    # Update manifest
    manifest.installed_at = datetime.now().isoformat()
    write_manifest(manifest, share_dir)
