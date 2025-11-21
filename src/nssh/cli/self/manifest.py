#!/usr/bin/env python3
"""Installation manifest tracking for self command."""

from __future__ import annotations

import json
from dataclasses import dataclass, field
from datetime import datetime
from pathlib import Path
from typing import Any, Dict, List, Optional


@dataclass
class InstalledFile:
    """Represents a file installed or tracked by self."""

    path: str
    type: str  # "file", "symlink", "directory", "reference"
    source: Optional[str] = None  # Asset source (e.g., "scripts/helper.sh")
    target: Optional[str] = None  # Symlink target
    reference_type: Optional[str] = (
        None  # For type="reference": "age_key", "ssh_config", etc.
    )

    def to_dict(self) -> Dict[str, Any]:
        return {
            "path": self.path,
            "type": self.type,
            "source": self.source,
            "target": self.target,
            "reference_type": self.reference_type,
        }

    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> InstalledFile:
        return cls(
            path=data["path"],
            type=data["type"],
            source=data.get("source"),
            target=data.get("target"),
            reference_type=data.get("reference_type"),
        )


@dataclass
class ProfileModification:
    """Represents a modification to a shell profile file."""

    path: str
    marker: str
    line_start: int
    line_end: int

    def to_dict(self) -> Dict[str, Any]:
        return {
            "path": self.path,
            "marker": self.marker,
            "line_start": self.line_start,
            "line_end": self.line_end,
        }

    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> ProfileModification:
        return cls(
            path=data["path"],
            marker=data["marker"],
            line_start=data["line_start"],
            line_end=data["line_end"],
        )


@dataclass
class InstallManifest:
    """Tracks what self has installed for uninstall support."""

    version: str = "1"
    installed_at: Optional[str] = None
    files: List[InstalledFile] = field(default_factory=list)
    profile_modifications: List[ProfileModification] = field(default_factory=list)

    def to_dict(self) -> Dict[str, Any]:
        return {
            "version": self.version,
            "installed_at": self.installed_at or datetime.now().isoformat(),
            "files": [f.to_dict() for f in self.files],
            "profile_modifications": [p.to_dict() for p in self.profile_modifications],
        }

    @classmethod
    def from_dict(cls, data: Dict[str, Any]) -> InstallManifest:
        return cls(
            version=data.get("version", "1"),
            installed_at=data.get("installed_at"),
            files=[InstalledFile.from_dict(f) for f in data.get("files", [])],
            profile_modifications=[
                ProfileModification.from_dict(p)
                for p in data.get("profile_modifications", [])
            ],
        )

    def add_file(
        self,
        path: Path,
        file_type: str,
        source: Optional[str] = None,
        target: Optional[Path] = None,
    ) -> None:
        """Add a file to the manifest."""
        self.files.append(
            InstalledFile(
                path=str(path.resolve()),
                type=file_type,
                source=source,
                target=str(target.resolve()) if target else None,
            )
        )

    def add_profile_modification(
        self, path: Path, marker: str, line_start: int, line_end: int
    ) -> None:
        """Add a profile modification to the manifest."""
        self.profile_modifications.append(
            ProfileModification(
                path=str(path.resolve()),
                marker=marker,
                line_start=line_start,
                line_end=line_end,
            )
        )

    def add_reference_file(self, path: Path, reference_type: str) -> None:
        """Track existing file for reference (not created by self)."""
        self.files.append(
            InstalledFile(
                path=str(path.resolve()),
                type="reference",
                source=None,
                target=None,
                reference_type=reference_type,
            )
        )


def get_manifest_path(share_dir: Path) -> Path:
    """Return the path to the installation manifest."""
    return share_dir / "manifest.json"


def read_manifest(share_dir: Path) -> Optional[InstallManifest]:
    """Read the installation manifest if it exists."""
    manifest_path = get_manifest_path(share_dir)
    if not manifest_path.exists():
        return None

    try:
        with open(manifest_path) as f:
            data = json.load(f)
        return InstallManifest.from_dict(data)
    except (json.JSONDecodeError, KeyError, ValueError) as exc:
        raise RuntimeError(f"Failed to parse manifest: {exc}") from exc


def write_manifest(manifest: InstallManifest, share_dir: Path) -> None:
    """Write the installation manifest atomically."""
    manifest_path = get_manifest_path(share_dir)
    manifest_path.parent.mkdir(parents=True, exist_ok=True)

    # Write to temp file first
    temp_path = manifest_path.with_suffix(".json.tmp")
    with open(temp_path, "w") as f:
        json.dump(manifest.to_dict(), f, indent=2)
        f.write("\n")

    # Atomic rename
    temp_path.replace(manifest_path)


def delete_manifest(share_dir: Path) -> None:
    """Delete the installation manifest."""
    manifest_path = get_manifest_path(share_dir)
    if manifest_path.exists():
        manifest_path.unlink()
