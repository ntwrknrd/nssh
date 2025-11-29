"""Remove command for nssh host."""

from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
from typing import List, Optional

from nssh.cli import click
from nssh.cli.common.batch import (
    BatchResult,
    HostEntry,
    is_batch_file,
    parse_batch_file,
)
from nssh.cli.common.prompt import confirm, prompt_required
from nssh.cli.common.banner import FAIL, NOOP, OK, WARN, banner
from nssh.cli.common.ui import render_removal_preview

from nssh.cli.common.credentials import (
    complete_hostname,
    console,
    get_manager,
    get_parser,
)


def _hostname_to_entry(hostname: str) -> HostEntry:
    """Convert a hostname string to a HostEntry (for .txt parsing)."""
    return HostEntry(hostname=hostname)


def _parse_remove_file(path: str) -> List[HostEntry]:
    """Parse a file containing hostnames to remove.

    Supports .txt (one hostname per line), .csv (hostname column),
    and .json (array of strings or objects with hostname key).

    Returns HostEntry objects so we can use the unified `alias` property
    for host identification (supports optional `host` field in CSV/JSON).

    Args:
        path: Path to batch file

    Returns:
        List of HostEntry objects to remove
    """
    return parse_batch_file(path, HostEntry, txt_to_entry=_hostname_to_entry)


@dataclass
class RemoveMatch:
    """Details about a host matched for removal."""

    input_hostname: str  # What was in the file (e.g., 'k3s-d.home.arpa')
    host_alias: str  # SSH config Host alias (e.g., 'k3s-d')
    config_file: Path  # Which file contains it


@dataclass
class BatchRemoveResult:
    """Extended result for batch remove with match details."""

    result: BatchResult
    matches: List[RemoveMatch]  # Hosts that will be/were removed
    skipped_hostnames: List[str]  # Hostnames not found


def _batch_remove(
    entries: List[HostEntry],
    parser,
    cm,
    dry_run: bool = False,
) -> BatchRemoveResult:
    """Remove multiple hosts from config files.

    Args:
        entries: List of HostEntry objects to remove
        parser: SSHConfigParser instance
        cm: CredentialManager instance
        dry_run: If True, preview without making changes

    Returns:
        BatchRemoveResult with counts, matches, and skipped hostnames
    """
    result = BatchResult()
    matches: List[RemoveMatch] = []
    skipped_hostnames: List[str] = []

    for entry in entries:
        try:
            # Derive the alias using the same logic as host add
            # (uses entry.host if set, otherwise hostname.split('.')[0])
            derived_alias = entry.alias
            host_alias = derived_alias
            target_file = None

            # Try to find by derived alias first
            found = parser.find_host_in_files(derived_alias)

            # Fallback: search by Hostname directive (e.g., full FQDN)
            if not found:
                found_by_hostname = parser.find_host_by_hostname(entry.hostname)
                if found_by_hostname:
                    target_file, host_alias, host_lines = found_by_hostname
                    found = (target_file, host_lines)

            if not found:
                # Not found - track for display
                result.skipped += 1
                skipped_hostnames.append(entry.hostname)
                continue

            target_file, host_lines = found
            matches.append(
                RemoveMatch(
                    input_hostname=entry.hostname,
                    host_alias=host_alias,
                    config_file=target_file,
                )
            )

            if dry_run:
                result.added += 1
                continue

            # Create backup
            parser.create_backup(target_file)

            # Parse and remove host
            header_lines, hosts = parser.parse_ssh_config(target_file)
            hosts = [(h, lines) for h, lines in hosts if h != host_alias]

            # Write back
            parser.write_ssh_config(target_file, header_lines, hosts)

            # Auto-clean host credentials (try both the input hostname and alias)
            for cred_name in {entry.hostname, host_alias}:
                if cm.get_host_credentials(cred_name):
                    cm.delete_host_all_credentials(cred_name)

            result.added += 1

        except Exception as e:
            result.failed += 1
            result.errors.append(f"{entry.hostname}: {str(e)}")

    # Rebuild index after batch operation
    if not dry_run:
        parser.rebuild_index()

    return BatchRemoveResult(
        result=result, matches=matches, skipped_hostnames=skipped_hostnames
    )


def _get_orphaned_context(hostname: str, parser, cm) -> Optional[str]:
    """Check if removing this host would orphan its context.

    Returns:
        Context name if it would be orphaned, None otherwise
    """
    # Find which file contains this host
    found = parser.find_host_in_files(hostname)
    if not found:
        return None

    target_file, _ = found

    # Get the context for this file
    contexts = cm.list_contexts()
    file_context = None
    for ctx in contexts:
        if ctx.get("git_include_file") == target_file.name:
            file_context = ctx
            break

    if not file_context:
        return None

    # Count other hosts in this file
    _, hosts = parser.parse_ssh_config(target_file)
    other_hosts = [h for h, _ in hosts if h != hostname]

    if not other_hosts:
        return file_context.get("name")

    return None


def _interactive_remove(
    ctx: click.Context,
    hostname: Optional[str],
    yes: bool,
    dry_run: bool,
    set_outcome,
) -> None:
    """Interactive flow for removing host(s) - handles multiple contexts."""
    parser = get_parser(ctx)
    cm = get_manager(ctx)

    # Get hostname
    input_hostname = prompt_required("Enter FQDN to remove", hostname)

    # Find ALL hosts matching this FQDN across all contexts
    all_matches = parser.find_all_hosts_by_hostname(input_hostname)

    # Fall back to alias search if no FQDN matches
    if not all_matches:
        alias_matches = parser.find_all_hosts_by_alias(input_hostname)
        if alias_matches:
            # Convert to same format: (file_path, host_alias, host_lines)
            all_matches = [
                (file_path, host_alias, host_lines)
                for file_path, host_alias, host_lines, _ in alias_matches
            ]

    if not all_matches:
        console.print(f"[yellow]![/yellow] Host '{input_hostname}' not found")
        set_outcome(NOOP)
        return

    # Process each match
    removed_count = 0
    skipped_count = 0

    for target_file, host_alias, host_lines in all_matches:
        console.print(f"[dim]Found in {target_file}...[/dim]")

        # Show host configuration preview
        render_removal_preview(host_lines)

        # Check if credentials exist
        has_credentials = cm.get_host_credentials(host_alias) is not None

        if has_credentials:
            console.print(
                f"[yellow]![/yellow] Host '{host_alias}' has stored credentials"
            )

        # Check for orphaned context
        orphaned_context = _get_orphaned_context(host_alias, parser, cm)
        if orphaned_context:
            console.print(
                f"[yellow]![/yellow] Context '{orphaned_context}' will have no hosts after removal"
            )

        if dry_run:
            console.print("[dim]Dry-run: would remove host from config[/dim]")
            if has_credentials:
                console.print("[dim]Dry-run: would delete stored credentials[/dim]")
            removed_count += 1
            continue

        # Confirm deletion (skip if --yes flag is used)
        console.print()
        if not yes:
            do_remove = confirm(
                f"Remove host '{host_alias}' from {target_file.name}?",
                default=False,
            )
            if not do_remove:
                console.print(f"[yellow]![/yellow] Skipped '{host_alias}'")
                skipped_count += 1
                continue

        # Ask about credential deletion if exists (auto-delete if --yes flag is used)
        delete_credentials = False
        if has_credentials:
            if yes:
                delete_credentials = True
            else:
                delete_credentials = confirm(
                    f"Also delete stored credentials for '{host_alias}'?",
                    default=True,
                )

        # Ask about orphaned context (only in interactive mode)
        delete_orphan_context = False
        if orphaned_context and not yes:
            delete_orphan_context = confirm(
                f"Delete orphaned context '{orphaned_context}'?",
                default=False,
            )

        # Remove host from config file
        try:
            # Create backup
            parser.create_backup(target_file)

            # Parse and remove host
            header_lines, hosts = parser.parse_ssh_config(target_file)
            hosts = [(h, lines) for h, lines in hosts if h != host_alias]

            # Write back
            parser.write_ssh_config(target_file, header_lines, hosts)

            console.print(
                f"[green]\u2713[/green] Host '{host_alias}' removed from {target_file.name}"
            )
            removed_count += 1

            # Delete credentials if requested
            if delete_credentials:
                cm.delete_host_all_credentials(host_alias)
                console.print(
                    f"[green]\u2713[/green] Credentials deleted for '{host_alias}'"
                )

            # Delete orphaned context if requested
            if delete_orphan_context and orphaned_context:
                cm.delete_context(orphaned_context)
                console.print(
                    f"[green]\u2713[/green] Orphaned context '{orphaned_context}' deleted"
                )

        except Exception as e:
            console.print(f"[red]Error removing '{host_alias}': {e}[/red]")
            skipped_count += 1

    # Rebuild host index after all removals
    if removed_count > 0 and not dry_run:
        parser.rebuild_index()

    # Set outcome
    if dry_run:
        set_outcome(NOOP)
    elif removed_count > 0 and skipped_count == 0:
        pass  # Default "Host removed" will be used
    elif removed_count > 0:
        set_outcome(WARN)
    else:
        set_outcome(NOOP)


@click.command(short_help="Remove host(s)")
@click.argument(
    "host_or_file",
    metavar="FQDN|FILE",
    required=False,
    default=None,
    shell_complete=complete_hostname,
)
@click.option("-y", "--yes", is_flag=True, default=False, help="Skip confirmations")
@click.option("--dry-run", is_flag=True, default=False, help="Preview only")
@click.pass_context
def remove_command(
    ctx: click.Context,
    host_or_file: Optional[str],
    yes: bool,
    dry_run: bool,
) -> None:
    """Remove SSH host(s) from configuration.

    Single-host mode (interactive):
        nssh host rm server.domain.local       # by FQDN
        nssh host rm server.domain.local -y    # no confirmation

    Batch mode (from file):
        nssh host rm ./hosts.txt   # one FQDN per line
        nssh host rm ./hosts.csv   # CSV with hostname column
        nssh host rm ./hosts.json  # JSON array
    """
    # Detect mode
    if host_or_file and is_batch_file(host_or_file):
        # Batch mode
        with banner("BATCH REMOVE HOSTS", OK) as set_outcome:
            _batch_remove_mode(ctx, host_or_file, dry_run, set_outcome)
    else:
        # Interactive/single-host mode
        with banner("REMOVE SSH HOST", OK) as set_outcome:
            _interactive_remove(ctx, host_or_file, yes, dry_run, set_outcome)


def _batch_remove_mode(ctx, host_or_file, dry_run, set_outcome) -> None:
    """Batch remove mode - remove multiple hosts from file."""
    cm = get_manager(ctx)
    parser = get_parser(ctx)

    try:
        entries = _parse_remove_file(host_or_file)
    except FileNotFoundError as e:
        console.print(f"[red]Error: {e}[/red]")
        set_outcome(FAIL)
        raise SystemExit(1)
    except ValueError as e:
        console.print(f"[red]Error parsing file: {e}[/red]")
        set_outcome(FAIL)
        raise SystemExit(1)

    console.print(
        f"\n[green]\u2713[/green] Loaded {len(entries)} entries from {host_or_file}"
    )

    if not entries:
        console.print("\n[red]No hostnames to process[/red]")
        set_outcome(FAIL)
        raise SystemExit(1)

    # Process removal
    batch_result = _batch_remove(entries, parser, cm, dry_run=dry_run)
    result = batch_result.result

    # Display hosts that will be removed, grouped by config file
    if batch_result.matches:
        dash_style = "[dim]-[/dim]" if dry_run else "[red]-[/red]"
        verb = "Would remove" if dry_run else "Removing"
        symbol = "[yellow]![/yellow]" if dry_run else "[green]-[/green]"

        # Group by config file
        by_config: dict[str, list] = {}
        for match in batch_result.matches:
            config_name = match.config_file.stem  # e.g., "work_hosts" -> "work_hosts"
            if config_name not in by_config:
                by_config[config_name] = []
            by_config[config_name].append(match)

        for config_name, matches in by_config.items():
            noun = "host entry" if len(matches) == 1 else "host entries"
            console.print(
                f"{symbol} {verb} {len(matches)} {noun} from '{config_name}' context:"
            )
            for match in matches[:10]:
                if match.input_hostname != match.host_alias:
                    console.print(
                        f"    {dash_style} [dim]{match.host_alias} [italic]({match.input_hostname})[/italic][/dim]"
                    )
                else:
                    console.print(f"    {dash_style} [dim]{match.host_alias}[/dim]")
            if len(matches) > 10:
                console.print(f"    [dim]... and {len(matches) - 10} more[/dim]")

    # Display hosts not found
    if batch_result.skipped_hostnames:
        console.print(
            f"[yellow]![/yellow] {len(batch_result.skipped_hostnames)} not found:"
        )
        for hostname in batch_result.skipped_hostnames[:10]:
            console.print(f"  [dim]- {hostname}[/dim]")
        if len(batch_result.skipped_hostnames) > 10:
            console.print(
                f"  [dim]... and {len(batch_result.skipped_hostnames) - 10} more[/dim]"
            )

    # Display errors
    if result.errors:
        console.print(f"[red]Error:[/red] {len(result.errors)} failed:")
        for error in result.errors[:10]:
            console.print(f"  [red]{error}[/red]")
        if len(result.errors) > 10:
            console.print(f"  [dim]... and {len(result.errors) - 10} more[/dim]")

    # Completion message
    if dry_run:
        set_outcome(NOOP)
    elif result.has_failures():
        set_outcome(FAIL)
        raise SystemExit(1)
    # else: default "Hosts removed" footer will be used
