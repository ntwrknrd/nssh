"""Shared helpers for the nssh benchmark CLI."""

from __future__ import annotations

import shutil
import statistics
import subprocess
import sys
import time
from pathlib import Path
from typing import Dict, List, Sequence, Tuple

from nssh.cli.common import ui
from nssh.core.diag import timing as timing_core
from nssh.core.ui.console import create_standard_table, get_console

console = get_console()
APP_TITLE = "nssh benchmark"


def resolve_nssh_binary() -> str | List[str]:
    """Return a resolvable path/command to execute nssh.

    Returns either:
    - str: path to nssh binary if found on PATH
    - List[str]: command prefix for development mode (e.g., [sys.executable, "-m", "nssh.cli.main"])
    """
    path = shutil.which("nssh")
    if path:
        return path

    # Development mode: check if we're in a repo with nssh package
    repo_root = Path(__file__).resolve().parents[3]
    package_dir = repo_root / "src" / "nssh"
    if package_dir.exists() and (package_dir / "cli" / "main.py").exists():
        console.print(
            "[yellow]![/yellow] nssh not on PATH, using development mode (python -m nssh.cli.main)"
        )
        return [sys.executable, "-m", "nssh.cli.main"]

    raise FileNotFoundError(
        "Unable to locate nssh binary. Install with 'uv tool install .'"
    )


def collect_timing_lines(
    cmd: Sequence[str], env: Dict[str, str]
) -> Tuple[List[str], subprocess.CompletedProcess[str]]:
    """Run a command and collect TIMING lines."""
    result = subprocess.run(
        cmd,
        capture_output=True,
        text=True,
        env=env,
        check=False,
        stdin=subprocess.DEVNULL,
    )

    raw_lines: List[str] = []
    for stream in (result.stdout, result.stderr):
        if not stream:
            continue
        raw_lines.extend(line for line in stream.splitlines() if "TIMING:" in line)

    return raw_lines, result


def run_simple_capture(
    base_cmd: List[str],
    env: Dict[str, str],
    warmups: int,
    samples: int,
) -> None:
    """Capture totals only (no instrumentation)."""
    total_runs = warmups + samples
    totals: List[float] = []
    for idx in range(1, total_runs + 1):
        cmd = list(base_cmd)
        start_ns = time.perf_counter_ns()
        result = subprocess.run(
            cmd,
            capture_output=True,
            text=True,
            env=env,
            check=False,
            stdin=subprocess.DEVNULL,
        )
        end_ns = time.perf_counter_ns()
        duration_ms = (end_ns - start_ns) / 1_000_000

        if idx <= warmups:
            console.print(f"[dim]Warmup run {idx}/{warmups} complete (ignored).[/dim]")
            continue

        totals.append(duration_ms)
        console.print(
            f"[dim]Sample {idx - warmups}/{samples}: total {timing_core.format_duration_ms(duration_ms)}[/dim]"
        )

        if result.returncode not in (0,):
            console.print(
                f"[yellow]Underlying nssh exited with {result.returncode}; continuing for timing.[/yellow]"
            )

    if not totals:
        console.print("[red]No samples recorded.[/red]")
        raise RuntimeError("no samples")

    mean_ms = statistics.mean(totals)
    median_ms = statistics.median(totals)
    min_ms = min(totals)
    max_ms = max(totals)
    std_ms = statistics.pstdev(totals) if len(totals) > 1 else 0.0

    summary_body = (
        f"Mean total: {timing_core.format_duration_ms(mean_ms)}  |  "
        f"Max: {timing_core.format_duration_ms(max_ms)}  |  "
        f"Std Dev: {timing_core.format_duration_ms(std_ms)}  |  Samples: {len(totals)}"
    )
    ui.show_panel("nssh benchmark", summary_body, style="cyan")

    table = create_standard_table(
        [("Statistic", "cyan"), ("Value", "green")],
        [
            ("Mean", timing_core.format_duration_ms(mean_ms)),
            ("Median", timing_core.format_duration_ms(median_ms)),
            (
                "Min/Max",
                f"{timing_core.format_duration_ms(min_ms)} / {timing_core.format_duration_ms(max_ms)}",
            ),
            ("Std Dev", timing_core.format_duration_ms(std_ms)),
        ],
    )
    console.print(table)

    console.print(
        "[yellow]Stage-level metrics disabled (simple timing uses a single wall-clock measurement).[/yellow]"
    )


def render_event_summary(summary: timing_core.TimingSummary) -> None:
    """Pretty-print legacy timing summaries."""
    ui.show_panel(
        "nssh benchmark",
        f"Timing summary (total overhead: {summary.total_ms}ms)",
        style="cyan",
    )

    table_rows = [
        (str(event.elapsed_ms), str(event.delta_ms), event.message)
        for event in summary.events
    ]
    table = create_standard_table(
        [("Elapsed (ms)", "cyan"), ("Delta (ms)", "magenta"), ("Event", "")],
        table_rows,
    )
    console.print(table)


def render_benchmark_summary(summary: timing_core.BenchmarkSummary) -> None:
    """Render Rich output for structured benchmark summaries."""
    headers, rows, footer = timing_core.summary_to_table(summary, include_footer=True)
    table = create_standard_table(headers, rows, footer=footer)
    console.print(table)

    # Validate timing and display warnings
    validation_warnings = timing_core.validate_stage_timing(
        summary.samples,
        gap_threshold_ms=10.0,
        overlap_threshold_ms=5.0,
    )

    if validation_warnings:
        console.print("\n[yellow]Timing Validation:[/yellow]")
        for warning in validation_warnings:
            severity_colors = {"info": "dim", "warning": "yellow", "error": "red"}
            color = severity_colors.get(warning.severity, "white")

            # Group warnings by run if multiple runs have issues
            console.print(
                f"  [{color}]Run {warning.run_id}: {warning.message}[/{color}]"
            )
