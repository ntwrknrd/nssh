from __future__ import annotations

import json
from datetime import datetime

import pytest

from nssh.core.diag import timing


SAMPLE_LINES = [
    "[100] TIMING: START: connect module",
    "[220] TIMING: END: connect module",
    "[245] TIMING: START: SSH connection",
]

STRUCTURED_LINES = [
    '[1000] TIMING: {"kind":"run","phase":"start","run":1}',
    '[1001] TIMING: {"kind":"stage","stage":"wrapper-overhead","phase":"start","run":1}',
    '[1010] TIMING: {"kind":"stage","stage":"config-parse","phase":"start","run":1}',
    '[1040] TIMING: {"kind":"stage","stage":"config-parse","phase":"finish","duration_ms":30,"run":1}',
    '[1050] TIMING: {"kind":"stage","stage":"host-selection","phase":"start","run":1}',
    '[1130] TIMING: {"kind":"stage","stage":"host-selection","phase":"finish","duration_ms":80,"run":1}',
    '[1140] TIMING: {"kind":"stage","stage":"credential-vault","phase":"start","run":1}',
    '[1180] TIMING: {"kind":"stage","stage":"credential-vault","phase":"finish","duration_ms":40,"run":1}',
    '[1190] TIMING: {"kind":"stage","stage":"connection-orchestration","phase":"start","run":1}',
    '[1210] TIMING: {"kind":"stage","stage":"connection-orchestration","phase":"finish","duration_ms":20,"run":1}',
    '[1211] TIMING: {"kind":"stage","stage":"wrapper-overhead","phase":"finish","duration_ms":110,"run":1}',
    '[1212] TIMING: {"kind":"stage","stage":"wrapper-teardown","phase":"start","run":1}',
    '[1215] TIMING: {"kind":"stage","stage":"ssh-connection","phase":"start","run":1}',
    '[1355] TIMING: {"kind":"stage","stage":"ssh-connection","phase":"finish","duration_ms":140,"run":1}',
    '[1358] TIMING: {"kind":"stage","stage":"wrapper-teardown","phase":"finish","duration_ms":3,"run":1}',
    '[1230] TIMING: {"kind":"run","phase":"finish","run":1,"duration_ms":230,"status":"ok"}',
    '[2000] TIMING: {"kind":"run","phase":"start","run":2}',
    '[2001] TIMING: {"kind":"stage","stage":"wrapper-overhead","phase":"start","run":2}',
    '[2010] TIMING: {"kind":"stage","stage":"config-parse","phase":"start","run":2}',
    '[2050] TIMING: {"kind":"stage","stage":"config-parse","phase":"finish","duration_ms":40,"run":2}',
    '[2060] TIMING: {"kind":"stage","stage":"host-selection","phase":"start","run":2}',
    '[2160] TIMING: {"kind":"stage","stage":"host-selection","phase":"finish","duration_ms":100,"run":2}',
    '[2170] TIMING: {"kind":"stage","stage":"credential-vault","phase":"start","run":2}',
    '[2230] TIMING: {"kind":"stage","stage":"credential-vault","phase":"finish","duration_ms":60,"run":2}',
    '[2240] TIMING: {"kind":"stage","stage":"connection-orchestration","phase":"start","run":2}',
    '[2290] TIMING: {"kind":"stage","stage":"connection-orchestration","phase":"finish","duration_ms":50,"run":2}',
    '[2291] TIMING: {"kind":"stage","stage":"wrapper-overhead","phase":"finish","duration_ms":120,"run":2}',
    '[2292] TIMING: {"kind":"stage","stage":"wrapper-teardown","phase":"start","run":2}',
    '[2295] TIMING: {"kind":"stage","stage":"ssh-connection","phase":"start","run":2}',
    '[2455] TIMING: {"kind":"stage","stage":"ssh-connection","phase":"finish","duration_ms":160,"run":2}',
    '[2470] TIMING: {"kind":"stage","stage":"wrapper-teardown","phase":"finish","duration_ms":15,"run":2}',
    '[2320] TIMING: {"kind":"run","phase":"finish","run":2,"duration_ms":320,"status":"ok"}',
]

STRUCTURED_WITH_DETAIL = [
    '[1300] TIMING: {"kind":"stage","stage":"config-parse","phase":"finish","run":9,"duration_ms":45,"detail":"~/.ssh/config"}',
    '[1310] TIMING: {"kind":"run","phase":"finish","run":9,"duration_ms":120,"status":"error","detail":"timeout"}',
]

UNORDERED_STRUCTURED_LINES = [
    '[200] TIMING: {"kind":"stage","stage":"beta","phase":"finish","run":1,"duration_ms":5}',
    '[180] TIMING: {"kind":"stage","stage":"alpha","phase":"finish","run":1,"duration_ms":3}',
    '[210] TIMING: {"kind":"run","phase":"finish","run":1,"duration_ms":9,"status":"error"}',
]


def test_parse_timing_lines_extracts_entries() -> None:
    entries = timing.parse_timing_lines(SAMPLE_LINES + ["noise line"])
    assert len(entries) == 3
    assert entries[0].timestamp_ms == 100
    assert entries[1].message == "END: connect module"


def test_build_summary_computes_elapsed_and_delta() -> None:
    summary = timing.build_summary(timing.parse_timing_lines(SAMPLE_LINES))
    assert summary.total_ms == 145
    assert summary.events[0].elapsed_ms == 0
    assert summary.events[1].delta_ms == 120
    assert summary.events[2].elapsed_ms == 145


def test_archive_summary_writes_files(tmp_path) -> None:
    entries = timing.parse_timing_lines(SAMPLE_LINES)
    summary = timing.build_summary(entries)

    def clock() -> datetime:
        return datetime(2025, 1, 15, 9, 30, 0)

    raw_path, summary_path = timing.archive_summary(
        tmp_path,
        "baseline",
        SAMPLE_LINES,
        summary,
        clock=clock,
    )

    assert raw_path.read_text().strip().splitlines()[0] == SAMPLE_LINES[0]

    payload = json.loads(summary_path.read_text())
    assert payload["label"] == "baseline"
    assert payload["total_ms"] == summary.total_ms
    assert payload["events"][1]["delta_ms"] == 120


def test_parse_timing_lines_preserves_payload() -> None:
    entries = timing.parse_timing_lines(STRUCTURED_LINES[:2])
    assert entries[0].payload is not None
    assert entries[1].payload is not None
    assert entries[0].payload["kind"] == "run"
    assert entries[1].payload["stage"] == "wrapper-overhead"


def test_parse_timing_lines_retains_detail_and_status_fields() -> None:
    entries = timing.parse_timing_lines(STRUCTURED_WITH_DETAIL)
    assert entries[0].payload is not None
    assert entries[1].payload is not None
    stage_payload = entries[0].payload
    run_payload = entries[1].payload
    assert stage_payload["detail"] == "~/.ssh/config"
    assert stage_payload["duration_ms"] == 45
    assert run_payload["status"] == "error"
    assert run_payload["detail"] == "timeout"


def test_build_benchmark_samples_extracts_stage_data() -> None:
    entries = timing.parse_timing_lines(STRUCTURED_LINES)
    samples = timing.build_benchmark_samples(entries)
    assert len(samples) == 2
    assert len(samples[0].stages) == 7
    assert samples[0].total_ms == 230
    assert samples[1].stages[1].stage == "host-selection"
    assert samples[0].stages[-2].stage == "ssh-connection"
    assert samples[0].stages[-1].stage == "wrapper-teardown"


def test_summarize_benchmark_reports_variance() -> None:
    entries = timing.parse_timing_lines(STRUCTURED_LINES)
    samples = timing.build_benchmark_samples(entries)
    summary = timing.summarize_benchmark(samples)

    host_stats = summary.stage_stats["host-selection"]
    assert host_stats.mean_ms == pytest.approx((80 + 100) / 2)
    assert host_stats.stddev_ms > 0
    assert summary.total_stats.max_ms == 320
    ssh_stats = summary.stage_stats["ssh-connection"]
    assert ssh_stats.mean_ms == pytest.approx((140 + 160) / 2)
    wrapper_stats = summary.stage_stats["wrapper-overhead"]
    assert wrapper_stats.median_ms == pytest.approx((110 + 120) / 2)
    teardown_stats = summary.stage_stats["wrapper-teardown"]
    assert teardown_stats.mean_ms == pytest.approx((3 + 15) / 2)


def test_enforce_budgets_detects_violation() -> None:
    entries = timing.parse_timing_lines(STRUCTURED_LINES)
    samples = timing.build_benchmark_samples(entries)
    summary = timing.summarize_benchmark(samples)

    with pytest.raises(timing.TimingBudgetError):
        timing.enforce_budgets(summary, stage_budgets={"credential-vault": 10})

    timing.enforce_budgets(summary, stage_budgets={"credential-vault": 100})


def test_build_benchmark_samples_orders_stages_and_tracks_status() -> None:
    entries = timing.parse_timing_lines(UNORDERED_STRUCTURED_LINES)
    samples = timing.build_benchmark_samples(entries)
    assert len(samples) == 1
    sample = samples[0]
    assert sample.status == "error"
    assert [stage.stage for stage in sample.stages] == ["alpha", "beta"]
    assert sample.total_ms == 9
