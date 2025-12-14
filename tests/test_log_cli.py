from __future__ import annotations

import json
import os
from datetime import datetime, timedelta, timezone
from pathlib import Path

from click.testing import CliRunner

from nssh.cli.log import app, common
from nssh.core.recording import manager as recording


def _make_session(
    tmp_path: Path, host: str, date: str, session: str = "session-000"
) -> Path:
    host_dir = tmp_path / host / date
    host_dir.mkdir(parents=True, exist_ok=True)
    cast_path = host_dir / f"{session}.cast"

    # Create valid asciinema v3 file
    # Parse date to get Unix timestamp (approximation for 10:00:00 UTC)
    import datetime

    dt = datetime.datetime.fromisoformat(f"{date}T10:00:00+00:00")
    timestamp = int(dt.timestamp())

    header = {
        "version": 3,
        "timestamp": timestamp,
        "title": f"nssh:{host}",
        "command": f"ssh {host}",
        "term": {"cols": 80, "rows": 24, "type": "xterm-256color"},
        "env": {"SHELL": "/bin/bash"},
    }

    # Create file with header and a few events (5 minutes = 300 seconds)
    lines = [
        json.dumps(header),
        '[1.0, "o", "output\\r\\n"]',
        '[299.0, "o", "exit\\r\\n"]',
        '[300.0, "x", "0"]',
    ]
    cast_path.write_text("\n".join(lines) + "\n")
    return cast_path


def test_play_dry_run_uses_asciinema(tmp_path, monkeypatch):
    base = tmp_path / "casts"
    cast_path = _make_session(base, "lab-sw1", "2025-11-14")
    monkeypatch.setenv("NSSH_RECORD_DIR", str(base))

    bin_dir = tmp_path / "bin"
    bin_dir.mkdir()
    asciinema = bin_dir / "asciinema"
    asciinema.write_text("#!/bin/sh\nexit 0\n")
    asciinema.chmod(0o755)

    env = os.environ.copy()
    env["PATH"] = f"{bin_dir}:{env['PATH']}"

    # Mock fzf picker to return the cast path
    monkeypatch.setattr(common, "resolve_recording_path", lambda s: cast_path)

    runner = CliRunner()
    result = runner.invoke(
        app,
        ["play", "--dry-run"],
        env=env,
    )
    assert result.exit_code == 0
    assert "asciinema play" in result.stdout


def test_play_selects_session(tmp_path, monkeypatch):
    """Test play command selecting specific session via fzf."""
    base = tmp_path / "casts"
    _make_session(base, "lab-sw1", "2025-11-14", session="session-000")
    session_001 = _make_session(base, "lab-sw1", "2025-11-14", session="session-001")
    monkeypatch.setenv("NSSH_RECORD_DIR", str(base))

    bin_dir = tmp_path / "bin"
    bin_dir.mkdir()
    asciinema = bin_dir / "asciinema"
    asciinema.write_text("#!/bin/sh\nexit 0\n")
    asciinema.chmod(0o755)

    env = os.environ.copy()
    env["PATH"] = f"{bin_dir}:{env['PATH']}"

    # Mock fzf picker to return session-001
    monkeypatch.setattr(common, "resolve_recording_path", lambda s: session_001)

    runner = CliRunner()
    result = runner.invoke(
        app,
        ["play", "--dry-run"],
        env=env,
    )
    assert result.exit_code == 0
    assert "session-001.cast" in result.stdout


def test_delete_older_than_dry_run(tmp_path, monkeypatch):
    """Test delete --older-than in dry-run mode."""
    base = tmp_path / "casts"
    base.mkdir()

    # Create old recording
    host_dir = base / "lab-sw1" / "2025-11-13"
    host_dir.mkdir(parents=True)
    old_cast = host_dir / "session-000.cast"
    old_cast.write_text("old recording")

    # Make it old by setting mtime
    past_time = datetime.now(timezone.utc) - timedelta(days=3)
    ts = past_time.timestamp()
    os.utime(old_cast, (ts, ts))

    monkeypatch.setenv("NSSH_RECORD_DIR", str(base))

    runner = CliRunner()
    result = runner.invoke(app, ["delete", "--older-than", "1", "--dry-run"])

    assert result.exit_code == 0
    assert "DRY RUN" in result.stdout
    assert old_cast.exists()  # Should not be deleted in dry-run


def test_delete_older_than_removes_old_recordings(tmp_path, monkeypatch):
    """Test delete --older-than actually removes old recordings."""
    base = tmp_path / "casts"
    base.mkdir()

    # Create old and new recordings
    host_dir = base / "lab-sw1"
    old_dir = host_dir / "2025-11-13"
    new_dir = host_dir / "2025-11-14"
    old_dir.mkdir(parents=True)
    new_dir.mkdir(parents=True)

    old_cast = old_dir / "session-000.cast"
    old_cast.write_text("old recording")
    old_index = old_dir / "session-000.index.json"
    old_index.write_text('{"sessions": []}')

    new_cast = new_dir / "session-000.cast"
    new_cast.write_text("new recording")

    # Make old files old
    past_time = datetime.now(timezone.utc) - timedelta(days=3)
    ts = past_time.timestamp()
    os.utime(old_cast, (ts, ts))
    os.utime(old_index, (ts, ts))

    monkeypatch.setenv("NSSH_RECORD_DIR", str(base))

    runner = CliRunner()
    result = runner.invoke(app, ["delete", "--older-than", "1"])

    assert result.exit_code == 0
    assert "DELETED" in result.stdout
    assert not old_cast.exists()
    assert not old_index.exists()
    assert new_cast.exists()


def test_delete_select_pattern_dry_run(tmp_path, monkeypatch):
    """Test delete --select with pattern in dry-run mode."""
    base = tmp_path / "casts"
    cast1 = _make_session(base, "lab-sw1", "2025-11-14")
    cast2 = _make_session(base, "lab-sw2", "2025-11-14")
    _make_session(base, "prod-sw1", "2025-11-14")

    monkeypatch.setenv("NSSH_RECORD_DIR", str(base))

    runner = CliRunner()
    result = runner.invoke(app, ["delete", "--select", "lab", "--dry-run", "-y"])

    assert result.exit_code == 0
    assert "Found 2 recording(s)" in result.stdout
    assert "Would delete" in result.stdout
    # Files should still exist in dry-run
    assert cast1.exists()
    assert cast2.exists()


def test_delete_select_pattern_deletes_matching(tmp_path, monkeypatch):
    """Test delete --select actually deletes matching recordings."""
    base = tmp_path / "casts"
    cast1 = _make_session(base, "lab-sw1", "2025-11-14")
    cast2 = _make_session(base, "lab-sw2", "2025-11-14")
    prod_cast = _make_session(base, "prod-sw1", "2025-11-14")

    monkeypatch.setenv("NSSH_RECORD_DIR", str(base))

    runner = CliRunner()
    result = runner.invoke(app, ["delete", "--select", "lab", "-y"])

    assert result.exit_code == 0
    assert "Found 2 recording(s)" in result.stdout
    assert not cast1.exists()
    assert not cast2.exists()
    assert prod_cast.exists()


def test_delete_select_no_matches(tmp_path, monkeypatch):
    """Test delete --select with no matching recordings."""
    base = tmp_path / "casts"
    _make_session(base, "prod-sw1", "2025-11-14")

    monkeypatch.setenv("NSSH_RECORD_DIR", str(base))

    runner = CliRunner()
    result = runner.invoke(app, ["delete", "--select", "nonexistent"])

    assert result.exit_code == 1
    assert "No recordings match" in result.stdout


def test_delete_multi_select_deletes_selected(tmp_path, monkeypatch):
    """Test delete with fzf multi-select."""
    base = tmp_path / "casts"
    cast1 = _make_session(base, "lab-sw1", "2025-11-14")
    cast2 = _make_session(base, "lab-sw2", "2025-11-14")

    monkeypatch.setenv("NSSH_RECORD_DIR", str(base))

    # Mock multi-select to return both paths
    monkeypatch.setattr(
        common, "resolve_recording_paths_multi", lambda s: [cast1, cast2]
    )

    runner = CliRunner()
    result = runner.invoke(app, ["delete", "-y"])

    assert result.exit_code == 0
    assert not cast1.exists()
    assert not cast2.exists()


def test_export_defaults_to_current_dir_txt(tmp_path, monkeypatch):
    """Test export command with -y uses default .txt output."""
    base = tmp_path / "casts"
    cast_path = _make_session(base, "lab-sw1", "2025-11-14")
    monkeypatch.setenv("NSSH_RECORD_DIR", str(base))

    # Set up working directory
    work_dir = tmp_path / "work"
    work_dir.mkdir()
    monkeypatch.chdir(work_dir)

    # Set up asciinema binary
    bin_dir = tmp_path / "bin"
    bin_dir.mkdir()
    asciinema = bin_dir / "asciinema"
    asciinema.write_text('#!/bin/sh\ntouch "$3"\nexit 0\n')
    asciinema.chmod(0o755)

    env = os.environ.copy()
    env["PATH"] = f"{bin_dir}:{env['PATH']}"

    # Mock fzf picker to return the cast path
    monkeypatch.setattr(common, "resolve_recording_path", lambda s: cast_path)

    runner = CliRunner()
    result = runner.invoke(
        app,
        ["export", "-y", "--dry-run"],
        env=env,
    )
    assert result.exit_code == 0
    # Should output to lab-sw1_2025-11-14_session-000.txt in current dir
    assert "lab-sw1_2025-11-14_session-000.txt" in result.stdout


def test_export_gif_format_via_prompt(tmp_path, monkeypatch):
    """Test export command with .gif extension uses asciicast2gif."""
    base = tmp_path / "casts"
    cast_path = _make_session(base, "lab-sw1", "2025-11-14")
    monkeypatch.setenv("NSSH_RECORD_DIR", str(base))

    # Set up working directory
    work_dir = tmp_path / "work"
    work_dir.mkdir()
    monkeypatch.chdir(work_dir)

    # Set up asciicast2gif binary
    bin_dir = tmp_path / "bin"
    bin_dir.mkdir()
    asciicast2gif = bin_dir / "asciicast2gif"
    asciicast2gif.write_text('#!/bin/sh\ntouch "$2"\nexit 0\n')
    asciicast2gif.chmod(0o755)

    env = os.environ.copy()
    env["PATH"] = f"{bin_dir}:{env['PATH']}"

    # Mock fzf picker to return the cast path
    monkeypatch.setattr(common, "resolve_recording_path", lambda s: cast_path)

    gif_output = work_dir / "output.gif"

    runner = CliRunner()
    # Provide .gif path via prompt input
    result = runner.invoke(
        app,
        ["export", "--dry-run"],
        input=f"{gif_output}\n",
        env=env,
    )
    assert result.exit_code == 0
    # Should use asciicast2gif since extension is .gif
    assert "asciicast2gif" in result.stdout


def test_load_sessions_sorts_by_cast_mtime(tmp_path, monkeypatch):
    newer_cast = tmp_path / "host" / "2025-11-16" / "session-000.cast"
    newer_cast.parent.mkdir(parents=True)
    older_cast = tmp_path / "host" / "2025-11-15" / "session-000.cast"
    older_cast.parent.mkdir(parents=True)

    newer_cast.write_text("new")
    older_cast.write_text("old")

    older_ts = datetime(2025, 11, 15, 12, tzinfo=timezone.utc).timestamp()
    newer_ts = datetime(2025, 11, 16, 12, tzinfo=timezone.utc).timestamp()
    os.utime(older_cast, (older_ts, older_ts))
    os.utime(newer_cast, (newer_ts, newer_ts))

    sessions = [
        recording.SessionRecord(
            host="older",
            cast_path=older_cast,
            started_at=datetime(2025, 11, 15, tzinfo=timezone.utc),
            finished_at=datetime(2025, 11, 15, 12, tzinfo=timezone.utc),
            argv=("ssh", "older"),
            session_label="session-000",
        ),
        recording.SessionRecord(
            host="newer",
            cast_path=newer_cast,
            started_at=datetime(2025, 11, 16, tzinfo=timezone.utc),
            finished_at=datetime(2025, 11, 16, 12, tzinfo=timezone.utc),
            argv=("ssh", "newer"),
            session_label="session-000",
        ),
    ]

    monkeypatch.setattr(
        common.recording,
        "iter_session_records",
        lambda *, settings=None: iter(sessions),
    )

    ordered = common.load_sessions()
    assert [entry.host for entry in ordered] == ["newer", "older"]


def test_session_updated_timestamp_falls_back_when_cast_missing(tmp_path):
    missing_cast = tmp_path / "host" / "2025-11-16" / "session-000.cast"
    entry = recording.SessionRecord(
        host="missing",
        cast_path=missing_cast,
        started_at=datetime(2025, 11, 16, tzinfo=timezone.utc),
        finished_at=datetime(2025, 11, 16, 12, tzinfo=timezone.utc),
        argv=("ssh", "missing"),
        session_label="session-000",
    )

    timestamp = common._session_updated_timestamp(entry)
    assert timestamp == entry.finished_at.timestamp()


def test_session_duration_seconds_sums_index_entries(tmp_path):
    cast_path = tmp_path / "host" / "2025-11-16" / "session-000.cast"
    cast_path.parent.mkdir(parents=True)
    cast_path.write_text("{}\n")

    index_path = cast_path.with_suffix(".index.json")
    payload = {
        "sessions": [
            {
                "started_at": "2025-11-16T01:00:00+00:00",
                "finished_at": "2025-11-16T01:05:00+00:00",
            },
            {
                "started_at": "2025-11-16T02:00:00+00:00",
                "finished_at": "2025-11-16T02:04:00+00:00",
            },
        ]
    }
    index_path.write_text(json.dumps(payload))

    entry = recording.SessionRecord(
        host="host",
        cast_path=cast_path,
        started_at=datetime(2025, 11, 16, tzinfo=timezone.utc),
        finished_at=datetime(2025, 11, 16, 12, tzinfo=timezone.utc),
        argv=("ssh", "host"),
        session_label="session-000",
    )

    assert common._session_duration_seconds(entry) == 9 * 60


def test_session_duration_seconds_falls_back_without_index(tmp_path):
    cast_path = tmp_path / "host" / "2025-11-16" / "session-000.cast"
    cast_path.parent.mkdir(parents=True)
    cast_path.write_text("{}")

    entry = recording.SessionRecord(
        host="host",
        cast_path=cast_path,
        started_at=datetime(2025, 11, 16, 0, 0, tzinfo=timezone.utc),
        finished_at=datetime(2025, 11, 16, 0, 10, tzinfo=timezone.utc),
        argv=("ssh", "host"),
        session_label="session-000",
    )

    assert common._session_duration_seconds(entry) == 600
