"""Sorting helpers that power the `nssh host sort` subcommand."""

from __future__ import annotations

from pathlib import Path
from typing import Dict, List, Optional, Tuple

from nssh.cli import typer
from nssh.cli.common import ui
from nssh.cli.common.prompt import confirm
from nssh.cli.common.selectors import select_include_file
from nssh.core.ssh.config import SSHConfigParser
from nssh.core.ui.console import create_standard_table, get_console

console = get_console()


def _find_misplaced_hosts(
    hosts: List[Tuple[str, List[str]]],
) -> List[Tuple[int, str, int]]:
    """Return hosts that are out of alphabetical order."""

    misplaced = []
    sorted_hosts = sorted(hosts, key=lambda x: x[0].lower())

    for current_idx, (hostname, _) in enumerate(hosts):
        correct_idx = next(i for i, (h, _) in enumerate(sorted_hosts) if h == hostname)
        if current_idx != correct_idx:
            misplaced.append((current_idx, hostname, correct_idx))

    return misplaced


def _find_duplicate_hosts(hosts: List[Tuple[str, List[str]]]) -> Dict[str, List[int]]:
    """Map hostnames to duplicate positions."""

    seen: Dict[str, int] = {}
    duplicates: Dict[str, List[int]] = {}

    for idx, (hostname, _) in enumerate(hosts):
        if hostname in seen:
            duplicates.setdefault(hostname, [seen[hostname]]).append(idx)
        else:
            seen[hostname] = idx

    return duplicates


def _remove_duplicates(
    hosts: List[Tuple[str, List[str]]],
    duplicates: Dict[str, List[int]],
    keep: str = "first",
) -> List[Tuple[str, List[str]]]:
    """Remove duplicates, keeping first or last occurrence."""

    indices_to_remove = set()

    for indices in duplicates.values():
        if keep == "first":
            indices_to_remove.update(indices[1:])
        else:
            indices_to_remove.update(indices[:-1])

    return [host for idx, host in enumerate(hosts) if idx not in indices_to_remove]


def _sort_config_file(
    parser: SSHConfigParser,
    file_path: Path,
) -> Tuple[int, int]:
    """Sort a single SSH config file alphabetically and remove duplicates."""

    console.print(f"\n[bold]Analyzing {file_path.name}...[/bold]")

    header_lines, hosts = parser.parse_ssh_config(file_path)
    total_hosts = len(hosts)
    if total_hosts == 0:
        console.print("[dim]No hosts found in file[/dim]")
        return 0, 0

    console.print(f"[dim]Found {total_hosts} host entries[/dim]")

    duplicates = _find_duplicate_hosts(hosts)
    if duplicates:
        console.print(f"\n[yellow]Found {len(duplicates)} duplicate hosts:[/yellow]")
        duplicate_rows = [
            (hostname, str(len(indices)), ", ".join(str(i + 1) for i in indices))
            for hostname, indices in list(duplicates.items())[:10]
        ]
        dup_table = create_standard_table(
            [("Host", "yellow"), ("Occurrences", ""), ("Positions", "dim")],
            duplicate_rows,
        )
        console.print(dup_table)
        if len(duplicates) > 10:
            console.print(f"[dim]... and {len(duplicates) - 10} more[/dim]")
    else:
        console.print("[green]✓ No duplicates found[/green]")

    misplaced = _find_misplaced_hosts(hosts)
    if not misplaced and not duplicates:
        console.print(
            "[green]✓ File is already perfectly sorted with no duplicates[/green]"
        )
        return 0, 0

    if misplaced:
        console.print(f"\n[yellow]Found {len(misplaced)} misplaced entries:[/yellow]")
        misplaced_rows = [
            (
                hostname,
                str(current_idx + 1),
                "→",
                str(correct_idx + 1),
                f"{abs(correct_idx - current_idx)} positions",
            )
            for current_idx, hostname, correct_idx in misplaced[:10]
        ]
        table = create_standard_table(
            [
                ("Host", "yellow"),
                ("Current Position", ""),
                ("→", ""),
                ("Correct Position", ""),
                ("Distance", ""),
            ],
            misplaced_rows,
        )
        console.print(table)
        if len(misplaced) > 10:
            console.print(f"[dim]... and {len(misplaced) - 10} more[/dim]")
    else:
        console.print("[green]✓ File is properly sorted[/green]")

    console.print("\n[bold]Ready to apply changes[/bold]")
    console.print("[dim]This will:[/dim]")
    console.print("[dim]  • Create a backup[/dim]")
    if duplicates:
        console.print("[dim]  • Remove duplicate entries (keep first occurrence)[/dim]")
    if misplaced:
        console.print("[dim]  • Sort all host entries alphabetically[/dim]")
    console.print("[dim]  • Preserve header comments and wildcards[/dim]")
    console.print("[dim]  • Keep host configurations intact[/dim]")

    if not confirm("\n[cyan]Proceed with sorting?[/cyan]", default=True):
        console.print("[yellow]Skipped[/yellow]")
        return 0, 0

    backup_path = parser.create_backup(file_path)
    console.print(f"[dim]Backup created: {backup_path}[/dim]")

    if duplicates:
        hosts = _remove_duplicates(hosts, duplicates, keep="first")
        console.print(f"[dim]Removed {len(duplicates)} duplicate entries[/dim]")

    sorted_hosts = sorted(hosts, key=lambda x: x[0].lower())
    parser.write_ssh_config(file_path, header_lines, sorted_hosts)

    console.print("\n[bold green]✓ Success![/bold green]")
    final_host_count = len(sorted_hosts)
    console.print(f"Final: {final_host_count} hosts (was {total_hosts})")
    if duplicates:
        console.print(f"Removed: {len(duplicates)} duplicates")
    if misplaced:
        console.print(f"Fixed: {len(misplaced)} misplacements")

    return len(misplaced), len(duplicates)


def cmd_sort(
    parser: SSHConfigParser, file: Optional[str] = None, all: bool = False
) -> None:
    """Sort SSH config file(s)."""

    ui.show_panel(
        "Sort SSH Config Files",
        "Sort hosts alphabetically and remove duplicates",
        style="cyan",
    )

    if all:
        files_to_sort = parser.find_include_files()
        if not files_to_sort:
            console.print("[red]Error: No Include files found in ~/.ssh/config[/red]")
            raise typer.Exit(1)
    else:
        selection = select_include_file(
            parser, file, "Select config file to sort:", allow_all=True
        )
        files_to_sort = selection if isinstance(selection, list) else [selection]

    total_misplaced = 0
    total_duplicates = 0

    for file_path in files_to_sort:
        try:
            misplaced, duplicates = _sort_config_file(parser, file_path)
            total_misplaced += misplaced
            total_duplicates += duplicates
        except Exception as exc:  # pragma: no cover - defensive logging
            console.print(f"\n[red]Error sorting {file_path.name}: {exc}[/red]")
            import traceback

            traceback.print_exc()
            continue

    parser.rebuild_index()

    if len(files_to_sort) > 1:
        console.print("\n[bold]Summary:[/bold]")
        console.print(f"Sorted {len(files_to_sort)} files")
        console.print(f"Total misplacements fixed: {total_misplaced}")
        console.print(f"Total duplicates removed: {total_duplicates}")
