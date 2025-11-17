from __future__ import annotations

from contextlib import contextmanager
from dataclasses import dataclass
from datetime import datetime
from pathlib import Path
from typing import (
    Any,
    Callable,
    Dict,
    Iterable,
    List,
    Mapping,
    Optional,
    Sequence,
    Tuple,
)

import os
import sys
import time

TimestampMs = int
DurationUs = int

TIMING_ENV_FLAG = "NSSH_DEBUG"
RUN_ID_ENV_FLAG = "NSSH_BENCHMARK_RUN"
DEFAULT_STAGE_ORDER: Tuple[str, ...] = (
    "wrapper-start",
    "config-parse",
    "host-selection",
    "credential-vault",
    "connection-orchestration",
    "recording-setup",
    "ssh-connection",
    "wrapper-teardown",
)


@dataclass(frozen=True)
class TimingEntry:
    """Single raw TIMING line parsed from application output."""

    timestamp_ms: TimestampMs
    message: str
    payload: Optional[Mapping[str, Any]] = None


@dataclass(frozen=True)
class TimingEvent:
    """Derived event with elapsed/delta timings for display/export."""

    timestamp_ms: TimestampMs
    elapsed_ms: float
    delta_ms: float
    message: str


@dataclass(frozen=True)
class TimingSummary:
    """Aggregate timing result suitable for rendering or archiving."""

    total_ms: float
    events: Tuple[TimingEvent, ...]


@dataclass(frozen=True)
class StageEvent:
    """Structured stage event emitted by instrumentation spans."""

    stage: str
    phase: str
    run_id: Optional[int]
    timestamp_ms: TimestampMs
    duration_ms: Optional[float] = None
    detail: Optional[str] = None


@dataclass(frozen=True)
class RunEvent:
    """Structured run-level event emitted by instrumentation spans."""

    phase: str
    run_id: Optional[int]
    timestamp_ms: TimestampMs
    duration_ms: Optional[float] = None
    status: Optional[str] = None
    detail: Optional[str] = None


@dataclass(frozen=True)
class StageSample:
    """Completed stage measurement for a specific run."""

    stage: str
    duration_ms: float
    run_id: int
    detail: Optional[str] = None
    finished_at_ms: Optional[TimestampMs] = None


@dataclass(frozen=True)
class BenchmarkSample:
    """Aggregate timing information for one benchmark run."""

    run_id: int
    stages: Tuple[StageSample, ...]
    total_ms: float
    status: str = "ok"


@dataclass(frozen=True)
class StageStats:
    """Statistical rollup for a measured stage across runs."""

    stage: str
    count: int
    mean_ms: float
    median_ms: float
    min_ms: float
    max_ms: float
    variance_ms: float
    stddev_ms: float


@dataclass(frozen=True)
class BenchmarkSummary:
    """Top-level benchmark summary with per-stage stats."""

    samples: Tuple[BenchmarkSample, ...]
    stage_stats: Mapping[str, StageStats]
    total_stats: StageStats
    ordered_stages: Tuple[str, ...]


class TimingDataError(ValueError):
    """Raised when timing data is missing or malformed."""


class TimingBudgetError(RuntimeError):
    """Raised when measured timings exceed declared budgets."""

    def __init__(self, violations: Sequence[str]):
        self.violations = tuple(violations)
        message = "; ".join(violations)
        super().__init__(message)


def _safe_int(value: Any) -> Optional[int]:
    if value is None:
        return None
    try:
        return int(value)
    except (TypeError, ValueError):
        return None


def _safe_float(value: Any) -> Optional[float]:
    if value is None:
        return None
    try:
        return float(value)
    except (TypeError, ValueError):
        return None


class StructuredTimingLogger:
    """Emit structured timing events that downstream tooling can parse."""

    def __init__(
        self,
        *,
        enabled: Optional[bool] = None,
        writer: Optional[Callable[[str], None]] = None,
    ) -> None:
        self.enabled = (
            enabled if enabled is not None else os.getenv(TIMING_ENV_FLAG, "0") == "1"
        )
        self._writer = writer or (lambda text: print(text, file=sys.stderr))

    @staticmethod
    def _timestamp_ms() -> TimestampMs:
        return int(time.time() * 1000)

    @staticmethod
    def _perf_ns() -> int:
        return time.perf_counter_ns()

    @staticmethod
    def _run_id() -> Optional[int]:
        value = os.getenv(RUN_ID_ENV_FLAG)
        return _safe_int(value)

    def emit(self, payload: Mapping[str, Any]) -> None:
        if not self.enabled:
            return
        body: Dict[str, Any] = dict(payload)
        if "run" not in body:
            run_id = self._run_id()
            if run_id is not None:
                body["run"] = run_id
        import json

        serialized = json.dumps(body, separators=(",", ":"))
        self._writer(f"[{self._timestamp_ms()}] TIMING: {serialized}")

    def emit_log(self, message: str) -> None:
        self.emit({"kind": "log", "message": message})

    def begin_stage(
        self, stage: str, detail: Optional[str] = None
    ) -> Optional[Tuple[str, int, Optional[str]]]:
        if not self.enabled:
            return None
        self.emit({"kind": "stage", "stage": stage, "phase": "start", "detail": detail})
        return (stage, self._perf_ns(), detail)

    def end_stage(self, token: Optional[Tuple[str, int, Optional[str]]]) -> None:
        if not self.enabled or token is None:
            return
        stage, started_ns, detail = token
        duration_ms = max((self._perf_ns() - started_ns) / 1_000_000, 0.0)
        payload = {
            "kind": "stage",
            "stage": stage,
            "phase": "finish",
            "duration_ms": duration_ms,
        }
        if detail:
            payload["detail"] = detail
        self.emit(payload)

    def begin_run(
        self, label: str, detail: Optional[str] = None
    ) -> Optional[Tuple[str, int, Optional[str]]]:
        if not self.enabled:
            return None
        self.emit({"kind": "run", "phase": "start", "label": label, "detail": detail})
        return (label, self._perf_ns(), detail)

    def end_run(
        self,
        token: Optional[Tuple[str, int, Optional[str]]],
        *,
        status: str = "ok",
        exit_code: Optional[int] = None,
    ) -> None:
        if not self.enabled or token is None:
            return
        label, started_ns, detail = token
        duration_ms = max((self._perf_ns() - started_ns) / 1_000_000, 0.0)
        payload = {
            "kind": "run",
            "phase": "finish",
            "label": label,
            "status": status,
            "duration_ms": duration_ms,
        }
        if detail:
            payload["detail"] = detail
        if exit_code is not None:
            payload["exit_code"] = exit_code
        self.emit(payload)


_DEFAULT_LOGGER = StructuredTimingLogger()


def get_logger() -> StructuredTimingLogger:
    """Return the process-wide structured timing logger."""

    return _DEFAULT_LOGGER


@contextmanager
def stage(
    name: str,
    detail: Optional[str] = None,
    logger: Optional[StructuredTimingLogger] = None,
):
    """Context manager recording start/end events for a stage."""

    log = logger or get_logger()
    token = log.begin_stage(name, detail)
    try:
        yield
    finally:
        log.end_stage(token)


@contextmanager
def run_span(
    label: str,
    detail: Optional[str] = None,
    logger: Optional[StructuredTimingLogger] = None,
):
    """Context manager for the entire benchmarked run."""

    log = logger or get_logger()
    token = log.begin_run(label, detail)
    try:
        yield
    except BaseException as exc:  # pragma: no cover - defensive
        status = "error"
        exit_code = None
        if isinstance(exc, SystemExit):
            exit_code = exc.code if isinstance(exc.code, int) else None
            status = "error" if exit_code not in (None, 0) else "ok"
        log.end_run(token, status=status, exit_code=exit_code)
        raise
    else:
        log.end_run(token)


def parse_timing_lines(raw_lines: Sequence[str]) -> List[TimingEntry]:
    """Extract structured TimingEntry records from raw log lines."""

    import json

    entries: List[TimingEntry] = []
    for raw in raw_lines:
        if "TIMING:" not in raw:
            continue
        if "[" not in raw or "]" not in raw:
            continue
        prefix, *rest = raw.split("]", 1)
        timestamp_str = prefix.replace("[", "").strip()
        if not timestamp_str.isdigit():
            continue
        message = rest[0] if rest else ""
        message = message.replace("TIMING:", "").strip()
        payload: Optional[Dict[str, Any]] = None
        if message.startswith("{") and message.endswith("}"):
            try:
                payload = json.loads(message)
            except json.JSONDecodeError:
                payload = None
        entries.append(
            TimingEntry(
                timestamp_ms=int(timestamp_str),
                message=message,
                payload=payload,
            )
        )
    return entries


def _stage_events(entries: Iterable[TimingEntry]) -> List[StageEvent]:
    events: List[StageEvent] = []
    for entry in entries:
        payload = entry.payload or {}
        if payload.get("kind") != "stage":
            continue
        events.append(
            StageEvent(
                stage=str(payload.get("stage", "unknown")),
                phase=str(payload.get("phase", "")),
                run_id=_safe_int(payload.get("run")),
                timestamp_ms=entry.timestamp_ms,
                duration_ms=_safe_float(payload.get("duration_ms")),
                detail=payload.get("detail"),
            )
        )
    return events


def _run_events(entries: Iterable[TimingEntry]) -> List[RunEvent]:
    events: List[RunEvent] = []
    for entry in entries:
        payload = entry.payload or {}
        if payload.get("kind") != "run":
            continue
        events.append(
            RunEvent(
                phase=str(payload.get("phase", "")),
                run_id=_safe_int(payload.get("run")),
                timestamp_ms=entry.timestamp_ms,
                duration_ms=_safe_float(payload.get("duration_ms")),
                status=payload.get("status"),
                detail=payload.get("detail"),
            )
        )
    return events


def build_benchmark_samples(
    entries: Sequence[TimingEntry],
    *,
    default_run_id: Optional[int] = None,
) -> List[BenchmarkSample]:
    """Convert structured log entries into benchmark samples."""

    stage_events = _stage_events(entries)
    if not stage_events:
        return []

    run_events = _run_events(entries)
    stage_map: Dict[int, List[StageSample]] = {}
    for stage_event in stage_events:
        if stage_event.phase != "finish" or stage_event.duration_ms is None:
            continue
        run_id = stage_event.run_id or default_run_id or 1
        duration_ms = float(stage_event.duration_ms)
        stage_map.setdefault(run_id, []).append(
            StageSample(
                stage=stage_event.stage,
                duration_ms=duration_ms,
                run_id=run_id,
                detail=stage_event.detail,
                finished_at_ms=stage_event.timestamp_ms,
            )
        )

    totals: Dict[int, float] = {}
    statuses: Dict[int, str] = {}
    for run_event in run_events:
        if run_event.phase != "finish" or run_event.duration_ms is None:
            continue
        run_id = run_event.run_id or default_run_id or 1
        totals[run_id] = float(run_event.duration_ms)
        if run_event.status:
            statuses[run_id] = run_event.status

    samples: List[BenchmarkSample] = []
    for run_id in sorted(stage_map):
        stages_for_run = tuple(
            sorted(
                stage_map[run_id],
                key=lambda sample: sample.finished_at_ms or 0,
            )
        )
        total_ms = totals.get(run_id)
        if total_ms is None:
            total_ms = sum(stage.duration_ms for stage in stages_for_run)
        samples.append(
            BenchmarkSample(
                run_id=run_id,
                stages=stages_for_run,
                total_ms=total_ms,
                status=statuses.get(run_id, "ok"),
            )
        )

    return samples


def _compute_stage_stats(stage: str, durations: Sequence[float]) -> StageStats:
    import math
    import statistics

    mean_ms = statistics.mean(durations)
    median_ms = statistics.median(durations)
    variance_ms = statistics.pvariance(durations) if len(durations) > 1 else 0.0
    stddev_ms = math.sqrt(variance_ms)
    return StageStats(
        stage=stage,
        count=len(durations),
        mean_ms=mean_ms,
        median_ms=median_ms,
        min_ms=min(durations),
        max_ms=max(durations),
        variance_ms=variance_ms,
        stddev_ms=stddev_ms,
    )


def summarize_benchmark(samples: Sequence[BenchmarkSample]) -> BenchmarkSummary:
    """Roll benchmark samples into per-stage summary statistics."""

    if not samples:
        raise TimingDataError("No benchmark samples provided")

    stage_order: List[str] = list(DEFAULT_STAGE_ORDER)
    for sample in samples:
        for stage_sample in sample.stages:
            if stage_sample.stage not in stage_order:
                stage_order.append(stage_sample.stage)

    stage_timings: Dict[str, List[float]] = {name: [] for name in stage_order}
    for sample in samples:
        for stage_sample in sample.stages:
            stage_timings.setdefault(stage_sample.stage, []).append(
                stage_sample.duration_ms
            )

    # Remove empty placeholders that never received data
    stage_timings = {k: v for k, v in stage_timings.items() if v}
    if not stage_timings:
        raise TimingDataError("No stage timings available in samples")

    stage_stats = {
        stage: _compute_stage_stats(stage, durations)
        for stage, durations in stage_timings.items()
    }
    total_stats = _compute_stage_stats(
        "total",
        [sample.total_ms for sample in samples],
    )

    ordered_stages = tuple(stage for stage in stage_order if stage in stage_stats)
    return BenchmarkSummary(
        samples=tuple(samples),
        stage_stats=stage_stats,
        total_stats=total_stats,
        ordered_stages=ordered_stages,
    )


def summary_to_table(
    summary: BenchmarkSummary, include_footer: bool = True
) -> Tuple[List[Tuple[str, str]], List[Tuple[str, ...]], Optional[Tuple[str, ...]]]:
    """Return headers, rows, and optional footer for Rich table rendering.

    Args:
        summary: The benchmark summary to convert to table format
        include_footer: If True, returns TOTAL row as a footer (default: True)

    Returns:
        Tuple of (headers, rows, footer) where footer is None if include_footer is False
    """

    headers = [
        ("Stage", "cyan"),
        ("Mean", "green"),
        ("Median", "green"),
        ("Min/Max", "magenta"),
        ("Std Dev", "yellow"),
        ("Samples", "white"),
    ]

    rows: List[Tuple[str, ...]] = []
    for stage in summary.ordered_stages:
        stats = summary.stage_stats[stage]
        rows.append(
            (
                stage,
                format_duration_ms(stats.mean_ms),
                format_duration_ms(stats.median_ms),
                f"{format_duration_ms(stats.min_ms)} / {format_duration_ms(stats.max_ms)}",
                format_duration_ms(stats.stddev_ms),
                str(stats.count),
            )
        )

    footer = None
    if include_footer:
        footer = (
            "TOTAL",
            format_duration_ms(summary.total_stats.mean_ms),
            format_duration_ms(summary.total_stats.median_ms),
            f"{format_duration_ms(summary.total_stats.min_ms)} / {format_duration_ms(summary.total_stats.max_ms)}",
            format_duration_ms(summary.total_stats.stddev_ms),
            str(summary.total_stats.count),
        )

    return headers, rows, footer


def summary_to_dict(summary: BenchmarkSummary) -> Dict[str, Any]:
    """Serialize a BenchmarkSummary into a JSON-friendly payload."""

    return {
        "runs": [
            {
                "run_id": sample.run_id,
                "status": sample.status,
                "total_ms": sample.total_ms,
                "stages": [
                    {
                        "stage": stage_sample.stage,
                        "duration_ms": stage_sample.duration_ms,
                        "detail": stage_sample.detail,
                    }
                    for stage_sample in sample.stages
                ],
            }
            for sample in summary.samples
        ],
        "stages": [
            {
                "stage": stage,
                "mean_ms": summary.stage_stats[stage].mean_ms,
                "median_ms": summary.stage_stats[stage].median_ms,
                "min_ms": summary.stage_stats[stage].min_ms,
                "max_ms": summary.stage_stats[stage].max_ms,
                "variance_ms": summary.stage_stats[stage].variance_ms,
                "stddev_ms": summary.stage_stats[stage].stddev_ms,
                "samples": summary.stage_stats[stage].count,
            }
            for stage in summary.ordered_stages
        ],
        "total": {
            "mean_ms": summary.total_stats.mean_ms,
            "median_ms": summary.total_stats.median_ms,
            "min_ms": summary.total_stats.min_ms,
            "max_ms": summary.total_stats.max_ms,
            "variance_ms": summary.total_stats.variance_ms,
            "stddev_ms": summary.total_stats.stddev_ms,
            "samples": summary.total_stats.count,
        },
    }


def enforce_budgets(
    summary: BenchmarkSummary,
    *,
    stage_budgets: Optional[Mapping[str, int]] = None,
    total_budget_ms: Optional[int] = None,
    metric: str = "max",
) -> None:
    """Raise TimingBudgetError if any provided budget is violated."""

    metric = metric.lower()
    if metric not in {"max", "mean", "median"}:
        raise ValueError("Metric must be one of: max, mean, median")

    def _value(stats: StageStats) -> float:
        if metric == "mean":
            return stats.mean_ms
        if metric == "median":
            return stats.median_ms
        return float(stats.max_ms)

    violations: List[str] = []
    stages = stage_budgets or {}
    for stage_name, limit in stages.items():
        stats = summary.stage_stats.get(stage_name)
        if stats is None:
            violations.append(f"No measurements found for stage '{stage_name}'")
            continue
        observed = _value(stats)
        if observed > limit:
            violations.append(
                f"Stage '{stage_name}' {metric}={observed:.1f}ms exceeds {limit}ms"
            )

    if total_budget_ms is not None:
        total_value = _value(summary.total_stats)
        if total_value > total_budget_ms:
            violations.append(
                f"Total {metric}={total_value:.1f}ms exceeds {total_budget_ms}ms"
            )

    if violations:
        raise TimingBudgetError(violations)


def build_summary(entries: Sequence[TimingEntry]) -> TimingSummary:
    """Convert parsed entries into elapsed/delta summaries."""

    if not entries:
        raise TimingDataError("No timing entries provided")

    start = entries[0].timestamp_ms
    events: List[TimingEvent] = []
    prev = start
    for entry in entries:
        elapsed = entry.timestamp_ms - start
        delta = entry.timestamp_ms - prev
        events.append(
            TimingEvent(
                timestamp_ms=entry.timestamp_ms,
                elapsed_ms=elapsed,
                delta_ms=delta,
                message=entry.message,
            )
        )
        prev = entry.timestamp_ms

    total_ms = events[-1].elapsed_ms
    return TimingSummary(total_ms=total_ms, events=tuple(events))


def archive_summary(
    archive_dir: Path,
    label: Optional[str],
    raw_lines: Sequence[str],
    summary: TimingSummary,
    clock: Optional[Callable[[], datetime]] = None,
) -> Tuple[Path, Path]:
    """Persist raw + JSON timing data for later comparison."""

    import json

    archive_dir.mkdir(parents=True, exist_ok=True)
    now = clock() if clock else datetime.now()
    timestamp = now.strftime("%Y%m%d-%H%M%S")
    label_slug = label.replace(" ", "-") if label else None
    base_name = f"{timestamp}-{label_slug}" if label_slug else timestamp

    raw_path = archive_dir / f"{base_name}.log"
    raw_path.write_text("\n".join(raw_lines) + "\n")

    json_events = [
        {
            "timestamp_ms": event.timestamp_ms,
            "elapsed_ms": event.elapsed_ms,
            "delta_ms": event.delta_ms,
            "message": event.message,
        }
        for event in summary.events
    ]
    summary_payload = {
        "recorded_at": timestamp,
        "label": label,
        "total_ms": summary.total_ms,
        "events": json_events,
    }
    summary_path = archive_dir / f"{base_name}.json"
    summary_path.write_text(json.dumps(summary_payload, indent=2) + "\n")

    return raw_path, summary_path


def format_duration_ms(value: float) -> str:
    """Format a duration stored in milliseconds using ms/µs units."""

    import math

    if math.isnan(value):  # pragma: no cover - defensive
        return "n/a"
    if abs(value) >= 1.0:
        return f"{value:.1f} ms"
    return f"{value * 1000:.1f} µs"
