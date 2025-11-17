from __future__ import annotations

import io

import pytest

from nssh.core.diag import timing_server


def _set_time(
    monkeypatch: pytest.MonkeyPatch, perf_values: list[int], time_values: list[float]
) -> None:
    perf_iter = iter(perf_values)
    time_iter = iter(time_values)

    def fake_perf_counter() -> int:
        return next(perf_iter)

    def fake_time() -> float:
        return next(time_iter)

    monkeypatch.setattr(timing_server.time, "perf_counter_ns", fake_perf_counter)
    monkeypatch.setattr(timing_server.time, "time", fake_time)


def test_fallback_snapshot_without_start(monkeypatch: pytest.MonkeyPatch) -> None:
    _set_time(monkeypatch, [123456789], [100.0])
    assert timing_server.fallback_snapshot("") == "100000 123456789"


def test_fallback_snapshot_with_start(monkeypatch: pytest.MonkeyPatch) -> None:
    _set_time(monkeypatch, [123456789], [100.0])
    result = timing_server.fallback_snapshot("123456000")
    assert result == "100000 123456789 0.000789"


def test_process_stream_handles_start_and_end(monkeypatch: pytest.MonkeyPatch) -> None:
    _set_time(monkeypatch, [1000, 2000], [1.0, 1.5])
    buffer = io.StringIO()
    timing_server._process_stream(["START\n", "END 500\n", "STOP\n"], buffer)
    lines = buffer.getvalue().strip().splitlines()
    assert lines[0] == "1000 1000"
    assert lines[1] == "1500 2000 0.001500"


def test_snapshot_response_ignores_unknown_commands(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    _set_time(monkeypatch, [10], [0.01])
    assert timing_server._snapshot_response("FOO") == ""
