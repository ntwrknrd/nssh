#!/usr/bin/env python3
"""Init command for self - initialize nssh with configuration and shell helpers."""

from __future__ import annotations

import os
import platform
import sys
from pathlib import Path

from nssh.cli import click
from nssh.cli.self.assets import append_profile_snippet, install_resource
from nssh.cli.self.manifest import InstallManifest, write_manifest
from nssh.cli.self.system import check_nssh_on_path, check_system_dependencies
from nssh.cli.self.validation import (
    validate_and_setup_configuration,
    create_first_include_file,
    guided_context_setup,
    offer_config_template,
)
from nssh.cli.common.banner import FAIL, NOOP, OK, banner
from nssh.cli.common.prompt import confirm, is_interactive
from nssh.core.env.migrate import migrate_legacy_files
from nssh.core.env.paths import (
    fish_completions_dir as resolve_fish_completions_dir,
    fish_functions_dir as resolve_fish_functions_dir,
    share_assets_dir as resolve_share_dir,
)
from nssh.core.ui.console import get_console

console = get_console()


def detect_user_shell() -> tuple[str, Path]:
    """Detect user's shell and suggest rc file.

    Uses $SHELL environment variable, with fallback to parent process detection
    via psutil if $SHELL is not set or empty.

    Returns:
        Tuple of (shell_name, rc_file_path)
    """
    shell = os.environ.get("SHELL", "")

    # Fallback: detect shell from parent process if $SHELL is not set
    if not shell:
        try:
            import psutil

            parent = psutil.Process().parent()
            if parent:
                shell = parent.name()
        except Exception:
            pass

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


@click.command(short_help="Install nssh")
@click.option("--skip-shell", is_flag=True, default=False, help="Skip shell setup")
@click.option("--dry-run", is_flag=True, default=False, help="Preview only")
def init_command(skip_shell: bool, dry_run: bool) -> None:
    """Initialize nssh with configuration and shell integration.

    Automatically detects your shell and offers to install:
    - Shell helper functions (bash/zsh/fish)
    - Fish completions (if fish detected)
    - Profile sourcing snippet

    Use --skip-shell to opt out of shell integration entirely.
    """
    share_dir = resolve_share_dir()
    fish_functions_dir = resolve_fish_functions_dir()
    fish_completions_dir = resolve_fish_completions_dir()
    interactive = is_interactive()

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

    with banner("INSTALL NSSH", OK) as set_outcome:
        _init_impl(
            share_dir,
            fish_functions_dir,
            fish_completions_dir,
            interactive,
            skip_shell,
            dry_run,
            set_outcome,
        )


def _init_impl(
    share_dir,
    fish_functions_dir,
    fish_completions_dir,
    interactive,
    skip_shell,
    dry_run,
    set_outcome,
) -> None:
    """Internal implementation for initialization."""
    manifest = InstallManifest()

    # === Migrate legacy files ===
    if not dry_run:
        migrated = migrate_legacy_files(prompt_overwrite=interactive)
        if migrated:
            console.print(
                f"[green]+[/green] Migrated {len(migrated)} files to new locations:"
            )
            for old, new in migrated:
                console.print(f"  [dim]{old} -> {new}[/dim]")

    # === Configuration ===
    validate_and_setup_configuration(manifest, dry_run=dry_run, yes=not interactive)

    # === Shell Integration ===
    if skip_shell:
        console.print(
            "[dim]-[/dim] Shell integration: [dim]skipped (--skip-shell)[/dim]"
        )
    else:
        shell_name, rc_file = detect_user_shell()

        if shell_name == "unknown":
            if interactive:
                console.print(
                    "[yellow]![/yellow] Shell integration: [dim]skipped (unknown shell)[/dim]"
                )
            else:
                console.print("[red]Error:[/red] Could not detect shell")
                console.print("[dim]Use --skip-shell to proceed[/dim]")
                set_outcome(FAIL)
                sys.exit(1)
        else:
            should_install = True
            if interactive:
                console.print()
                should_install = confirm(
                    f"Install shell integration for {shell_name}?", default=True
                )

            if should_install:
                if shell_name == "fish":
                    install_resource(
                        "scripts",
                        "nssh-shell-integration.fish",
                        share_dir / "nssh-shell-integration.fish",
                        executable=True,
                        symlink=False,
                        dry_run=dry_run,
                        yes=True,
                        manifest=manifest,
                        quiet=True,
                    )
                    install_resource(
                        "scripts",
                        "nssh-shell-integration.fish",
                        fish_functions_dir / "nssh.fish",
                        executable=True,
                        symlink=False,
                        dry_run=dry_run,
                        yes=True,
                        manifest=manifest,
                        quiet=True,
                    )
                    install_resource(
                        "completions",
                        "nssh.fish",
                        fish_completions_dir / "nssh.fish",
                        executable=True,
                        symlink=False,
                        dry_run=dry_run,
                        yes=True,
                        manifest=manifest,
                        quiet=True,
                    )
                    console.print(
                        f"[green]+[/green] Shell scripts: {shell_name} + completions"
                    )
                else:
                    install_resource(
                        "scripts",
                        "nssh-shell-integration.sh",
                        share_dir / "nssh-shell-integration.sh",
                        executable=True,
                        symlink=False,
                        dry_run=dry_run,
                        yes=True,
                        manifest=manifest,
                        quiet=True,
                    )
                    console.print(f"[green]+[/green] Shell scripts: {shell_name}")

                append_profile_snippet(
                    rc_file.expanduser(), share_dir, dry_run, manifest, quiet=True
                )
                console.print(f"[green]+[/green] Profile snippet: {rc_file}")
            else:
                console.print("[dim]-[/dim] Shell integration: [dim]skipped[/dim]")

    # === Additional Setup ===
    created_include_file = create_first_include_file(
        manifest, dry_run, yes=not interactive
    )
    if created_include_file:
        guided_context_setup(created_include_file, dry_run, yes=not interactive)

    offer_config_template(manifest, dry_run, yes=not interactive)

    # === Finalize ===
    if not dry_run:
        write_manifest(manifest, share_dir)
    else:
        set_outcome(NOOP)
