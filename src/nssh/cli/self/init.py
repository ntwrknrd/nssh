#!/usr/bin/env python3
"""Init command for self - initialize nssh with configuration and shell helpers."""

from __future__ import annotations

import os
import platform
import sys
from pathlib import Path
from typing import Annotated, Optional

from nssh.cli import typer
from nssh.cli.self.assets import append_profile_snippet, install_resource
from nssh.cli.self.manifest import InstallManifest, write_manifest
from nssh.cli.self.system import check_nssh_on_path, check_system_dependencies
from nssh.cli.self.validation import (
    validate_and_setup_configuration,
    create_first_include_file,
    guided_context_setup,
    offer_config_template,
)
from nssh.cli.common import ui
from nssh.cli.common.prompt import confirm
from nssh.core.env.paths import (
    fish_completions_dir as resolve_fish_completions_dir,
    fish_functions_dir as resolve_fish_functions_dir,
    share_assets_dir as resolve_share_dir,
)
from nssh.core.ui.console import get_console

console = get_console()


def detect_user_shell() -> tuple[str, Path]:
    """Detect user's shell and suggest rc file.

    Returns:
        Tuple of (shell_name, rc_file_path)
    """
    shell = os.environ.get("SHELL", "")

    if "fish" in shell:
        return "fish", Path.home() / ".config/fish/config.fish"
    elif "zsh" in shell:
        return "zsh", Path.home() / ".zshrc"
    elif "bash" in shell:
        # Prefer .bashrc on Linux, .bash_profile on macOS
        if platform.system() == "Darwin":
            return "bash", Path.home() / ".bash_profile"
        return "bash", Path.home() / ".bashrc"
    else:
        return "unknown", Path.home() / ".bashrc"


def init_command(
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
    """Initialize nssh with configuration and optional shell integration."""
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
        "nssh self init",
        "Initialize nssh configuration and shell helpers",
        style="cyan",
    )

    console.print("[green]✓[/green] nssh found on PATH")

    manifest = InstallManifest()

    # Validate and setup configuration files interactively
    validate_and_setup_configuration(manifest, dry_run=dry_run)

    console.print()  # Blank line for readability

    # Auto-detect shell and offer integration if not explicitly specified
    if not install_shell_helpers and append_shell_snippet is None and not force:
        shell_name, rc_file = detect_user_shell()
        console.print(f"[dim]Detected shell: {shell_name}[/dim]")

        if shell_name != "unknown":
            if confirm(f"Install shell integration for {shell_name}?", default=True):
                install_shell_helpers = True
                append_shell_snippet = rc_file
                console.print(
                    f"[dim]Will install helpers and append to {rc_file}[/dim]"
                )

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

    # Offer to create first include file
    created_include_file = create_first_include_file(manifest, dry_run, force)

    # Offer to set up context credential for the include file
    if created_include_file:
        guided_context_setup(created_include_file, dry_run, force)

    # Offer to create config.toml from template
    offer_config_template(manifest, dry_run, force)

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
