#!/usr/bin/env python3
"""Status command for self - show installation status."""

from __future__ import annotations

import shutil
from pathlib import Path

from nssh.cli.self.manifest import InstallManifest, read_manifest, write_manifest
from nssh.cli.self.system import check_nssh_on_path
from nssh.cli.common import ui
from nssh.core.env.paths import (
    age_key_path as get_age_key_path,
    credential_file_path as get_credential_file_path,
    share_assets_dir,
    ssh_config_path,
    ssh_include_dir,
)
from nssh.core.auth.credentials import CredentialManager
from nssh.core.ssh.config import SSHConfigParser
from nssh.core.ui.console import get_console

console = get_console()


def show_next_steps() -> None:
    """Show actionable next steps based on current configuration state."""
    age_key_path = get_age_key_path()
    ssh_config = ssh_config_path()
    ssh_conf_d = ssh_include_dir()

    has_age_key = age_key_path.exists()
    has_ssh_config = ssh_config.exists()
    has_include_files = ssh_conf_d.exists() and list(ssh_conf_d.glob("*"))

    # Check for contexts
    has_contexts = False
    try:
        cm = CredentialManager()
        contexts = cm.list_contexts()
        has_contexts = len(contexts) > 0
    except Exception:
        pass

    # Check for hosts
    has_hosts = False
    hosts: list[tuple[str, list[str]]] = []
    try:
        parser = SSHConfigParser()
        for file_path in parser.find_include_files():
            _, file_hosts = parser.parse_ssh_config(file_path)
            hosts.extend(file_hosts)
        has_hosts = len(hosts) > 0
    except Exception:
        pass

    console.print("[bold cyan]Setup Status:[/bold cyan]")
    console.print(
        f"  {'[green]✓[/green]' if has_age_key else '[red]✗[/red]'} Age encryption key"
    )
    console.print(
        f"  {'[green]✓[/green]' if has_ssh_config else '[red]✗[/red]'} SSH config"
    )
    console.print(
        f"  {'[green]✓[/green]' if has_include_files else '[yellow]![/yellow]'} Include files ({len(list(ssh_conf_d.glob('*'))) if ssh_conf_d.exists() else 0})"
    )
    console.print(
        f"  {'[green]✓[/green]' if has_contexts else '[yellow]![/yellow]'} Contexts ({len(contexts) if has_contexts else 0})"
    )
    console.print(
        f"  {'[green]✓[/green]' if has_hosts else '[yellow]![/yellow]'} Hosts ({len(hosts) if has_hosts else 0})"
    )

    console.print()
    console.print("[bold cyan]Next Steps:[/bold cyan]")

    if not has_age_key:
        console.print("  1. [cyan]Run: nssh self init[/cyan] (to create age key)")
    elif not has_ssh_config or not has_include_files:
        console.print("  1. [cyan]Run: nssh self init[/cyan] (to set up SSH config)")
    elif not has_contexts:
        console.print(
            "  1. [cyan]Create first context:[/cyan] nssh cred ctx add <name> --file <file>"
        )
        console.print(
            "     [dim]or run:[/dim] nssh self init [dim](guided setup)[/dim]"
        )
    elif not has_hosts:
        console.print("  1. [cyan]Add first host:[/cyan] nssh host add")
    else:
        console.print(
            "  [green]✓ Ready to connect![/green] Try: [cyan]nssh <hostname>[/cyan]"
        )
        console.print()
        console.print("[dim]Additional commands:[/dim]")
        console.print("  [dim]• nssh host list - Show all configured hosts[/dim]")
        console.print("  [dim]• nssh cred ctx list - Show all contexts[/dim]")
        console.print("  [dim]• nssh log list - Show recorded sessions[/dim]")


def status_command():
    """Show self installation status and discover existing files."""

    share_dir = share_assets_dir()

    ui.show_panel(
        "nssh self status",
        "Current installation status",
        style="cyan",
    )

    # Check CLI availability
    nssh_on_path = check_nssh_on_path()
    if nssh_on_path:
        nssh_path = shutil.which("nssh")
        console.print(f"[green]✓[/green] CLI: [cyan]{nssh_path}[/cyan]")
    else:
        console.print("[red]✗[/red] CLI: Not found on PATH")

    # Check manifest
    manifest = read_manifest(share_dir)

    if manifest is None:
        console.print("[yellow]![/yellow] No self manifest found")
        console.print("[dim]Scanning for existing files...[/dim]")
        manifest = InstallManifest()
    else:
        console.print(f"[green]✓[/green] Manifest: {share_dir / 'manifest.json'}")
        if manifest.installed_at:
            console.print(f"[dim]  Installed: {manifest.installed_at}[/dim]")

    # Discover existing files that should be tracked
    manifest_updated = False

    # Get tracked file paths for quick lookup
    tracked_paths = {f.path for f in manifest.files}

    # Check for age key
    age_key_path = get_age_key_path()
    if age_key_path.exists() and str(age_key_path.resolve()) not in tracked_paths:
        console.print(f"[yellow]![/yellow] Found untracked age key: {age_key_path}")
        manifest.add_reference_file(age_key_path, "age_key")
        manifest_updated = True

    # Check for SSH config
    ssh_config = ssh_config_path()
    if ssh_config.exists() and str(ssh_config.resolve()) not in tracked_paths:
        console.print(f"[yellow]![/yellow] Found untracked SSH config: {ssh_config}")
        manifest.add_reference_file(ssh_config, "ssh_config")
        manifest_updated = True

    # Check for SSH include directory
    ssh_conf_d = ssh_include_dir()
    if ssh_conf_d.exists() and str(ssh_conf_d.resolve()) not in tracked_paths:
        console.print(f"[yellow]![/yellow] Found untracked SSH conf.d: {ssh_conf_d}")
        manifest.add_reference_file(ssh_conf_d, "ssh_conf_d")
        manifest_updated = True

    # Check for credentials file
    cred_path = get_credential_file_path()
    if cred_path.exists() and str(cred_path.resolve()) not in tracked_paths:
        console.print(f"[yellow]![/yellow] Found untracked credentials: {cred_path}")
        manifest.add_reference_file(cred_path, "credentials")
        manifest_updated = True

    # Check for state directories
    state_dir = Path.home() / ".local" / "state" / "nssh"
    if state_dir.exists() and str(state_dir.resolve()) not in tracked_paths:
        console.print(
            f"[yellow]![/yellow] Found untracked state directory: {state_dir}"
        )
        manifest.add_reference_file(state_dir, "state_dir")
        manifest_updated = True

    # Check for config file
    config_path = Path.home() / ".config" / "nssh" / "config.toml"
    if config_path.exists() and str(config_path.resolve()) not in tracked_paths:
        console.print(f"[yellow]![/yellow] Found untracked config: {config_path}")
        manifest.add_reference_file(config_path, "config_file")
        manifest_updated = True

    # Write updated manifest if needed
    if manifest_updated:
        console.print()
        console.print("[cyan]Updating manifest with discovered files...[/cyan]")
        write_manifest(manifest, share_dir)
        console.print(
            f"[green]✓[/green] Manifest updated: {share_dir / 'manifest.json'}"
        )

    console.print()  # Blank line for readability

    # Separate files by type
    created_files = [f for f in manifest.files if f.type != "reference"]
    reference_files = [f for f in manifest.files if f.type == "reference"]

    # Show created files
    if created_files:
        console.print(f"[cyan]Created files ({len(created_files)}):[/cyan]")
        for file_entry in created_files:
            file_path = Path(file_entry.path)
            exists = file_path.exists() or file_path.is_symlink()
            status_icon = "[green]✓[/green]" if exists else "[red]✗[/red]"
            console.print(f"  {status_icon} {file_path}")

    # Show tracked files
    if reference_files:
        console.print(f"[cyan]Tracked files ({len(reference_files)}):[/cyan]")
        for file_entry in reference_files:
            file_path = Path(file_entry.path)
            exists = file_path.exists() or file_path.is_symlink()
            status_icon = "[green]✓[/green]" if exists else "[red]✗[/red]"
            ref_type = file_entry.reference_type or "unknown"
            console.print(f"  {status_icon} {file_path} [dim]({ref_type})[/dim]")

    # Show profile modifications
    if manifest.profile_modifications:
        console.print(
            f"\n[cyan]Profile integrations ({len(manifest.profile_modifications)}):[/cyan]"
        )
        for mod in manifest.profile_modifications:
            profile_path = Path(mod.path)
            exists = profile_path.exists()
            status_icon = "[green]✓[/green]" if exists else "[red]✗[/red]"
            console.print(f"  {status_icon} {profile_path}")

    # Show next steps based on configuration state
    console.print()
    show_next_steps()
