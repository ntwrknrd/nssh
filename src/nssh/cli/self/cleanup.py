#!/usr/bin/env python3
"""Cleanup command for self - remove installed nssh files."""

from __future__ import annotations

import shutil
from pathlib import Path
from typing import Annotated

from nssh.cli import typer
from nssh.cli.self.manifest import delete_manifest, read_manifest
from nssh.cli.common import ui
from nssh.core.env.paths import (
    credential_file_path,
    host_index_path,
    share_assets_dir,
)
from nssh.core.env.settings import default_config_path
from nssh.core.recording.manager import load_recording_settings
from nssh.core.ui.console import get_console

console = get_console()


def cleanup_command(
    keep_config: Annotated[
        bool,
        typer.Option("--keep-config", help="Keep credentials and config files"),
    ] = False,
    keep_recordings: Annotated[
        bool,
        typer.Option("--keep-recordings", help="Keep recording data"),
    ] = False,
    dry_run: Annotated[
        bool,
        typer.Option("--dry-run/--no-dry-run", help="Preview actions without removing"),
    ] = False,
):
    """Remove nssh files tracked by self (cleanup installation)."""

    share_dir = share_assets_dir()

    ui.show_panel(
        "nssh self cleanup",
        "Remove files installed by self",
        style="yellow",
    )

    manifest = read_manifest(share_dir)
    if manifest is None:
        console.print("[red]Error:[/red] No manifest found - nothing to uninstall")
        console.print(f"[dim]Expected manifest at: {share_dir / 'manifest.json'}[/dim]")
        raise typer.Exit(1)

    console.print(f"[cyan]Found manifest with {len(manifest.files)} files[/cyan]")

    # Remove profile modifications
    for mod in manifest.profile_modifications:
        profile_path = Path(mod.path)
        if not profile_path.exists():
            console.print(f"[yellow]![/yellow] Profile file not found: {profile_path}")
            continue

        console.print(f"[yellow]Removing[/yellow] profile snippet from {profile_path}")
        if not dry_run:
            lines = profile_path.read_text().splitlines()
            # Find marker and remove block
            new_lines = []
            skip = False
            for line in lines:
                if mod.marker in line:
                    skip = True
                elif skip and "# <<< nssh integration <<<" in line:
                    skip = False
                    continue
                elif not skip:
                    new_lines.append(line)
            profile_path.write_text("\n".join(new_lines) + "\n")

    # Remove files in reverse order (skip reference files)
    for file_entry in reversed(manifest.files):
        file_path = Path(file_entry.path)

        # Skip reference files - we didn't create them
        if file_entry.type == "reference":
            console.print(
                f"[dim]Skipping tracked file: {file_path} ({file_entry.reference_type})[/dim]"
            )
            continue

        if not file_path.exists() and not file_path.is_symlink():
            console.print(f"[dim]Already removed: {file_path}[/dim]")
            continue

        console.print(f"[yellow]Removing[/yellow] {file_path}")
        if not dry_run:
            if file_entry.type == "directory":
                # Only remove directory if empty
                try:
                    file_path.rmdir()
                except OSError:
                    console.print(
                        f"[dim]Directory not empty, keeping: {file_path}[/dim]"
                    )
            else:
                file_path.unlink()

    # Optionally remove config/credentials
    if not keep_config:
        config_files = [
            host_index_path(),
            credential_file_path(),
            default_config_path(),
        ]
        for config_file in config_files:
            if config_file.exists():
                console.print(f"[yellow]Removing[/yellow] config: {config_file}")
                if not dry_run:
                    config_file.unlink()

    # Optionally remove recordings
    if not keep_recordings:
        recordings_dir = load_recording_settings().directory
        if recordings_dir.exists():
            console.print(f"[yellow]Removing[/yellow] recordings: {recordings_dir}")
            if not dry_run:
                shutil.rmtree(recordings_dir)

    # Remove manifest
    if not dry_run:
        delete_manifest(share_dir)
        console.print("[green]✓[/green] Manifest removed")

    ui.show_panel(
        "Cleanup Complete",
        "nssh has been removed from your system",
        style="green",
    )
