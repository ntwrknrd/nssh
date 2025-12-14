"""Sorting helpers that power the `nssh host sort` subcommand."""

from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
from typing import Dict, List, Optional, Tuple

from nssh.cli.common import ui
from nssh.cli.common.banner import FAIL, NOOP, OK, banner
from nssh.cli.common.prompt import confirm
from nssh.core.ssh.config import SSHConfigParser
from nssh.core.ui.console import get_console

console = get_console()


@dataclass
class FileAnalysis:
    """Analysis results for a single config file."""

    path: Path
    hosts: List[Tuple[str, List[str]]]
    duplicates: Dict[str, List[int]]
    misplaced: List[Tuple[int, str, int]]

    @property
    def host_count(self) -> int:
        return len(self.hosts)

    @property
    def has_issues(self) -> bool:
        return bool(self.duplicates or self.misplaced)


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


def _analyze_file(parser: SSHConfigParser, file_path: Path) -> FileAnalysis:
    """Analyze a config file for duplicates and misplaced hosts."""
    header_lines, hosts = parser.parse_ssh_config(file_path)
    duplicates = _find_duplicate_hosts(hosts)
    misplaced = _find_misplaced_hosts(hosts)
    return FileAnalysis(
        path=file_path,
        hosts=hosts,
        duplicates=duplicates,
        misplaced=misplaced,
    )


def _apply_fixes(parser: SSHConfigParser, analysis: FileAnalysis) -> Tuple[int, int]:
    """Apply sorting and deduplication fixes to a file."""
    header_lines, hosts = parser.parse_ssh_config(analysis.path)

    backup_path = parser.create_backup(analysis.path)
    console.print(f"[dim]Backup: {backup_path}[/dim]")

    dup_count = len(analysis.duplicates)
    if analysis.duplicates:
        hosts = _remove_duplicates(hosts, analysis.duplicates, keep="first")

    sorted_hosts = sorted(hosts, key=lambda x: x[0].lower())
    parser.write_ssh_config(analysis.path, header_lines, sorted_hosts)

    return len(analysis.misplaced), dup_count


def _format_count(count: int, warn_style: bool = True) -> str:
    """Format a count, optionally with yellow styling if non-zero."""
    if count > 0 and warn_style:
        return f"[yellow]{count}[/yellow]"
    return str(count)


def _show_reorder_preview(
    hosts: List[Tuple[str, List[str]]],
    misplaced: List[Tuple[int, str, int]],
    max_display: int = 8,
) -> None:
    """Show a before/after preview of host reordering."""
    hostnames = [h[0] for h in hosts]
    sorted_names = sorted(hostnames, key=str.lower)

    # Find the range of indices that are affected
    misplaced_indices = {m[0] for m in misplaced}

    if not misplaced_indices:
        return

    min_idx = max(0, min(misplaced_indices) - 1)
    max_idx = min(len(hostnames), max(misplaced_indices) + 2)

    # Limit display range
    if max_idx - min_idx > max_display:
        max_idx = min_idx + max_display

    # Build table rows showing before -> after
    rows = []
    if min_idx > 0:
        rows.append(("...", "", "..."))

    for i in range(min_idx, max_idx):
        current = hostnames[i]
        sorted_name = sorted_names[i]

        # Format current (highlight if misplaced)
        if i in misplaced_indices:
            current_fmt = f"[yellow]{current}[/yellow]"
        else:
            current_fmt = f"[dim]{current}[/dim]"

        # Format sorted (highlight if different)
        if sorted_name != hostnames[i]:
            sorted_fmt = f"[green]{sorted_name}[/green]"
        else:
            sorted_fmt = f"[dim]{sorted_name}[/dim]"

        rows.append((current_fmt, "->", sorted_fmt))

    if max_idx < len(hostnames):
        rows.append(("...", "", "..."))

    console.print("[dim]Previewing changes...[/dim]")
    ui.print_table(
        (("Current", ""), ("", "dim"), ("Sorted", "")),
        rows,
    )
    console.print()


def cmd_sort(parser: SSHConfigParser, select_pattern: Optional[str] = None) -> None:
    """Sort SSH config file(s)."""
    with banner("SORT SSH HOSTS", OK) as set_outcome:
        _cmd_sort_impl(parser, select_pattern, set_outcome)


def _cmd_sort_impl(
    parser: SSHConfigParser, select_pattern: Optional[str], set_outcome
) -> None:
    """Internal implementation for sorting."""
    import re

    all_files = parser.find_include_files()
    if not all_files:
        console.print("[red]Error: No Include files found in ~/.ssh/config[/red]")
        set_outcome(FAIL)
        raise SystemExit(1)

    if select_pattern:
        try:
            pattern = re.compile(select_pattern, re.IGNORECASE)
        except re.error as e:
            console.print(f"[red]Invalid regex pattern: {e}[/red]")
            set_outcome(FAIL)
            raise SystemExit(1)
        files_to_sort = [f for f in all_files if pattern.search(f.stem)]
        if not files_to_sort:
            console.print(
                f"[yellow]No config files match pattern: {select_pattern}[/yellow]"
            )
            raise SystemExit(0)
    else:
        files_to_sort = all_files

    # Phase 1: Analyze all files
    analyses: List[FileAnalysis] = []
    for file_path in files_to_sort:
        try:
            analyses.append(_analyze_file(parser, file_path))
        except Exception as exc:  # pragma: no cover - defensive logging
            console.print(f"[red]Error analyzing {file_path.name}: {exc}[/red]")
            continue

    if not analyses:
        set_outcome(FAIL)
        raise SystemExit(1)

    # Phase 2: Display summary table
    total_hosts = sum(a.host_count for a in analyses)
    total_dups = sum(len(a.duplicates) for a in analyses)
    total_misplaced = sum(len(a.misplaced) for a in analyses)

    rows = [
        (
            a.path.name,
            str(a.host_count),
            _format_count(len(a.duplicates)),
            _format_count(len(a.misplaced)),
        )
        for a in analyses
    ]

    ui.print_table(
        (
            ("Include File", "cyan"),
            ("Hosts", "dim"),
            ("Duplicates", ""),
            ("Misplaced", ""),
        ),
        rows,
        footer=("Total", str(total_hosts), str(total_dups), str(total_misplaced)),
    )

    # Phase 3: Apply fixes if needed
    files_with_issues = [a for a in analyses if a.has_issues]

    if not files_with_issues:
        console.print("\n[green]✓[/green] All files sorted, no issues found")
        set_outcome(NOOP)
        return

    console.print()
    fixed_misplaced = 0
    fixed_dups = 0

    for analysis in files_with_issues:
        dup_count = len(analysis.duplicates)
        mis_count = len(analysis.misplaced)

        # Show duplicates if any
        if analysis.duplicates:
            dup_names = ", ".join(sorted(analysis.duplicates.keys())[:5])
            if len(analysis.duplicates) > 5:
                dup_names += f" (+{len(analysis.duplicates) - 5} more)"
            console.print(f"[dim]Duplicates: {dup_names}[/dim]")

        # Show reorder preview if misplaced
        if analysis.misplaced:
            _show_reorder_preview(analysis.hosts, analysis.misplaced)

        # Build description of what will be fixed
        parts = []
        if dup_count:
            parts.append(f"{dup_count} duplicate{'s' if dup_count != 1 else ''}")
        if mis_count:
            parts.append(f"{mis_count} misplaced")

        if not confirm(f"Fix {analysis.path.name} ({', '.join(parts)})?", default=True):
            console.print(f"[yellow]![/yellow] Skipped {analysis.path.name}")
            continue

        try:
            m, d = _apply_fixes(parser, analysis)
            fixed_misplaced += m
            fixed_dups += d
            console.print(
                f"[green]✓[/green] Fixed {analysis.path.name} ({', '.join(parts)})"
            )
        except Exception as exc:  # pragma: no cover
            console.print(f"[red]✗[/red] Error fixing {analysis.path.name}: {exc}")

    parser.rebuild_index()

    if fixed_misplaced == 0 and fixed_dups == 0:
        set_outcome(NOOP)
