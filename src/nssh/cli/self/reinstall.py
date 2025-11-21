#!/usr/bin/env python3
"""Reinstall command for self - clean reinstall and refresh files."""

from __future__ import annotations

import shutil
import subprocess
from datetime import datetime
from importlib import resources
from pathlib import Path
from typing import Annotated, Optional

from nssh.cli import typer
from nssh.cli.self.assets import get_asset, write_file
from nssh.cli.self.manifest import read_manifest, write_manifest
from nssh.cli.self.validation import validate_in_nssh_directory
from nssh.cli.common import ui
from nssh.core.env.paths import share_assets_dir
from nssh.core.ui.console import get_console

console = get_console()


def clear_python_cache() -> None:
    """Clear Python __pycache__ directories and .pyc files."""
    console.print("[cyan]Step 1/3:[/cyan] Clearing Python cache...")

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
        # Also clear .pyc files
        subprocess.run(
            ["find", ".", "-type", "f", "-name", "*.pyc", "-delete"],
            cwd=Path.cwd(),
            capture_output=True,
            timeout=10,
        )
        console.print("[green]✓[/green] Cache cleared")
    except Exception as exc:
        console.print(f"[yellow]![/yellow] Cache clear failed: {exc}")


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


def bump_version(pyproject_path: Path) -> None:
    """Bump the patch version in pyproject.toml.

    Args:
        pyproject_path: Path to pyproject.toml file
    """
    console.print("[cyan]Auto-bumping[/cyan] patch version...")

    content = pyproject_path.read_text()
    lines = content.splitlines()

    for i, line in enumerate(lines):
        if line.strip().startswith("version = "):
            # Parse current version
            current = line.split('"')[1]
            parts = current.split(".")
            if len(parts) == 3:
                major, minor, patch = parts
                new_patch = int(patch) + 1
                new_version = f"{major}.{minor}.{new_patch}"
                lines[i] = f'version = "{new_version}"'

                # Write back
                pyproject_path.write_text("\n".join(lines) + "\n")
                console.print(
                    f"[green]✓[/green] Version bumped: {current} → {new_version}"
                )
                break
            else:
                console.print(f"[yellow]![/yellow] Unable to parse version: {current}")


def reinstall_package(dev_dir: Path) -> None:
    """Reinstall the nssh package using uv.

    Args:
        dev_dir: Development directory containing pyproject.toml
    """
    if not shutil.which("uv"):
        console.print("[yellow]![/yellow] uv not found on PATH - skipping reinstall")
        console.print(
            "[dim]Install uv: curl -LsSf https://astral.sh/uv/install.sh | sh[/dim]"
        )
        return

    # Check if nssh is installed via uv tool
    result = subprocess.run(
        ["uv", "tool", "list"],
        capture_output=True,
        text=True,
    )

    if result.returncode == 0 and "nssh" in result.stdout:
        console.print("[cyan]Running:[/cyan] uv tool uninstall nssh")
        subprocess.run(["uv", "tool", "uninstall", "nssh"], capture_output=True)

        console.print("[cyan]Running:[/cyan] uv tool install . --force --reinstall")
        install_result = subprocess.run(
            ["uv", "tool", "install", ".", "--force", "--reinstall"],
            cwd=dev_dir,
            capture_output=True,
            text=True,
        )
        if install_result.returncode != 0:
            console.print(
                f"[red]Error:[/red] Reinstall failed:\n{install_result.stderr}"
            )
            raise typer.Exit(1)
        console.print("[green]✓[/green] Package reinstalled")
    else:
        console.print(
            "[yellow]![/yellow] nssh not installed via uv tool - skipping reinstall"
        )
        console.print("[dim]Install with: uv tool install .[/dim]")


def refresh_tracked_files(manifest) -> None:
    """Refresh all tracked files from assets.

    Args:
        manifest: Install manifest with tracked files
    """
    console.print(
        f"[cyan]Step 3/3:[/cyan] Refreshing {len(manifest.files)} tracked files..."
    )

    for file_entry in manifest.files:
        file_path = Path(file_entry.path)
        if file_entry.source:
            category, name = file_entry.source.split("/", 1)
            console.print(f"[yellow]Updating[/yellow] {file_path}")
            try:
                with resources.as_file(get_asset(category, name)) as src_path:
                    write_file(src_path, file_path, executable=True)
            except FileNotFoundError:
                console.print(
                    f"[red]Error:[/red] Source not found: {file_entry.source}"
                )


def reinstall_command(
    skip_uv: Annotated[
        bool,
        typer.Option("--skip-uv", help="Skip uv tool reinstall; only refresh files"),
    ] = False,
    dev: Annotated[
        bool,
        typer.Option(
            "--dev", help="Auto-bump patch version in pyproject.toml before reinstall"
        ),
    ] = False,
):
    """Reinstall nssh - clear cache and reinstall package (if dev mode)."""

    share_dir = share_assets_dir()

    # Validate we're in the nssh directory
    validate_in_nssh_directory()

    ui.show_panel(
        "nssh self reinstall",
        "Clean reinstall and refresh",
        style="cyan",
    )

    manifest = read_manifest(share_dir)
    if manifest is None:
        console.print("[red]Error:[/red] No manifest found")
        console.print("[dim]Run 'nssh self install' first[/dim]")
        raise typer.Exit(1)

    # Step 1: Clear Python cache
    clear_python_cache()

    # Step 2: Detect dev environment and reinstall if needed
    if not skip_uv:
        console.print("[cyan]Step 2/3:[/cyan] Checking for package reinstall...")

        dev_dir = detect_dev_directory()

        if dev_dir:
            console.print(f"[yellow]![/yellow] Development mode detected: {dev_dir}")

            # Auto-bump version if --dev flag is set
            if dev:
                pyproject_path = dev_dir / "pyproject.toml"
                bump_version(pyproject_path)

            # Always reinstall if uv tool nssh is installed
            reinstall_package(dev_dir)
        else:
            console.print("[dim]No dev environment detected - skipping reinstall[/dim]")
    else:
        console.print("[cyan]Step 2/3:[/cyan] Skipped (--skip-uv)")

    # Step 3: Re-install all tracked files
    refresh_tracked_files(manifest)

    # Update manifest timestamp
    manifest.installed_at = datetime.now().isoformat()
    write_manifest(manifest, share_dir)

    ui.show_panel(
        "Reinstall Complete",
        "Package reinstalled and files refreshed to current versions",
        style="green",
    )
