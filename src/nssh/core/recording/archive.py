"""Automatic recording archiver for asciinema cast files.

The archiver bundles recordings into monthly ``tar.gz`` files, prunes old
bundles, and deletes source casts only after successful verification.  It is
designed to be idempotent and cheap to invoke as part of normal CLI startup;
when disabled it is effectively a no-op.
"""

from __future__ import annotations

import json
import os
import random
import re
import tarfile
from dataclasses import dataclass, field
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Dict, List, Mapping, Optional, Sequence, Tuple

from nssh.core.diag import timing as timing_core
from nssh.core.env.settings import default_config_path, default_state_root, expand_path
from nssh.core.env.settings import load_toml_config as _load_toml_config
from nssh.core.recording.manager import (
    RecordingSettings,
    acquire_lock,
    load_recording_settings,
)

LOGGER = timing_core.get_logger()


# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------


@dataclass(frozen=True)
class RecordingArchiveSettings:
    enabled: bool = False
    archive_dir: Path = field(
        default_factory=lambda: default_state_root() / "archives"
    )
    min_age: timedelta = field(default_factory=lambda: timedelta(days=30))
    max_bundles: int = 12
    max_run_bytes: int = 0  # 0 = unlimited
    jitter: timedelta = field(default_factory=lambda: timedelta(minutes=30))


def _parse_bool(value: object, default: bool = False) -> bool:
    if value is None:
        return default
    if isinstance(value, bool):
        return value
    if isinstance(value, (int, float)):
        return bool(value)
    if isinstance(value, str):
        lowered = value.strip().lower()
        if lowered in {"1", "true", "yes", "on"}:
            return True
        if lowered in {"0", "false", "no", "off"}:
            return False
    return default


def _parse_int(value: object, default: int) -> int:
    try:
        if isinstance(value, bool):
            return default
        if isinstance(value, (int, float)):
            return int(value)
        if isinstance(value, str) and value.strip():
            return int(value.strip())
    except (TypeError, ValueError):
        pass
    return default


_DURATION_RE = re.compile(r"(?i)^\s*(?P<value>[\d.]+)\s*(?P<unit>[smhdw]?)\s*$")


def _parse_duration(value: object, default: timedelta) -> timedelta:
    """Parse a simple duration string like '30d', '12h', '15m', or seconds."""

    if value is None:
        return default
    if isinstance(value, timedelta):
        return value
    if isinstance(value, (int, float)):
        return timedelta(seconds=float(value))
    if isinstance(value, str):
        match = _DURATION_RE.match(value)
        if not match:
            return default
        magnitude = float(match.group("value"))
        unit = match.group("unit").lower()
        if unit == "s" or unit == "":
            return timedelta(seconds=magnitude)
        if unit == "m":
            return timedelta(minutes=magnitude)
        if unit == "h":
            return timedelta(hours=magnitude)
        if unit == "d":
            return timedelta(days=magnitude)
        if unit == "w":
            return timedelta(weeks=magnitude)
    return default


def _load_config() -> Mapping[str, object]:
    return _load_toml_config(default_config_path())


def load_archive_settings() -> RecordingArchiveSettings:
    """Load archive settings from config.toml."""

    config = _load_config()
    recording_section = config.get("recording", {}) if isinstance(config, dict) else {}
    archive_section = (
        recording_section.get("archive", {})
        if isinstance(recording_section, dict)
        else {}
    )

    enabled = _parse_bool(archive_section.get("enabled"), default=False)
    archive_dir_value = archive_section.get("dir")
    if isinstance(archive_dir_value, (str, os.PathLike)):
        archive_dir = expand_path(str(archive_dir_value))
    else:
        archive_dir = default_state_root() / "archives"

    min_age = _parse_duration(
        archive_section.get("min_age", archive_section.get("min_age_days")),  # compat
        default=timedelta(days=30),
    )
    jitter = _parse_duration(
        archive_section.get("jitter"),
        default=timedelta(minutes=30),
    )

    max_bundles = max(0, _parse_int(archive_section.get("max_bundles"), 12))
    max_run_bytes = max(0, _parse_int(archive_section.get("max_run_bytes"), 0))

    return RecordingArchiveSettings(
        enabled=enabled,
        archive_dir=archive_dir,
        min_age=min_age if min_age.total_seconds() > 0 else timedelta(days=30),
        max_bundles=max_bundles,
        max_run_bytes=max_run_bytes,
        jitter=jitter if jitter.total_seconds() >= 0 else timedelta(minutes=30),
    )


# ---------------------------------------------------------------------------
# Core data structures
# ---------------------------------------------------------------------------


@dataclass(frozen=True)
class ArchiveState:
    last_run_at: Optional[datetime]
    next_run_at: Optional[datetime]


@dataclass(frozen=True)
class ArchiveCandidate:
    path: Path
    relpath: str
    size: int
    mtime: datetime
    month_key: str


@dataclass(frozen=True)
class ArchiveRunSummary:
    started_at: datetime
    finished_at: datetime
    eligible_files: int
    processed_files: int
    archived_files: int
    skipped_existing: int
    deleted_files: int
    bundles_written: int
    bundles_pruned: int
    bytes_considered: int
    bytes_archived: int
    bytes_deleted: int
    cap_reached: bool
    errors: Tuple[str, ...] = ()
    reason: Optional[str] = None


# ---------------------------------------------------------------------------
# Archiver implementation
# ---------------------------------------------------------------------------


class RecordingArchiver:
    """Archiver that bundles stale recordings into monthly tarballs."""

    def __init__(
        self,
        archive_settings: Optional[RecordingArchiveSettings] = None,
        recording_settings: Optional[RecordingSettings] = None,
        rand: Optional[random.Random] = None,
    ) -> None:
        self.archive_settings = archive_settings or load_archive_settings()
        self.recording_settings = recording_settings or load_recording_settings()
        self.random = rand or random.Random()

        self._state_path = self.archive_settings.archive_dir / ".archive-state.json"
        self._lock_path = self.archive_settings.archive_dir / ".archive-state.lock"

    # Public API ------------------------------------------------------------

    def maybe_run(self, *, now: Optional[datetime] = None, force: bool = False) -> ArchiveRunSummary:
        """Run the archiver if enabled and due.

        Args:
            now: Optional reference time (defaults to current UTC time).
            force: Run regardless of next_run_at schedule.
        """
        start = now or datetime.now(timezone.utc)

        if not self.archive_settings.enabled:
            return ArchiveRunSummary(
                started_at=start,
                finished_at=start,
                eligible_files=0,
                processed_files=0,
                archived_files=0,
                skipped_existing=0,
                deleted_files=0,
                bundles_written=0,
                bundles_pruned=0,
                bytes_considered=0,
                bytes_archived=0,
                bytes_deleted=0,
                cap_reached=False,
                errors=(),
                reason="disabled",
            )

        self.archive_settings.archive_dir.mkdir(parents=True, exist_ok=True)

        with acquire_lock(self._lock_path):
            state = self._load_state()
            if (
                not force
                and state
                and state.next_run_at is not None
                and start < state.next_run_at
            ):
                return ArchiveRunSummary(
                    started_at=start,
                    finished_at=start,
                    eligible_files=0,
                    processed_files=0,
                    archived_files=0,
                    skipped_existing=0,
                    deleted_files=0,
                    bundles_written=0,
                    bundles_pruned=0,
                    bytes_considered=0,
                    bytes_archived=0,
                    bytes_deleted=0,
                    cap_reached=False,
                    errors=(),
                    reason="not-due",
                )

            summary = self.run_once(now=start)
            next_run = self._compute_next_run(start)
            self._write_state(ArchiveState(last_run_at=start, next_run_at=next_run))
            return summary

    def run_once(self, *, now: Optional[datetime] = None) -> ArchiveRunSummary:
        """Execute a single archive pass regardless of schedule."""

        started_at = now or datetime.now(timezone.utc)
        cfg = self.archive_settings
        recording_cfg = self.recording_settings

        pruned = 0
        prune_errors: List[str] = []

        cfg.archive_dir.mkdir(parents=True, exist_ok=True)

        base_dir = recording_cfg.directory
        if not base_dir.exists():
            pruned, prune_errors = self._prune_old_bundles(cfg.max_bundles)
            return ArchiveRunSummary(
                started_at=started_at,
                finished_at=started_at,
                eligible_files=0,
                processed_files=0,
                archived_files=0,
                skipped_existing=0,
                deleted_files=0,
                bundles_written=0,
                bundles_pruned=pruned,
                bytes_considered=0,
                bytes_archived=0,
                bytes_deleted=0,
                cap_reached=False,
                errors=tuple(prune_errors),
                reason="missing-recording-dir",
            )

        cutoff = started_at - cfg.min_age
        candidates, cap_reached, bytes_considered = self._discover_candidates(
            base_dir=base_dir,
            cutoff=cutoff,
            max_run_bytes=cfg.max_run_bytes,
        )

        if not candidates:
            pruned, prune_errors = self._prune_old_bundles(cfg.max_bundles)
            return ArchiveRunSummary(
                started_at=started_at,
                finished_at=datetime.now(timezone.utc),
                eligible_files=0,
                processed_files=0,
                archived_files=0,
                skipped_existing=0,
                deleted_files=0,
                bundles_written=0,
                bundles_pruned=pruned,
                bytes_considered=bytes_considered,
                bytes_archived=0,
                bytes_deleted=0,
                cap_reached=cap_reached,
                errors=tuple(prune_errors),
                reason="no-eligible-files" if not prune_errors else "partial-failure",
            )

        month_groups: Dict[str, List[ArchiveCandidate]] = {}
        for candidate in candidates:
            month_groups.setdefault(candidate.month_key, []).append(candidate)

        archived_files = 0
        skipped_existing = 0
        deleted_files = 0
        bytes_archived = 0
        bytes_deleted = 0
        bundles_written = 0
        errors: List[str] = []

        for month_key in sorted(month_groups.keys()):
            result = self._process_month(
                month_key=month_key,
                candidates=month_groups[month_key],
            )
            archived_files += result["archived"]
            skipped_existing += result["skipped"]
            deleted_files += result["deleted"]
            bytes_archived += result["bytes_archived"]
            bytes_deleted += result["bytes_deleted"]
            bundles_written += result["bundles_written"]
            errors.extend(result["errors"])

        pruned, prune_errors = self._prune_old_bundles(cfg.max_bundles)
        errors.extend(prune_errors)

        finished_at = datetime.now(timezone.utc)
        return ArchiveRunSummary(
            started_at=started_at,
            finished_at=finished_at,
            eligible_files=len(candidates),
            processed_files=len(candidates),
            archived_files=archived_files,
            skipped_existing=skipped_existing,
            deleted_files=deleted_files,
            bundles_written=bundles_written,
            bundles_pruned=pruned,
            bytes_considered=bytes_considered,
            bytes_archived=bytes_archived,
            bytes_deleted=bytes_deleted,
            cap_reached=cap_reached,
            errors=tuple(errors),
            reason=None if not errors else "partial-failure",
        )

    # Internal helpers -----------------------------------------------------

    def _compute_next_run(self, now: datetime) -> datetime:
        jitter_window = self.archive_settings.jitter.total_seconds()
        jitter_offset = self.random.uniform(-jitter_window, jitter_window)
        return now + timedelta(days=1) + timedelta(seconds=jitter_offset)

    def _load_state(self) -> Optional[ArchiveState]:
        if not self._state_path.exists():
            return None
        try:
            raw = json.loads(self._state_path.read_text(encoding="utf-8"))
            last_raw = raw.get("last_run_at")
            next_raw = raw.get("next_run_at")
            last_dt = (
                datetime.fromisoformat(last_raw) if isinstance(last_raw, str) else None
            )
            next_dt = (
                datetime.fromisoformat(next_raw) if isinstance(next_raw, str) else None
            )
            return ArchiveState(last_run_at=last_dt, next_run_at=next_dt)
        except Exception:
            return None

    def _write_state(self, state: ArchiveState) -> None:
        payload = {
            "last_run_at": state.last_run_at.isoformat()
            if state.last_run_at
            else None,
            "next_run_at": state.next_run_at.isoformat()
            if state.next_run_at
            else None,
        }
        tmp = self._state_path.with_name(self._state_path.name + ".tmp")
        self._state_path.parent.mkdir(parents=True, exist_ok=True)
        with open(tmp, "w", encoding="utf-8") as handle:
            json.dump(payload, handle)
            handle.flush()
            os.fsync(handle.fileno())
        os.replace(tmp, self._state_path)

    def _discover_candidates(
        self,
        *,
        base_dir: Path,
        cutoff: datetime,
        max_run_bytes: int,
    ) -> Tuple[List[ArchiveCandidate], bool, int]:
        """Find eligible .cast files grouped by month key."""

        candidates: List[ArchiveCandidate] = []
        cap_reached = False
        total_bytes = 0

        cast_files = []
        for cast_path in base_dir.rglob("*.cast"):
            if not cast_path.is_file():
                continue
            try:
                stat = cast_path.stat()
            except OSError:
                continue
            mtime = datetime.fromtimestamp(stat.st_mtime, tz=timezone.utc)
            if mtime >= cutoff:
                continue
            cast_files.append((mtime, cast_path, stat.st_size))

        # Deterministic ordering: oldest first, then path
        cast_files.sort(key=lambda item: (item[0], str(item[1])))

        for mtime, cast_path, size in cast_files:
            if max_run_bytes and (total_bytes + size) > max_run_bytes:
                cap_reached = True
                break
            relpath = cast_path.relative_to(base_dir).as_posix()
            month_key = mtime.strftime("%Y-%m")
            candidates.append(
                ArchiveCandidate(
                    path=cast_path,
                    relpath=relpath,
                    size=size,
                    mtime=mtime,
                    month_key=month_key,
                )
            )
            total_bytes += size

        return candidates, cap_reached, total_bytes

    def _process_month(
        self,
        *,
        month_key: str,
        candidates: Sequence[ArchiveCandidate],
    ) -> Dict[str, object]:
        archive_path = self.archive_settings.archive_dir / f"recordings-{month_key}.tar.gz"

        existing_sizes = self._read_existing_entries(archive_path)
        to_append: List[ArchiveCandidate] = []
        deletable: List[ArchiveCandidate] = []
        errors: List[str] = []

        for candidate in candidates:
            existing_size = existing_sizes.get(candidate.relpath)
            if existing_size is None:
                to_append.append(candidate)
                continue
            if existing_size == candidate.size:
                deletable.append(candidate)
            else:
                errors.append(
                    f"Size mismatch for {candidate.relpath} (archive={existing_size}, disk={candidate.size})"
                )

        wrote_bundle = False
        bytes_archived = 0
        appended_relpaths: List[str] = []

        if to_append:
            result = self._rewrite_archive(
                archive_path=archive_path,
                new_candidates=to_append,
            )
            errors.extend(result["errors"])
            if not result["failed"]:
                wrote_bundle = True
                bytes_archived = result["bytes_archived"]
                appended_relpaths = result["added"]
                # Newly written files become deletable after verification
                for candidate in to_append:
                    if candidate.relpath in appended_relpaths:
                        deletable.append(candidate)
            else:
                # On failure do not delete anything
                deletable = []

        deleted_files = 0
        bytes_deleted = 0
        for candidate in deletable:
            if self._delete_cast_and_index(candidate.path):
                deleted_files += 1
                bytes_deleted += candidate.size

        return {
            "archived": len(appended_relpaths),
            "skipped": len(deletable) - len(appended_relpaths),
            "deleted": deleted_files,
            "bytes_archived": bytes_archived,
            "bytes_deleted": bytes_deleted,
            "bundles_written": 1 if wrote_bundle else 0,
            "errors": errors,
        }

    def _read_existing_entries(self, archive_path: Path) -> Dict[str, int]:
        if not archive_path.exists():
            return {}
        try:
            with tarfile.open(archive_path, "r:gz") as tar:
                return {
                    member.name: member.size
                    for member in tar.getmembers()
                    if member.isfile()
                }
        except (tarfile.TarError, OSError) as exc:
            LOGGER.emit_log(
                f"recording_archive.py - Failed to read archive {archive_path}: {exc}"
            )
            return {}

    def _rewrite_archive(
        self,
        *,
        archive_path: Path,
        new_candidates: Sequence[ArchiveCandidate],
    ) -> Dict[str, object]:
        tmp_path = archive_path.with_name(archive_path.name + ".tmp")
        added: List[str] = []
        errors: List[str] = []
        bytes_archived = 0

        try:
            # Open temporary archive for writing (gzip level 1 for speed)
            with tarfile.open(
                tmp_path, "w:gz", compresslevel=1, format=tarfile.PAX_FORMAT
            ) as out_tar:
                # Copy existing entries if archive already exists
                if archive_path.exists():
                    with tarfile.open(archive_path, "r:gz") as in_tar:
                        for member in in_tar.getmembers():
                            fileobj = in_tar.extractfile(member) if member.isfile() else None
                            out_tar.addfile(member, fileobj)
                            if fileobj:
                                fileobj.close()

                # Append new candidates
                for candidate in new_candidates:
                    info = tarfile.TarInfo(candidate.relpath)
                    info.size = candidate.size
                    info.mtime = candidate.mtime.timestamp()
                    info.mode = 0o600
                    with open(candidate.path, "rb") as handle:
                        out_tar.addfile(info, handle)
                    added.append(candidate.relpath)
                    bytes_archived += candidate.size

                out_tar.close()  # Ensure data flushed before fsync

            # fsync temp file then replace
            with open(tmp_path, "rb") as handle:
                handle.flush()
                os.fsync(handle.fileno())
            os.replace(tmp_path, archive_path)
            with open(archive_path, "rb") as handle:
                os.fsync(handle.fileno())
        except Exception as exc:  # pragma: no cover - defensive
            errors.append(f"Failed to write archive {archive_path}: {exc}")
            try:
                tmp_path.unlink(missing_ok=True)
            except OSError:
                pass
            return {
                "failed": True,
                "added": added,
                "errors": errors,
                "bytes_archived": bytes_archived,
            }

        # Verify sizes of newly added entries
        try:
            with tarfile.open(archive_path, "r:gz") as tar:
                sizes = {m.name: m.size for m in tar.getmembers() if m.isfile()}
            for candidate in new_candidates:
                archived_size = sizes.get(candidate.relpath)
                if archived_size != candidate.size:
                    errors.append(
                        f"Verification failed for {candidate.relpath}: expected {candidate.size}, found {archived_size}"
                    )
        except Exception as exc:  # pragma: no cover - defensive
            errors.append(f"Failed to verify archive {archive_path}: {exc}")

        failed = bool(errors)
        return {
            "failed": failed,
            "added": added if not failed else [],
            "errors": errors,
            "bytes_archived": bytes_archived if not failed else 0,
        }

    def _delete_cast_and_index(self, cast_path: Path) -> bool:
        """Delete cast file and its index if present, ignoring errors."""

        try:
            cast_path.unlink(missing_ok=False)
        except FileNotFoundError:
            return False
        except OSError as exc:  # pragma: no cover - defensive
            LOGGER.emit_log(
                f"recording_archive.py - Failed to delete cast {cast_path}: {exc}"
            )
            return False

        index_path = cast_path.with_suffix(".index.json")
        try:
            index_path.unlink(missing_ok=True)
        except OSError as exc:
            LOGGER.emit_log(
                f"recording_archive.py - Failed to delete index {index_path}: {exc}"
            )

        self._cleanup_empty_dirs(cast_path.parent)
        return True

    def _cleanup_empty_dirs(self, start_dir: Path) -> None:
        base_dir = self.recording_settings.directory
        current = start_dir
        while base_dir in current.parents or current == base_dir:
            try:
                current.rmdir()
            except OSError:
                break
            if current == base_dir:
                break
            current = current.parent

    def _prune_old_bundles(self, keep: int) -> Tuple[int, List[str]]:
        """Retain the newest ``keep`` bundles, deleting older ones."""

        pattern = re.compile(r"^recordings-(\d{4})-(\d{2})\.tar\.gz$")
        archives: List[Tuple[int, int, Path]] = []
        for entry in self.archive_settings.archive_dir.glob("recordings-*.tar.gz"):
            match = pattern.match(entry.name)
            if not match:
                continue
            year = int(match.group(1))
            month = int(match.group(2))
            archives.append((year, month, entry))

        # Sort descending (newest first)
        archives.sort(key=lambda item: (item[0], item[1]), reverse=True)
        errors: List[str] = []
        pruned = 0

        if keep <= 0:
            return (0, errors)

        for entry in archives[keep:]:
            path = entry[2]
            try:
                path.unlink()
                pruned += 1
            except OSError as exc:
                errors.append(f"Failed to delete archive {path}: {exc}")

        return pruned, errors


# Convenience ----------------------------------------------------------------


def maybe_run_archive(now: Optional[datetime] = None) -> ArchiveRunSummary:
    """Convenience helper used by CLI startup."""

    archiver = RecordingArchiver()
    return archiver.maybe_run(now=now)
