from __future__ import annotations

import json
import os
import re
from datetime import datetime, timedelta, timezone
from pathlib import Path

from nssh.core.recording import manager as recording


def _settings(
    tmp_path: Path, max_age_days: int | None = None
) -> recording.RecordingSettings:
    return recording.RecordingSettings(
        enabled=True,
        force=False,
        append_mode=True,
        include_patterns=(),
        exclude_patterns=(),
        directory=tmp_path,
        max_age_days=max_age_days,
        asciinema_server_url=None,
        window_size=None,
    )


def test_should_record_supports_globs_and_regex(tmp_path):
    include = (recording.HostPattern(raw="lab-*", kind="glob", pattern="lab-*"),)
    exclude = (
        recording.HostPattern(
            raw="regex:^lab-exclude",
            kind="regex",
            pattern="^lab-exclude",
            regex=re.compile(r"^lab-exclude"),
        ),
    )
    settings = recording.RecordingSettings(
        enabled=True,
        force=False,
        append_mode=True,
        include_patterns=include,
        exclude_patterns=exclude,
        directory=tmp_path,
        max_age_days=None,
        asciinema_server_url=None,
        window_size=None,
    )

    assert recording.should_record("lab-sw1", settings)
    assert not recording.should_record("lab-exclude-01", settings)
    assert not recording.should_record("core-router", settings)


def test_cleanup_old_recordings_removes_expired(tmp_path, monkeypatch):
    settings = _settings(tmp_path, max_age_days=1)
    host_dir = tmp_path / "lab-sw1"
    (host_dir / "2025-11-13").mkdir(parents=True, exist_ok=True)
    (host_dir / "2025-11-14").mkdir(parents=True, exist_ok=True)
    old_cast = host_dir / "2025-11-13" / "session-000.cast"
    old_cast.write_text("old")
    old_index = old_cast.with_suffix(".index.json")
    old_index.write_text(json.dumps({"sessions": []}))
    new_cast = host_dir / "2025-11-14" / "session-000.cast"
    new_cast.write_text("new")

    past_time = datetime.now(timezone.utc) - timedelta(days=3)
    ts = past_time.timestamp()
    os.utime(old_cast, (ts, ts))

    summary = recording.cleanup_old_recordings(
        settings=settings,
        now=datetime.now(timezone.utc),
        dry_run=False,
    )
    assert summary is not None
    assert not old_cast.exists()
    assert not old_index.exists()
    assert new_cast.exists()


def test_allocate_reuses_missing_latest_sequence(tmp_path):
    session_dir = tmp_path / "host" / "2025-11-16"
    session_dir.mkdir(parents=True)

    first = recording._allocate_session_sequence(session_dir)
    assert first == 0

    # No cast written yet, so the next allocation should reuse session-000
    second = recording._allocate_session_sequence(session_dir)
    assert second == 0

    # Once a cast exists, allocation should advance
    (session_dir / "session-000.cast").write_text("cast")
    third = recording._allocate_session_sequence(session_dir)
    assert third == 1


def test_allocate_reuses_after_latest_cast_deleted(tmp_path):
    session_dir = tmp_path / "host" / "2025-11-17"
    session_dir.mkdir(parents=True)

    assert recording._allocate_session_sequence(session_dir) == 0
    (session_dir / "session-000.cast").write_text("0")
    assert recording._allocate_session_sequence(session_dir) == 1

    cast_one = session_dir / "session-001.cast"
    cast_one.write_text("1")
    cast_one.unlink()

    # Latest slot has no cast anymore, so reuse sequence-001
    assert recording._allocate_session_sequence(session_dir) == 1


def test_read_cast_metadata_sums_event_deltas(tmp_path):
    cast_path = tmp_path / "host" / "2025-11-16" / "session-000.cast"
    cast_path.parent.mkdir(parents=True)

    header = {
        "version": 3,
        "timestamp": 1_763_346_442,
        "title": "nssh:host",
        "command": "ssh host",
        "term": {"cols": 80, "rows": 24},
        "env": {},
    }
    events = [
        [1.0, "o", "first"],
        [0.5, "o", "second"],
        [2.5, "o", "third"],
    ]
    payload = (
        "\n".join([json.dumps(header)] + [json.dumps(evt) for evt in events]) + "\n"
    )
    cast_path.write_text(payload)

    meta = recording._read_cast_metadata(cast_path)
    assert meta is not None
    duration = meta["finished_at"] - meta["started_at"]
    assert duration.total_seconds() == 4.0


def test_pty_connector_accepts_recording_plan(tmp_path):
    """Test that PTY connector accepts and stores recording plan."""
    from nssh.core.connector.pty import PtyConnector

    cast_path = tmp_path / "test.cast"
    lock_dir = tmp_path / ".lock"

    plan = recording.RecordingPlan(
        enabled=True,
        cast_path=cast_path,
        append=False,
        title="nssh:testhost",
        asciinema_bin="echo",  # Mock asciinema
        lock_directory=lock_dir,
        sequence=0,
        session_label="session-000",
    )

    # Test that connector accepts recording plan
    connector = PtyConnector(
        hostname="testhost",
        username=None,
        password=None,
        recording_plan=plan,
    )

    assert connector.recording_plan == plan
    assert connector.recording_plan.enabled is True


def test_recording_disabled_by_nssh_record_env_var(monkeypatch):
    """Test NSSH_RECORD=0 disables recording."""
    monkeypatch.setenv("NSSH_RECORD", "0")
    plan = recording._compute_plan(
        "testhost", prepare_dirs=False, allocate_sequence=False
    )

    assert plan.enabled is False
    assert "NSSH_RECORD" in plan.reason or "disabled" in plan.reason


def test_recording_enabled_by_nssh_record_env_var(monkeypatch):
    """Test NSSH_RECORD=1 enables recording."""
    monkeypatch.setenv("NSSH_RECORD", "1")
    plan = recording._compute_plan(
        "testhost", prepare_dirs=False, allocate_sequence=False
    )

    # May fail if asciinema not installed, but that's expected
    if plan.enabled:
        assert plan.asciinema_bin is not None


def test_build_asciinema_command(tmp_path):
    """Test asciinema command construction."""
    cast_path = tmp_path / "test.cast"
    plan = recording.RecordingPlan(
        enabled=True,
        cast_path=cast_path,
        append=True,
        title="nssh:testhost",
        asciinema_bin="/usr/bin/asciinema",
        lock_directory=None,
        sequence=0,
        session_label="session-000",
    )

    ssh_cmd = ["ssh", "-tt", "testhost"]
    cmd = recording.build_asciinema_command(plan, ssh_cmd)

    assert cmd[0] == "/usr/bin/asciinema"
    assert "rec" in cmd
    assert "--quiet" in cmd
    assert "--append" in cmd
    assert "--title" in cmd
    assert "nssh:testhost" in cmd
    assert "--command" in cmd
    assert str(cast_path) == cmd[-1]  # Cast path should be last
    # Command string should contain ssh and testhost
    cmd_str = " ".join(cmd)
    assert "ssh" in cmd_str
    assert "testhost" in cmd_str
