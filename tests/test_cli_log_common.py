from __future__ import annotations

from datetime import datetime, timedelta
from pathlib import Path

import pytest
from typer import Exit

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
        max_age_days=None,
    )


def test_pick_recording_interactive_defaults_to_recent(monkeypatch, tmp_path):
    session = _make_session(tmp_path, "alpha", "2025-11-18", 12)
    settings = _make_settings(tmp_path)

    monkeypatch.setattr(common, "load_sessions", lambda _: [session])

    captured = {}

    def fake_select(options, prompt):
        captured["options"] = list(options)
        captured["prompt"] = prompt
        return options[0]

    monkeypatch.setattr(common, "select_via_fzf", fake_select)

    result = common.pick_recording_interactive(None, settings)
    assert result == session.cast_path
    assert captured["prompt"] == "Select recording:"
    assert len(captured["options"]) == 1


def test_pick_recording_interactive_filters_by_date(monkeypatch, tmp_path):
    sessions = [
        _make_session(tmp_path, "alpha", "2025-11-18", 12),
        _make_session(tmp_path, "beta", "2025-11-17", 5),
    ]
    settings = _make_settings(tmp_path)

    monkeypatch.setattr(common, "load_sessions", lambda _: sessions)

    captured = {}

    def fake_select(options, prompt):
        captured["options"] = list(options)
        captured["prompt"] = prompt
        return options[0]

    monkeypatch.setattr(common, "select_via_fzf", fake_select)

    result = common.pick_recording_interactive("2025-11-17", settings)
    assert result == sessions[1].cast_path
    assert captured["prompt"] == "Select recording (2025-11-17):"
    assert len(captured["options"]) == 1
    assert "beta" in captured["options"][0]


def test_pick_recording_interactive_exits_when_no_sessions(monkeypatch, tmp_path):
    settings = _make_settings(tmp_path)
    monkeypatch.setattr(common, "load_sessions", lambda _: [])

    with pytest.raises(Exit) as exc:
        common.pick_recording_interactive(None, settings)

    assert exc.value.exit_code == 1
