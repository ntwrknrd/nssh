#!/usr/bin/env python3
"""Configuration validation and setup for self."""

from __future__ import annotations

import shutil
import subprocess
import sys
from pathlib import Path
from importlib import resources

from nssh.cli.self.manifest import InstallManifest
from nssh.cli.common.prompt import (
    confirm,
    ask_text,
    is_interactive,
    prompt_password_with_confirmation,
)
from nssh.cli.common.ssh_include import ensure_conf_d_include
from nssh.core.env.paths import age_key_path as get_age_key_path
from nssh.core.env.paths import credential_file_path as get_credential_file_path
from nssh.core.env.paths import ssh_config_path, ssh_include_dir
from nssh.core.auth.credentials import CredentialManager
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


def validate_age_key(
    manifest: InstallManifest, dry_run: bool, yes: bool = False
) -> bool:
    """Validate age key exists or offer to create it.

    Args:
        manifest: Install manifest to track files
        dry_run: Preview mode (don't create files)
        yes: Auto-accept prompts (for non-interactive mode)

    Returns:
        True if age key exists or was created, False otherwise

    Raises:
        SystemExit: If user declines to create age key or non-interactive without --yes
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

    # In non-interactive mode without --yes, we cannot prompt
    if not is_interactive() and not yes:
        console.print("[red]Error: Non-interactive mode requires --yes flag[/red]")
        console.print("[dim]Run with --yes to auto-generate age key[/dim]")
        sys.exit(1)

    # Auto-accept if --yes flag provided
    if not yes and not confirm("Generate age key now?", default=True):
        console.print("[red]Age key required for credential encryption[/red]")
        sys.exit(1)

    age_key_path.parent.mkdir(parents=True, exist_ok=True)
    subprocess.run(["age-keygen", "-o", str(age_key_path)], check=True)
    age_key_path.chmod(0o600)
    console.print(f"[green]+[/green] Age key created: {age_key_path}")
    manifest.add_file(age_key_path, "file", "generated/age_key")
    return True


def validate_ssh_config(
    manifest: InstallManifest, dry_run: bool, yes: bool = False
) -> bool:
    """Validate SSH config exists or offer to create it.

    Prefers ~/.ssh/conf.d/ structure for new installations.

    Args:
        manifest: Install manifest to track files
        dry_run: Preview mode (don't create files)
        yes: Auto-accept prompts (for non-interactive mode)

    Returns:
        True if SSH config exists or was created
    """
    ssh_config = ssh_config_path()
    ssh_conf_d = ssh_include_dir()

    # Check for existing config
    if ssh_config.exists():
        console.print(f"[green]✓[/green] SSH config: {ssh_config}")

        ensure_conf_d_include(
            dry_run=dry_run,
            create_if_missing=False,
            preview_title="SSH config change preview",
            yes=yes,
        )

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

    # In non-interactive mode without --yes, we cannot prompt
    if not is_interactive() and not yes:
        console.print("[red]Error: Non-interactive mode requires --yes flag[/red]")
        console.print("[dim]Run with --yes to auto-create SSH config[/dim]")
        sys.exit(1)

    # Auto-accept if --yes flag provided
    if not yes and not confirm(
        "Create SSH config with conf.d/ structure?", default=True
    ):
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
    console.print(f"[green]+[/green] Created: {ssh_config}")
    manifest.add_file(ssh_config, "file", "generated/ssh_config")

    # Create include directory
    ssh_conf_d.mkdir(parents=True, exist_ok=True)
    console.print(f"[green]+[/green] Created: {ssh_conf_d}/")
    manifest.add_file(ssh_conf_d, "directory", "generated/ssh_conf_d")
    return True


def create_first_include_file(
    manifest: InstallManifest,
    dry_run: bool,
    yes: bool = False,
) -> Path | None:
    """Offer to create first include file in conf.d structure.

    Args:
        manifest: Install manifest to track files
        dry_run: Preview mode (don't create files)
        yes: Auto-accept prompts (for non-interactive mode)

    Returns:
        Path to created include file, or None if skipped
    """
    ssh_conf_d = ssh_include_dir()

    # Check if conf.d directory exists
    if not ssh_conf_d.exists():
        console.print(
            "[yellow]![/yellow] Cannot create include file: conf.d/ directory not found"
        )
        return None

    # Check if there are already include files
    existing_files = list(ssh_conf_d.glob("*"))
    if existing_files:
        console.print(
            f"[green]✓[/green] Include files exist: {len(existing_files)} file(s) in {ssh_conf_d}/"
        )
        return None

    # In non-interactive mode without --yes, skip this optional step
    if not is_interactive() and not yes:
        console.print(
            "[dim]Skipping include file creation (non-interactive mode)[/dim]"
        )
        return None

    # Offer to create first include file
    if not yes:
        console.print()
        console.print("[cyan]Create first include file?[/cyan]")
        console.print(
            "[dim]Include files organize hosts (e.g., work_hosts, homelab_hosts)[/dim]"
        )

        if not confirm("Create first include file?", default=True):
            console.print(
                "[dim]Skipped. Create later with: touch ~/.ssh/conf.d/<name>[/dim]"
            )
            return None

        include_file_name = ask_text(
            "Enter include file name (without path)",
            default="default",
        )
    else:
        include_file_name = "default"

    # Ensure no extension or path separators
    include_file_name = include_file_name.strip()
    if "/" in include_file_name or "\\" in include_file_name:
        console.print(
            "[yellow]Warning: Include file name should not contain path separators[/yellow]"
        )
        include_file_name = Path(include_file_name).name

    include_file = ssh_conf_d / include_file_name

    if include_file.exists():
        console.print(f"[yellow]![/yellow] Include file already exists: {include_file}")
        return include_file

    console.print(f"[dim]Creating include file: {include_file}[/dim]")

    if dry_run:
        console.print(f"[dim]Would create: {include_file}[/dim]")
        return include_file

    include_file.touch()
    include_file.chmod(0o644)
    manifest.add_file(include_file, "file", "generated/include_file")
    console.print(f"[green]+[/green] Created: {include_file}")

    return include_file


def guided_context_setup(
    include_file: Path | None,
    dry_run: bool,
    yes: bool = False,
) -> bool:
    """Guide user through creating first context credential.

    Args:
        include_file: Path to include file (if created), or None
        dry_run: Preview mode (don't create credentials)
        yes: Auto-accept prompts (for non-interactive mode)

    Returns:
        True if context was created, False otherwise
    """
    if include_file is None:
        console.print("[dim]Skipping context setup: No include file created[/dim]")
        return False

    # Check if we have an age key (required for encryption)
    age_key_path = get_age_key_path()
    if not age_key_path.exists():
        console.print(
            "[yellow]![/yellow] Cannot create context: Age key not found (required for encryption)"
        )
        return False

    # In non-interactive mode, skip context setup (requires password input)
    if not is_interactive() and not yes:
        console.print(
            "[dim]Skipping context setup (requires interactive password input)[/dim]"
        )
        return False

    # Offer to create context credential
    if not yes:
        console.print()
        console.print("[cyan]Set up context credential?[/cyan]")
        console.print(
            "[dim]Context credentials provide fallback auth for all hosts in the include file[/dim]"
        )

        if not confirm("Create context credential now?", default=True):
            console.print(
                f"[dim]Skipped. Create later with: nssh cred ctx add <name> --file {include_file.name}[/dim]"
            )
            return False

        # Prompt for context name
        default_context_name = include_file.stem or "default"
        context_name = ask_text(
            "Enter context name",
            default=default_context_name,
        )

        # Prompt for username
        import os

        default_username = os.getenv("USER", "admin")
        username = ask_text(
            "Enter default username for this context",
            default=default_username,
        )
    else:
        context_name = include_file.stem or "default"
        username = "admin"

    # Preview what will be created
    console.print()
    console.print("[bold]Context Preview:[/bold]")
    console.print(f"  Name: {context_name}")
    console.print(f"  File: {include_file.name}")
    console.print(f"  User: {username}")
    console.print()

    if dry_run:
        console.print(
            f"[dim]Would create context '{context_name}' for file '{include_file.name}' with username '{username}'[/dim]"
        )
        console.print("[dim]Would prompt for password (not shown in dry-run)[/dim]")
        return True

    # Prompt for password
    try:
        password = prompt_password_with_confirmation(
            f"[cyan]Enter password for {username} in context '{context_name}'[/cyan]"
        )
    except ValueError as exc:
        console.print(f"[red]{exc}[/red]")
        return False

    # Create context and credential
    try:
        cm = CredentialManager()

        # Create context
        cm.create_context(context_name, include_file.name)
        console.print(f"[green]+[/green] Context '{context_name}' created")

        # Add credential
        cm.add_context_credential(context_name, username, password, overwrite=True)
        console.print(
            f"[green]+[/green] Fallback credential set for context '{context_name}'"
        )

        console.print()
        console.print("[bold green]✓ Context setup complete![/bold green]")
        console.print(
            f"[dim]All hosts in {include_file.name} will use this credential by default[/dim]"
        )
        return True
    except Exception as exc:
        console.print(f"[red]Error creating context: {exc}[/red]")
        return False


def offer_config_template(
    manifest: InstallManifest,
    dry_run: bool,
    yes: bool = False,
) -> bool:
    """Offer to create config.toml from example template.

    Args:
        manifest: Install manifest to track files
        dry_run: Preview mode (don't create files)
        yes: Auto-accept prompts (for non-interactive mode)

    Returns:
        True if config file was created, False otherwise
    """
    config_path = Path.home() / ".config/nssh/config.toml"

    if config_path.exists():
        console.print(f"[green]✓[/green] Config file: {config_path}")
        if not dry_run:
            manifest.add_reference_file(config_path, "config")
        return False

    # In non-interactive mode without --yes, skip this optional step
    if not is_interactive() and not yes:
        console.print("[dim]Skipping config file creation (non-interactive mode)[/dim]")
        return False

    # Offer to create config file
    if not yes:
        console.print()
        console.print("[cyan]Create config file from template?[/cyan]")
        console.print(
            "[dim]Optional: Customize recording, encryption paths, etc.[/dim]"
        )

        if not confirm("Create config.toml?", default=False):
            console.print(
                "[dim]Skipped. Config file is optional (defaults will be used)[/dim]"
            )
            return False

    if dry_run:
        console.print(f"[dim]Would create: {config_path}[/dim]")
        console.print("[dim]Would copy from: docs/examples/config/config.toml[/dim]")
        return True

    # Copy example config
    try:
        # Try to get the asset from package resources
        import nssh.assets.examples  # type: ignore[import-not-found]

        with resources.as_file(
            resources.files(nssh.assets.examples) / "config.toml"  # type: ignore[attr-defined]
        ) as example_config:
            config_path.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(example_config, config_path)
            config_path.chmod(0o644)
            console.print(f"[green]+[/green] Created: {config_path}")
            console.print("[dim]Edit this file to customize nssh behavior[/dim]")
            manifest.add_file(config_path, "file", "generated/config")
            return True
    except (ImportError, FileNotFoundError):
        # Fallback: try to find example in docs (development mode)
        docs_example = Path("docs/examples/config/config.toml")
        if docs_example.exists():
            config_path.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(docs_example, config_path)
            config_path.chmod(0o644)
            console.print(f"[green]+[/green] Created: {config_path}")
            console.print("[dim]Edit this file to customize nssh behavior[/dim]")
            manifest.add_file(config_path, "file", "generated/config")
            return True
        else:
            console.print(
                "[yellow]![/yellow] Could not find example config.toml template"
            )
            return False


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
    yes: bool = False,
) -> None:
    """Validate configuration files and offer to create missing ones interactively.

    Prefers ~/.ssh/conf.d/ structure for new files, respects existing files.
    Tracks all validated files in manifest for future reference.

    Args:
        manifest: Install manifest to track files
        dry_run: Preview mode (don't create files)
        yes: Auto-accept prompts (for non-interactive mode)
    """
    # Validate age key
    validate_age_key(manifest, dry_run, yes=yes)

    # Validate SSH config
    validate_ssh_config(manifest, dry_run, yes=yes)

    # Check credentials file (informational only)
    check_credentials_file(manifest, dry_run)
