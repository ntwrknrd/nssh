from __future__ import annotations

import os
import tarfile
from datetime import datetime, timedelta, timezone
from pathlib import Path

from nssh.core.recording.archive import RecordingArchiveSettings, RecordingArchiver
from nssh.core.recording.manager import RecordingSettings


def _recording_settings(base_dir: Path) -> RecordingSettings:
    return RecordingSettings(
        enabled=True,
        force=False,
        append_mode=True,
        include_patterns=(),
        exclude_patterns=(),
        directory=base_dir,
        asciinema_server_url=None,
        window_size=None,
    )


def _touch(path: Path, *, content: str = "cast", mtime: datetime) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(content, encoding="utf-8")
    ts = mtime.timestamp()
    os.utime(path, (ts, ts))


def test_archives_old_casts_and_deletes_sources(tmp_path):
    now = datetime(2025, 1, 15, tzinfo=timezone.utc)
    base_dir = tmp_path / "casts"
    archive_dir = tmp_path / "archives"

    old_time = now - timedelta(days=40)
    recent_time = now - timedelta(days=5)

    old_cast = base_dir / "lab-sw1" / "2024-12-06" / "session-000.cast"
    old_index = old_cast.with_suffix(".index.json")
    _touch(old_cast, content="old", mtime=old_time)
    _touch(old_index, content="{}", mtime=old_time)

    recent_cast = base_dir / "lab-sw1" / "2025-01-10" / "session-001.cast"
    _touch(recent_cast, content="recent", mtime=recent_time)

    archiver = RecordingArchiver(
        archive_settings=RecordingArchiveSettings(
            enabled=True,
            archive_dir=archive_dir,
            min_age=timedelta(days=30),
            max_bundles=12,
            max_run_bytes=0,
            jitter=timedelta(minutes=1),
        ),
        recording_settings=_recording_settings(base_dir),
    )

    summary = archiver.run_once(now=now)

    assert summary.archived_files == 1
    assert summary.deleted_files == 1
    bundle = archive_dir / "recordings-2024-12.tar.gz"
    assert bundle.exists()

    with tarfile.open(bundle, "r:gz") as tar:
        names = {m.name for m in tar.getmembers()}
    assert "lab-sw1/2024-12-06/session-000.cast" in names

    assert not old_cast.exists()
    assert not old_index.exists()
    assert recent_cast.exists()


def test_retention_prunes_old_bundles(tmp_path):
    base_dir = tmp_path / "casts"
    archive_dir = tmp_path / "archives"
    base_dir.mkdir()
    archive_dir.mkdir()

    months = ["2024-10", "2024-11", "2024-12", "2025-01"]
    for month in months:
        (archive_dir / f"recordings-{month}.tar.gz").write_bytes(b"dummy")

    archiver = RecordingArchiver(
        archive_settings=RecordingArchiveSettings(
            enabled=True,
            archive_dir=archive_dir,
            min_age=timedelta(days=1),
            max_bundles=2,
            max_run_bytes=0,
            jitter=timedelta(minutes=1),
        ),
        recording_settings=_recording_settings(base_dir),
    )

    summary = archiver.run_once(now=datetime.now(timezone.utc))
    remaining = sorted(p.name for p in archive_dir.glob("recordings-*.tar.gz"))

    assert summary.bundles_pruned == 2
    assert remaining == ["recordings-2024-12.tar.gz", "recordings-2025-01.tar.gz"]


def test_idempotent_when_entries_already_archived(tmp_path):
    now = datetime(2025, 2, 1, tzinfo=timezone.utc)
    base_dir = tmp_path / "casts"
    archive_dir = tmp_path / "archives"

    old_time = datetime(2025, 1, 1, tzinfo=timezone.utc)
    cast_path = base_dir / "lab-sw1" / "2025-01-01" / "session-000.cast"
    _touch(cast_path, content="one", mtime=old_time)

    archiver = RecordingArchiver(
        archive_settings=RecordingArchiveSettings(
            enabled=True,
            archive_dir=archive_dir,
            min_age=timedelta(days=30),
            max_bundles=5,
            max_run_bytes=0,
            jitter=timedelta(minutes=1),
        ),
        recording_settings=_recording_settings(base_dir),
    )

    first = archiver.run_once(now=now)
    assert first.archived_files == 1
    assert not cast_path.exists()

    second = archiver.run_once(now=now + timedelta(minutes=10))
    assert second.archived_files == 0
    assert second.deleted_files == 0

    bundle = archive_dir / "recordings-2025-01.tar.gz"
    with tarfile.open(bundle, "r:gz") as tar:
        names = [m.name for m in tar.getmembers() if m.isfile()]
    assert names.count("lab-sw1/2025-01-01/session-000.cast") == 1


def test_failure_does_not_delete_sources(tmp_path):
    now = datetime(2025, 3, 1, tzinfo=timezone.utc)
    base_dir = tmp_path / "casts"
    archive_dir = tmp_path / "archives"

    old_time = now - timedelta(days=45)
    cast_path = base_dir / "lab-sw1" / "2025-01-10" / "session-000.cast"
    _touch(cast_path, content="data", mtime=old_time)

    archive_dir.mkdir(parents=True, exist_ok=True)
    archive_dir.chmod(0o400)  # read-only to force failure

    archiver = RecordingArchiver(
        archive_settings=RecordingArchiveSettings(
            enabled=True,
            archive_dir=archive_dir,
            min_age=timedelta(days=30),
            max_bundles=12,
            max_run_bytes=0,
            jitter=timedelta(minutes=1),
        ),
        recording_settings=_recording_settings(base_dir),
    )

    summary = archiver.run_once(now=now)

    assert cast_path.exists()
    assert summary.errors
    assert summary.reason == "partial-failure"

    # Reset permissions for cleanup on Windows/macOS
    archive_dir.chmod(0o700)


def test_respects_max_run_bytes_cap(tmp_path):
    now = datetime(2025, 4, 1, tzinfo=timezone.utc)
    base_dir = tmp_path / "casts"
    archive_dir = tmp_path / "archives"

    old_time = now - timedelta(days=40)
    sizes = [4, 4, 4]
    casts = []
    for idx, size in enumerate(sizes):
        cast_path = base_dir / "lab" / "2025-02-01" / f"session-00{idx}.cast"
        _touch(cast_path, content="x" * size, mtime=old_time)
        casts.append(cast_path)

    archiver = RecordingArchiver(
        archive_settings=RecordingArchiveSettings(
            enabled=True,
            archive_dir=archive_dir,
            min_age=timedelta(days=30),
            max_bundles=12,
            max_run_bytes=5,  # allow only one cast per run
            jitter=timedelta(minutes=1),
        ),
        recording_settings=_recording_settings(base_dir),
    )

    summary = archiver.run_once(now=now)
    assert summary.cap_reached is True
    assert summary.archived_files == 1
    assert not casts[0].exists()
    assert casts[1].exists() and casts[2].exists()

    # Next run should process another file
    summary_next = archiver.run_once(now=now + timedelta(hours=1))
    assert summary_next.archived_files == 1
    assert not casts[1].exists()
    assert casts[2].exists()
