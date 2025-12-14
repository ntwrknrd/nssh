from __future__ import annotations

from datetime import datetime, timedelta
from pathlib import Path

import pytest

from nssh.cli.log import common
from nssh.core.recording import manager as recording


def _make_session(
    tmp_path: Path, host: str, date_str: str, minutes: int
) -> recording.SessionRecord:
    started = datetime.fromisoformat(f"{date_str}T12:00:00+00:00")
    finished = started + timedelta(minutes=minutes)
    cast_path = tmp_path / host / date_str / "session-001.cast"
    cast_path.parent.mkdir(parents=True, exist_ok=True)
    cast_path.write_text("{}", encoding="utf-8")
    return recording.SessionRecord(
        host=host,
        cast_path=cast_path,
        started_at=started,
        finished_at=finished,
        argv=("ssh", host),
        session_label=None,
    )


def _make_settings(tmp_path: Path) -> recording.RecordingSettings:
    return recording.RecordingSettings(
        enabled=True,
        force=False,
        append_mode=True,
        include_patterns=(),
        exclude_patterns=(),
        directory=tmp_path,
        asciinema_server_url=None,
        window_size=None,
    )


def test_resolve_recording_path_shows_all_sessions(monkeypatch, tmp_path):
    session = _make_session(tmp_path, "alpha", "2025-11-18", 12)
    settings = _make_settings(tmp_path)

    monkeypatch.setattr(common, "load_sessions", lambda _: [session])

    captured = {}

    def fake_select(options, prompt, *, multi=False, exit_on_cancel=True):
        captured["options"] = list(options)
        captured["prompt"] = prompt
        return [options[0]]

    monkeypatch.setattr(common, "fzf_select", fake_select)

    result = common.resolve_recording_path(settings)
    assert result == session.cast_path
    assert captured["prompt"] == "Select recording:"
    assert len(captured["options"]) == 1


def test_resolve_recording_path_exits_when_no_sessions(monkeypatch, tmp_path):
    settings = _make_settings(tmp_path)
    monkeypatch.setattr(common, "load_sessions", lambda _: [])

    with pytest.raises(SystemExit) as exc:
        common.resolve_recording_path(settings)

    assert exc.value.code == 1


def test_filter_sessions_by_host_exact_date(tmp_path):
    sessions = [
        _make_session(tmp_path, "alpha", "2025-11-18", 12),
        _make_session(tmp_path, "beta", "2025-11-18", 5),
        _make_session(tmp_path, "alpha", "2025-11-17", 10),
    ]

    result = common.filter_sessions_by_host(sessions, "alpha", "2025-11-18")
    assert len(result) == 1
    assert result[0].host == "alpha"
    assert result[0].started_at.strftime("%Y-%m-%d") == "2025-11-18"


def test_filter_sessions_by_host_wildcard_date(tmp_path):
    sessions = [
        _make_session(tmp_path, "alpha", "2025-11-18", 12),
        _make_session(tmp_path, "beta", "2025-11-18", 5),
        _make_session(tmp_path, "alpha", "2025-11-17", 10),
    ]

    result = common.filter_sessions_by_host(sessions, "alpha", "*")
    assert len(result) == 2
    assert all(s.host == "alpha" for s in result)


def test_filter_sessions_by_host_case_insensitive(tmp_path):
    sessions = [
        _make_session(tmp_path, "Alpha", "2025-11-18", 12),
    ]

    result = common.filter_sessions_by_host(sessions, "ALPHA", "*")
    assert len(result) == 1


def test_filter_sessions_by_host_substring_match(tmp_path):
    sessions = [
        _make_session(tmp_path, "prod-server-01", "2025-11-18", 12),
        _make_session(tmp_path, "dev-server-01", "2025-11-18", 5),
    ]

    result = common.filter_sessions_by_host(sessions, "prod", "*")
    assert len(result) == 1
    assert result[0].host == "prod-server-01"


def test_delete_recording_removes_cast_and_index(tmp_path):
    cast_path = tmp_path / "host" / "2025-11-18" / "session-001.cast"
    cast_path.parent.mkdir(parents=True, exist_ok=True)
    cast_path.write_text("{}", encoding="utf-8")

    index_path = cast_path.with_suffix(".index.json")
    index_path.write_text("{}", encoding="utf-8")

    class FakeConsole:
        def __init__(self):
            self.messages = []

        def print(self, msg):
            self.messages.append(msg)

    console = FakeConsole()
    common.delete_recording(cast_path, tmp_path, dry_run=False, console=console)

    assert not cast_path.exists()
    assert not index_path.exists()
    assert any("Deleted" in m for m in console.messages)


def test_delete_recording_dry_run_does_not_delete(tmp_path):
    cast_path = tmp_path / "host" / "2025-11-18" / "session-001.cast"
    cast_path.parent.mkdir(parents=True, exist_ok=True)
    cast_path.write_text("{}", encoding="utf-8")

    class FakeConsole:
        def __init__(self):
            self.messages = []

        def print(self, msg):
            self.messages.append(msg)

    console = FakeConsole()
    common.delete_recording(cast_path, tmp_path, dry_run=True, console=console)

    assert cast_path.exists()
    assert any("Would delete" in m for m in console.messages)


def test_delete_recording_cleans_empty_directories(tmp_path):
    cast_path = tmp_path / "host" / "2025-11-18" / "session-001.cast"
    cast_path.parent.mkdir(parents=True, exist_ok=True)
    cast_path.write_text("{}", encoding="utf-8")

    class FakeConsole:
        def print(self, msg):
            pass

    common.delete_recording(cast_path, tmp_path, dry_run=False, console=FakeConsole())

    assert not cast_path.parent.exists()  # date dir removed
    assert not cast_path.parent.parent.exists()  # host dir removed


def test_select_sessions_by_pattern_matches_host(tmp_path):
    sessions = [
        _make_session(tmp_path, "lab-sw1", "2025-11-18", 12),
        _make_session(tmp_path, "prod-sw1", "2025-11-18", 5),
        _make_session(tmp_path, "lab-sw2", "2025-11-17", 10),
    ]

    result = common.select_sessions_by_pattern(sessions, "lab")
    assert len(result) == 2
    assert all("lab" in s.host for s in result)


def test_select_sessions_by_pattern_matches_date(tmp_path):
    sessions = [
        _make_session(tmp_path, "lab-sw1", "2025-11-18", 12),
        _make_session(tmp_path, "prod-sw1", "2025-11-18", 5),
        _make_session(tmp_path, "lab-sw2", "2025-11-17", 10),
    ]

    result = common.select_sessions_by_pattern(sessions, "2025-11-18")
    assert len(result) == 2
    assert all(s.started_at.strftime("%Y-%m-%d") == "2025-11-18" for s in result)


def test_select_sessions_by_pattern_case_insensitive(tmp_path):
    sessions = [
        _make_session(tmp_path, "LAB-SW1", "2025-11-18", 12),
    ]

    result = common.select_sessions_by_pattern(sessions, "lab-sw1")
    assert len(result) == 1


def test_select_sessions_by_pattern_regex(tmp_path):
    sessions = [
        _make_session(tmp_path, "lab-sw1", "2025-11-18", 12),
        _make_session(tmp_path, "lab-sw2", "2025-11-18", 5),
        _make_session(tmp_path, "prod-sw1", "2025-11-17", 10),
    ]

    result = common.select_sessions_by_pattern(sessions, r"lab-sw[12]")
    assert len(result) == 2


def test_resolve_recording_paths_multi_returns_multiple(monkeypatch, tmp_path):
    session1 = _make_session(tmp_path, "alpha", "2025-11-18", 12)
    session2 = _make_session(tmp_path, "beta", "2025-11-18", 5)
    settings = _make_settings(tmp_path)

    monkeypatch.setattr(common, "load_sessions", lambda _: [session1, session2])

    # Mock fzf_select to return both options
    def fake_fzf_select(options, prompt, *, multi=False, exit_on_cancel=True):
        return list(options)  # Return all options

    monkeypatch.setattr(common, "fzf_select", fake_fzf_select)

    result = common.resolve_recording_paths_multi(settings)
    assert len(result) == 2
    assert session1.cast_path in result
    assert session2.cast_path in result


def test_resolve_recording_paths_multi_exits_on_cancel(monkeypatch, tmp_path):
    session = _make_session(tmp_path, "alpha", "2025-11-18", 12)
    settings = _make_settings(tmp_path)

    monkeypatch.setattr(common, "load_sessions", lambda _: [session])

    # Mock fzf_select to raise FzfCancelled (simulating cancel)
    def fake_fzf_select(options, prompt, *, multi=False, exit_on_cancel=True):
        from nssh.cli.common.selectors import FzfCancelled

        raise FzfCancelled()

    monkeypatch.setattr(common, "fzf_select", fake_fzf_select)

    with pytest.raises(SystemExit) as exc:
        common.resolve_recording_paths_multi(settings)

    assert exc.value.code == 0
