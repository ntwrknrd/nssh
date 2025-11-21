#!/usr/bin/env python3
"""Configuration validation and setup for self."""

from __future__ import annotations

import subprocess
import sys
from pathlib import Path

from nssh.cli.self.manifest import InstallManifest
from nssh.cli.common.prompt import confirm
from nssh.core.env.paths import age_key_path as get_age_key_path
from nssh.core.env.paths import credential_file_path as get_credential_file_path
from nssh.core.env.paths import ssh_config_path, ssh_include_dir
from nssh.core.ui.console import get_console

console = get_console()


def validate_in_nssh_directory() -> Path:
    """Validate that we're running from within the nssh project directory.

    Returns:
        Path to the nssh project root

    Raises:
        SystemExit: If not in nssh directory
    """
    cwd = Path.cwd()
    pyproject_path = cwd / "pyproject.toml"

    # Check if pyproject.toml exists in current directory
    if not pyproject_path.exists():
        console.print(
            "[red]Error:[/red] This command must be run from the nssh project directory"
        )
        console.print(f"[dim]Current directory: {cwd}[/dim]")
        console.print("[dim]Expected: pyproject.toml not found[/dim]")
        sys.exit(1)

    # Verify it's the nssh project
    pyproject_content = pyproject_path.read_text()
    if 'name = "nssh"' not in pyproject_content:
        console.print(
            "[red]Error:[/red] This command must be run from the nssh project directory"
        )
        console.print(f"[dim]Current directory: {cwd}[/dim]")
        console.print("[dim]Found pyproject.toml but not for nssh project[/dim]")
        sys.exit(1)

    return cwd


def validate_age_key(manifest: InstallManifest, dry_run: bool) -> bool:
    """Validate age key exists or offer to create it.

    Args:
        manifest: Install manifest to track files
        dry_run: Preview mode (don't create files)

    Returns:
        True if age key exists or was created, False otherwise

    Raises:
        SystemExit: If user declines to create age key
    """
    age_key_path = get_age_key_path()

    if age_key_path.exists():
        console.print(f"[green]✓[/green] Age key: {age_key_path}")
        # Track existing file in manifest for reference
        if not dry_run:
            manifest.add_reference_file(age_key_path, "age_key")
        return True

    console.print("[yellow]![/yellow] Age encryption key not found")
    console.print(f"   Expected: {age_key_path}")

    if dry_run:
        console.print(f"[dim]Would run: age-keygen -o {age_key_path}[/dim]")
        return True

    if not confirm("Generate age key now?", default=True):
        console.print("[red]Age key required for credential encryption[/red]")
        sys.exit(1)

    age_key_path.parent.mkdir(parents=True, exist_ok=True)
    subprocess.run(["age-keygen", "-o", str(age_key_path)], check=True)
    age_key_path.chmod(0o600)
    console.print(f"[green]✓[/green] Age key created: {age_key_path}")
    manifest.add_file(age_key_path, "file", "generated/age_key")
    return True


def validate_ssh_config(manifest: InstallManifest, dry_run: bool) -> bool:
    """Validate SSH config exists or offer to create it.

    Prefers ~/.ssh/conf.d/ structure for new installations.

    Args:
        manifest: Install manifest to track files
        dry_run: Preview mode (don't create files)

    Returns:
        True if SSH config exists or was created
    """
    ssh_config = ssh_config_path()
    ssh_conf_d = ssh_include_dir()

    # Check for existing config
    if ssh_config.exists():
        console.print(f"[green]✓[/green] SSH config: {ssh_config}")
        if not dry_run:
            manifest.add_reference_file(ssh_config, "ssh_config")
        return True

    # Check for existing conf.d structure
    if ssh_conf_d.exists() and any(ssh_conf_d.iterdir()):
        console.print(
            f"[green]✓[/green] SSH config: {ssh_conf_d}/ (directory structure)"
        )
        if not dry_run:
            manifest.add_reference_file(ssh_conf_d, "ssh_conf_d")
        return True

    # No SSH config - offer to create conf.d structure
    console.print("[yellow]![/yellow] SSH config not found")
    console.print("   Will create ~/.ssh/conf.d/ directory structure")

    if dry_run:
        console.print(f"[dim]Would create: {ssh_config}[/dim]")
        console.print(f"[dim]Would create: {ssh_conf_d}/[/dim]")
        return True

    if not confirm("Create SSH config with conf.d/ structure?", default=True):
        console.print(
            "[yellow]Warning: SSH config required for host management[/yellow]"
        )
        return False

    # Create SSH config with Include directive
    ssh_config.parent.mkdir(parents=True, exist_ok=True)

    # Use configured include_dir path
    include_pattern = str(ssh_conf_d / "*")
    ssh_config_content = f"""# SSH configuration
# Host entries managed in {ssh_conf_d} directory

Include {include_pattern}

Host *
  ServerAliveInterval 60
"""
    ssh_config.write_text(ssh_config_content)
    ssh_config.chmod(0o644)
    console.print(f"[green]✓[/green] Created: {ssh_config}")
    manifest.add_file(ssh_config, "file", "generated/ssh_config")

    # Create include directory
    ssh_conf_d.mkdir(parents=True, exist_ok=True)
    console.print(f"[green]✓[/green] Created: {ssh_conf_d}/")
    manifest.add_file(ssh_conf_d, "directory", "generated/ssh_conf_d")
    return True


def check_credentials_file(manifest: InstallManifest, dry_run: bool) -> None:
    """Check for credentials file and provide guidance.

    Args:
        manifest: Install manifest to track files
        dry_run: Preview mode
    """
    cred_path = get_credential_file_path()
    ssh_conf_d = ssh_include_dir()

    if cred_path.exists():
        console.print(f"[green]✓[/green] Credentials: {cred_path}")
        if not dry_run:
            manifest.add_reference_file(cred_path, "credentials")
    else:
        # Check if using conf.d structure and suggest appropriate path
        if ssh_conf_d.exists():
            cred_conf_d_path = ssh_conf_d / "nssh_credentials.age"
            console.print("[dim]Note: Create credentials when adding contexts:[/dim]")
            console.print(f"[dim]      Suggested: {cred_conf_d_path}[/dim]")
            console.print(f"[dim]      Default:   {cred_path}[/dim]")
        else:
            console.print("[dim]Note: Create credentials when adding contexts:[/dim]")
            console.print(f"[dim]      {cred_path}[/dim]")


def validate_and_setup_configuration(
    manifest: InstallManifest,
    dry_run: bool = False,
) -> None:
    """Validate configuration files and offer to create missing ones interactively.

    Prefers ~/.ssh/conf.d/ structure for new files, respects existing files.
    Tracks all validated files in manifest for future reference.

    Args:
        manifest: Install manifest to track files
        dry_run: Preview mode (don't create files)
    """
    # Validate age key
    validate_age_key(manifest, dry_run)

    # Validate SSH config
    validate_ssh_config(manifest, dry_run)

    # Check credentials file (informational only)
    check_credentials_file(manifest, dry_run)
