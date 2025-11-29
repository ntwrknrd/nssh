"""Capture command wiring for nssh benchmark."""

from __future__ import annotations

import json
import os
import platform
from datetime import datetime
from pathlib import Path
from typing import Dict, List

from nssh.cli import click
from nssh.cli.common.banner import FAIL, OK, banner
from nssh.core.diag import timing as timing_core
from nssh.core.env.paths import project_root

from . import common


def _save_benchmark_archive(
    archive_dir: Path,
    output_txt_path: Path,
    metadata_path: Path,
    host: str,
    warmups: int,
    samples: int,
    simple_only: bool,
    no_record: bool,
    summary: timing_core.BenchmarkSummary | timing_core.TimingSummary | None = None,
) -> None:
    """Save output.txt, metadata.json, and create latest symlink."""
    import importlib.metadata

    # Get nssh version
    try:
        nssh_version = importlib.metadata.version("nssh")
    except importlib.metadata.PackageNotFoundError:
        nssh_version = "dev"

    # Create metadata
    metadata = {
        "timestamp": datetime.now().isoformat(),
        "host": host,
        "warmups": warmups,
        "samples": samples,
        "simple_only": simple_only,
        "no_record": no_record,
        "nssh_version": nssh_version,
        "python_version": platform.python_version(),
    }
    metadata_path.write_text(json.dumps(metadata, indent=2) + "\n")

    # Create output.txt with summary
    output_lines = [
        "nssh Benchmark Results",
        "=" * 50,
        f"Timestamp: {metadata['timestamp']}",
        f"Host: {host}",
        f"Warmups: {warmups}",
        f"Samples: {samples}",
        f"Simple mode: {simple_only}",
        f"Recording disabled: {no_record}",
        f"nssh version: {nssh_version}",
        f"Python version: {metadata['python_version']}",
        "",
    ]

    if summary:
        output_lines.append("Summary Statistics:")
        output_lines.append("-" * 50)
        if isinstance(summary, timing_core.BenchmarkSummary):
            # Structured benchmark summary
            for stage_name in summary.ordered_stages:
                stage = summary.stage_stats[stage_name]
                output_lines.append(
                    f"{stage.stage}: "
                    f"mean={timing_core.format_duration_ms(stage.mean_ms)}, "
                    f"median={timing_core.format_duration_ms(stage.median_ms)}, "
                    f"min={timing_core.format_duration_ms(stage.min_ms)}, "
                    f"max={timing_core.format_duration_ms(stage.max_ms)}"
                )
        else:
            # Legacy summary
            output_lines.append(f"Total: {summary.total_ms}ms")
            for event in summary.events:
                output_lines.append(
                    f"  {event.message}: {event.elapsed_ms}ms (+{event.delta_ms}ms)"
                )

    output_txt_path.write_text("\n".join(output_lines) + "\n")

    # Create/update 'latest' symlink
    latest_link = archive_dir.parent / "latest"
    if latest_link.exists() or latest_link.is_symlink():
        latest_link.unlink()
    latest_link.symlink_to(archive_dir.name, target_is_directory=True)

    # Print success message
    common.console.print(
        f"\n[green]Benchmark results archived to:[/green] {archive_dir}"
    )
    common.console.print("[dim]  - timing.log: raw timing data")
    common.console.print("[dim]  - output.txt: summary and statistics")
    common.console.print("[dim]  - metadata.json: run configuration")
    common.console.print(f"[dim]  - Latest run: benchmark/latest -> {archive_dir.name}")


@click.command(short_help="SSH connection overhead")
@click.argument("host")
@click.option("--warmups", default=1, help="Warmup runs ignored in summary")
@click.option("--samples", default=3, help="Measured runs captured")
@click.option(
    "--simple-only/--structured",
    default=False,
    help="Report only total durations",
)
@click.option(
    "--no-record",
    is_flag=True,
    default=False,
    help="Disable session recording",
)
def capture_command(
    host: str,
    warmups: int,
    samples: int,
    simple_only: bool,
    no_record: bool,
) -> None:
    """Capture timing data and archive results in benchmark/{timestamp}/ directory."""
    if samples < 1:
        raise click.BadParameter("--samples must be at least 1")
    if warmups < 0:
        raise click.BadParameter("--warmups must be >= 0")

    with banner("BENCHMARK CAPTURE", OK) as set_outcome:
        _capture_impl(host, warmups, samples, simple_only, no_record, set_outcome)


def _capture_impl(
    host: str,
    warmups: int,
    samples: int,
    simple_only: bool,
    no_record: bool,
    set_outcome,
) -> None:
    """Internal implementation for benchmark capture."""
    # Clean up stale recording locks before starting benchmark
    from nssh.core.recording import manager as recording

    stale_count = recording.cleanup_stale_locks()
    if stale_count > 0:
        common.console.print(
            f"[dim]Cleaned {stale_count} stale recording lock(s)[/dim]"
        )

    # Create timestamped archive directory
    timestamp = datetime.now().strftime("%Y-%m-%d_%H-%M-%S")
    root = project_root()
    archive_dir = root / "benchmark" / timestamp
    archive_dir.mkdir(parents=True, exist_ok=True)

    # Prepare output file paths
    timing_log_path = archive_dir / "timing.log"
    output_txt_path = archive_dir / "output.txt"
    metadata_path = archive_dir / "metadata.json"

    try:
        nssh_binary = common.resolve_nssh_binary()
    except FileNotFoundError as exc:
        common.console.print(f"[red]{exc}[/red]")
        set_outcome(FAIL)
        raise SystemExit(1)

    # Handle both str (binary path) and list (dev mode command prefix)
    if isinstance(nssh_binary, list):
        base_cmd = nssh_binary + [host, "--", "echo", "nssh-benchmark-test"]
    else:
        base_cmd = [nssh_binary, host, "--", "echo", "nssh-benchmark-test"]

    env_base: Dict[str, str] = os.environ.copy()

    # Handle recording configuration
    if no_record:
        # Force disable recording when --no-record is specified
        env_base["NSSH_RECORD"] = "0"
    else:
        # Always enable headless mode for benchmarks to avoid terminal capability
        # detection timeouts (benchmarks are non-interactive by design)
        env_base["NSSH_RECORD_HEADLESS"] = "1"

    if not simple_only:
        env_base["NSSH_DEBUG"] = "1"

    common.console.print("[dim]Command:[/dim] " + " ".join(base_cmd))

    if simple_only:
        try:
            common.run_simple_capture(base_cmd, env_base, warmups, samples)
        except RuntimeError:
            set_outcome(FAIL)
            raise SystemExit(1) from None

        # For simple mode, save minimal metadata and output
        timing_log_path.write_text(
            "Simple mode: detailed timing instrumentation disabled\n"
            "Only total wall-clock measurements were captured\n"
        )
        _save_benchmark_archive(
            archive_dir,
            output_txt_path,
            metadata_path,
            host,
            warmups,
            samples,
            simple_only,
            no_record,
            None,
        )
        return

    recorded_samples: List[timing_core.BenchmarkSample] = []
    aggregated_lines: List[str] = []
    last_entries: List[timing_core.TimingEntry] = []

    for idx in range(1, warmups + samples + 1):
        cmd = list(base_cmd)
        env = env_base.copy()
        env["NSSH_BENCHMARK_RUN"] = str(idx)
        raw_lines, result = common.collect_timing_lines(cmd, env)
        aggregated_lines.extend(raw_lines)
        aggregated_lines.append("")

        if not raw_lines:
            common.console.print(
                "[yellow]No timing lines captured. Ensure NSSH_DEBUG is honored.[/yellow]"
            )
            set_outcome(FAIL)
            raise SystemExit(1)

        entries = timing_core.parse_timing_lines(raw_lines)
        last_entries = entries

        if idx <= warmups:
            common.console.print(
                f"[dim]Warmup run {idx}/{warmups} complete (ignored).[/dim]"
            )
            continue

        run_samples = timing_core.build_benchmark_samples(entries, default_run_id=idx)
        if not run_samples:
            common.console.print(
                "[yellow]Structured stage timings missing; falling back to legacy summary.[/yellow]"
            )
        else:
            recorded_samples.extend(run_samples)
            total_str = timing_core.format_duration_ms(run_samples[0].total_ms)
            common.console.print(
                f"[dim]Sample {idx - warmups}/{samples}: total {total_str}[/dim]"
            )

        if result.returncode not in (0,):
            common.console.print(
                f"[yellow]Underlying nssh exited with {result.returncode}; continuing for timing.[/yellow]"
            )

    # Write timing log
    timing_log_path.write_text("\n".join(aggregated_lines).rstrip() + "\n")

    if recorded_samples:
        summary = timing_core.summarize_benchmark(recorded_samples)
        common.render_benchmark_summary(summary)
        _save_benchmark_archive(
            archive_dir,
            output_txt_path,
            metadata_path,
            host,
            warmups,
            samples,
            simple_only,
            no_record,
            summary,
        )
        return

    if not last_entries:
        common.console.print("[red]No timing entries were captured.[/red]")
        set_outcome(FAIL)
        raise SystemExit(1)

    try:
        legacy_summary = timing_core.build_summary(last_entries)
    except timing_core.TimingDataError as exc:
        common.console.print(f"[red]{exc}[/red]")
        set_outcome(FAIL)
        raise SystemExit(1)

    common.render_event_summary(legacy_summary)
    _save_benchmark_archive(
        archive_dir,
        output_txt_path,
        metadata_path,
        host,
        warmups,
        samples,
        simple_only,
        no_record,
        legacy_summary,
    )
