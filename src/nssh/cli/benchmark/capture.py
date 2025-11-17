"""Capture command wiring for nssh benchmark."""

from __future__ import annotations

import os
from pathlib import Path
from typing import Dict, List, Optional

from nssh.cli import typer

from nssh.core.diag import timing as timing_core

from . import common


def capture_command(
    host: str = typer.Argument(..., help="Host alias passed to nssh"),
    output: Path = typer.Option(
        Path("timing.log"),
        "--output",
        "-o",
        help="Destination for captured timing log",
    ),
    dry_run: bool = typer.Option(
        False,
        "--dry-run/--real-run",
        help="Use a ProxyCommand stub instead of establishing a real SSH session",
    ),
    session_exit: bool = typer.Option(
        True,
        "--session-exit/--no-session-exit",
        help="Automatically close the SSH session (sends EOF via -n)",
    ),
    ssh_arg: Optional[List[str]] = typer.Option(
        None,
        "--ssh-arg",
        "-a",
        help="Additional arguments forwarded to nssh (repeatable)",
    ),
    warmups: int = typer.Option(1, "--warmups", help="Warmup runs ignored in summary"),
    samples: int = typer.Option(3, "--samples", help="Measured runs captured"),
    json_output: Optional[Path] = typer.Option(
        None,
        "--json-output",
        help="Write benchmark summary JSON to PATH",
    ),
    stage_budget: Optional[List[str]] = typer.Option(
        None,
        "--stage-budget",
        help="Enforce stage budget (stage=MS). Repeat for multiple stages",
    ),
    total_budget: Optional[int] = typer.Option(
        None,
        "--total-budget",
        help="Fail if total metric exceeds this many ms",
    ),
    budget_metric: str = typer.Option(
        "max",
        "--budget-metric",
        help="Metric for budget enforcement: max, mean, or median",
        show_default=True,
    ),
    simple_only: bool = typer.Option(
        False,
        "--simple-only/--structured",
        help="Disable instrumentation and report only total wall-clock durations",
    ),
) -> None:
    """Capture a timing log by running nssh with NSSH_DEBUG=1."""
    if samples < 1:
        raise typer.BadParameter("--samples must be at least 1")
    if warmups < 0:
        raise typer.BadParameter("--warmups must be >= 0")

    try:
        stage_budgets = common.parse_stage_budget(stage_budget)
    except ValueError as exc:
        raise typer.BadParameter(str(exc)) from exc

    metric_choice = budget_metric.lower()
    if metric_choice not in {"max", "mean", "median"}:
        raise typer.BadParameter("--budget-metric must be one of: max, mean, median")

    if simple_only and (stage_budgets or total_budget is not None):
        common.console.print(
            "[yellow]Stage/total budgets are ignored in --simple-only mode.[/yellow]"
        )
        stage_budgets = {}
        total_budget = None
    if simple_only and json_output:
        common.console.print(
            "[yellow]--json-output is ignored in --simple-only mode.[/yellow]"
        )
        json_output = None

    try:
        nssh_binary = common.resolve_nssh_binary()
    except FileNotFoundError as exc:
        common.console.print(f"[red]{exc}[/red]")
        raise typer.Exit(1)
    base_cmd = [nssh_binary, host]
    if dry_run:
        base_cmd.extend(["-o", "ProxyCommand=echo 'Would connect'; exit 1"])
    if ssh_arg:
        base_cmd.extend(list(ssh_arg))
    if not dry_run and session_exit:
        base_cmd.append("-n")

    env_base: Dict[str, str] = os.environ.copy()
    env_base.setdefault("NSSH_RECORD", "0")
    if not simple_only:
        env_base["NSSH_DEBUG"] = "1"

    total_runs = warmups + samples
    common.ui.show_panel(
        "Capturing timing data",
        f"Warmups: {warmups}  |  Samples: {samples}",
        style="cyan",
    )
    common.console.print("[dim]Command:[/dim] " + " ".join(base_cmd))

    if simple_only:
        try:
            common.run_simple_capture(base_cmd, env_base, warmups, samples)
        except RuntimeError:
            raise typer.Exit(1) from None
        return

    recorded_samples: List[timing_core.BenchmarkSample] = []
    aggregated_lines: List[str] = []
    last_entries: List[timing_core.TimingEntry] = []

    for idx in range(1, total_runs + 1):
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
            raise typer.Exit(1)

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

    output = output.expanduser()
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text("\n".join(aggregated_lines).rstrip() + "\n")
    common.console.print(f"[green]Timing log written to[/green] {output}")

    if recorded_samples:
        summary = timing_core.summarize_benchmark(recorded_samples)
        common.render_benchmark_summary(summary)

        if stage_budgets or total_budget is not None:
            try:
                timing_core.enforce_budgets(
                    summary,
                    stage_budgets=stage_budgets,
                    total_budget_ms=total_budget,
                    metric=metric_choice,
                )
            except timing_core.TimingBudgetError as exc:
                common.console.print(f"[red]Budget violation: {exc}[/red]")
                raise typer.Exit(2)

        if json_output:
            common.write_summary_json(json_output, summary)
        return

    if not last_entries:
        common.console.print("[red]No timing entries were captured.[/red]")
        raise typer.Exit(1)

    try:
        legacy_summary = timing_core.build_summary(last_entries)
    except timing_core.TimingDataError as exc:
        common.console.print(f"[red]{exc}[/red]")
        raise typer.Exit(1)

    if stage_budgets or total_budget:
        common.console.print(
            "[yellow]Budgets ignored because stage instrumentation was not detected.[/yellow]"
        )

    common.render_event_summary(legacy_summary)
