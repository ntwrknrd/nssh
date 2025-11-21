#!/usr/bin/env python3
"""Install command for self - install nssh and optional shell helpers."""

from __future__ import annotations

import sys
from pathlib import Path
from typing import Annotated, Optional

from nssh.cli import typer
from nssh.cli.self.assets import append_profile_snippet, install_resource
from nssh.cli.self.manifest import InstallManifest, write_manifest
from nssh.cli.self.system import check_nssh_on_path, check_system_dependencies
from nssh.cli.self.validation import validate_and_setup_configuration
from nssh.cli.common import ui
from nssh.core.env.paths import (
    fish_completions_dir as resolve_fish_completions_dir,
    fish_functions_dir as resolve_fish_functions_dir,
    share_assets_dir as resolve_share_dir,
)
from nssh.core.ui.console import get_console

console = get_console()


def install_command(
    install_shell_helpers: Annotated[
        bool,
        typer.Option(
            "--install-shell-helpers/--no-install-shell-helpers",
            help="Install optional bash/zsh/fish wrapper functions",
        ),
    ] = False,
    install_fish_completions: Annotated[
        bool,
        typer.Option(
            "--install-fish-completions/--no-install-fish-completions",
            help="Install fish completion files",
        ),
    ] = False,
    append_shell_snippet: Annotated[
        Optional[Path],
        typer.Option(
            "--append-shell-snippet",
            help="Append sourcing snippet to shell rc/profile",
        ),
    ] = None,
    symlink: Annotated[
        bool,
        typer.Option(
            "--symlink/--copy",
            help="Symlink assets instead of copying (default: copy)",
        ),
    ] = False,
    dry_run: Annotated[
        bool,
        typer.Option("--dry-run/--no-dry-run", help="Preview actions without writing"),
    ] = False,
    force: Annotated[
        bool,
        typer.Option("-f", "--force", help="Overwrite files without prompting"),
    ] = False,
):
    """Install shell helpers and optional shell integration."""
    share_dir = resolve_share_dir()
    fish_functions_dir = resolve_fish_functions_dir()
    fish_completions_dir = resolve_fish_completions_dir()

    # Check for required system dependencies first
    check_system_dependencies()

    # Verify nssh is installed via uv tool install
    if not check_nssh_on_path():
        console.print("[red]✗[/red] nssh not found on PATH", style="bold")
        console.print()
        console.print("[yellow]Self requires nssh to be installed first.[/yellow]")
        console.print()
        console.print("Install nssh with:")
        console.print("  [cyan]uv tool install .[/cyan]")
        console.print()
        console.print("Then run self again to install shell helpers.")
        sys.exit(1)

    ui.show_panel(
        "nssh self install",
        "Validate configuration and install shell helpers",
        style="cyan",
    )

    console.print("[green]✓[/green] nssh found on PATH")

    manifest = InstallManifest()

    # Validate and setup configuration files interactively
    validate_and_setup_configuration(manifest, dry_run=dry_run)

    console.print()  # Blank line for readability

    # Install shell helpers if requested
    if install_shell_helpers:
        ui.show_panel(
            "Shell Helpers",
            f"→ [green]{share_dir}[/green]",
            style="cyan",
        )
        for helper in [
            "nssh-shell-integration.sh",
            "nssh-shell-integration.fish",
        ]:
            install_resource(
                "scripts",
                helper,
                share_dir / helper,
                executable=True,
                symlink=symlink,
                dry_run=dry_run,
                force=force,
                manifest=manifest,
            )

        # Install Fish function if shell helpers enabled
        install_resource(
            "scripts",
            "nssh-shell-integration.fish",
            fish_functions_dir / "nssh.fish",
            executable=True,
            symlink=symlink,
            dry_run=dry_run,
            force=force,
            manifest=manifest,
        )

    # Install fish completions if requested
    if install_fish_completions:
        ui.show_panel(
            "Fish Completions",
            f"→ [green]{fish_completions_dir}[/green]",
            style="cyan",
        )
        install_resource(
            "completions",
            "nssh.fish",
            fish_completions_dir / "nssh.fish",
            executable=True,
            symlink=symlink,
            dry_run=dry_run,
            force=force,
            manifest=manifest,
        )

    # Append profile snippet if requested
    if append_shell_snippet is not None:
        ui.show_panel(
            "Shell Profile",
            f"→ [green]{append_shell_snippet.expanduser()}[/green]",
            style="cyan",
        )
        append_profile_snippet(
            append_shell_snippet.expanduser(), share_dir, dry_run, manifest
        )

    # Write manifest
    if not dry_run:
        write_manifest(manifest, share_dir)
        console.print(
            f"[green]✓[/green] Manifest written to {share_dir / 'manifest.json'}"
        )

    ui.show_panel(
        "Installation Complete",
        "Run 'nssh self status' to verify installation",
        style="green",
    )
