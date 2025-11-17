"""Analyze command wiring for nssh benchmark."""

from __future__ import annotations

from pathlib import Path
from typing import Optional

from nssh.cli import typer

from nssh.core.diag import timing as timing_core

from . import common


def analyze_command(
    source: Optional[Path] = typer.Argument(
        None,
        help="Path to timing log (omit to read from stdin)",
    ),
    archive_dir: Optional[Path] = typer.Option(
        None,
        "--archive-dir",
        help="Directory to store archived timing artifacts",
    ),
    label: Optional[str] = typer.Option(
        None,
        "--label",
        help="Label saved with archived timing data",
    ),
) -> None:
    """Analyze timing logs and optionally archive results."""
    try:
        raw_lines = common.load_lines(source)
    except RuntimeError:
        raise typer.Exit(1) from None
    except FileNotFoundError as exc:
        common.console.print(f"[red]{exc}[/red]")
        raise typer.Exit(1)

    entries = timing_core.parse_timing_lines(raw_lines)
    if not entries:
        common.console.print("[yellow]No TIMING events found in input.[/yellow]")
        raise typer.Exit(1)

    samples = timing_core.build_benchmark_samples(entries)
    render_legacy = False
    if samples:
        try:
            benchmark_summary = timing_core.summarize_benchmark(samples)
        except timing_core.TimingDataError as exc:
            common.console.print(f"[red]{exc}[/red]")
            raise typer.Exit(1)
        common.render_benchmark_summary(benchmark_summary)
    else:
        render_legacy = True

    try:
        legacy_summary = timing_core.build_summary(entries)
    except timing_core.TimingDataError as exc:
        if render_legacy:
            common.console.print(f"[red]{exc}[/red]")
            raise typer.Exit(1)
        legacy_summary = None

    if render_legacy and legacy_summary:
        common.render_event_summary(legacy_summary)

    if archive_dir:
        archive_dir = archive_dir.expanduser()
        if legacy_summary is None:
            legacy_summary = timing_core.build_summary(entries)
        timing_core.archive_summary(archive_dir, label, raw_lines, legacy_summary)
        common.console.print(
            f"[green]Archived timing artifacts in[/green] {archive_dir}"
        )
