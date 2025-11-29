"""Shared batch operation utilities for nssh CLI commands."""

from __future__ import annotations

import csv
import json
from dataclasses import dataclass, field
from pathlib import Path
from typing import Callable, List, Optional, Type, TypeVar

from nssh.core.ui.console import get_console

console = get_console()

T = TypeVar("T")

# Security: Maximum batch file size to prevent memory exhaustion
MAX_BATCH_FILE_SIZE = 10 * 1024 * 1024  # 10 MB


@dataclass
class BatchResult:
    """Result of a batch operation."""

    added: int = 0
    skipped: int = 0
    failed: int = 0
    errors: List[str] = field(default_factory=list)

    @property
    def total_processed(self) -> int:
        return self.added + self.skipped + self.failed

    @property
    def removed(self) -> int:
        """Alias for added - used in removal operations for semantic clarity."""
        return self.added

    def has_failures(self) -> bool:
        return self.failed > 0


@dataclass
class HostEntry:
    """Entry for batch host operations (add/edit).

    The `host` field is optional. If omitted, the SSH alias is derived
    from the hostname by taking the first segment before the first dot.
    For example, 'rpi-b.home.arpa' becomes 'rpi-b'.
    """

    hostname: str
    user: Optional[str] = None
    port: Optional[int] = None
    context: Optional[str] = None
    password: Optional[str] = None
    host: Optional[str] = None

    @property
    def alias(self) -> str:
        """Return the SSH Host alias for this entry.

        If `host` is explicitly set, use it. Otherwise derive from hostname
        by splitting on the first dot.
        """
        if self.host:
            return self.host
        return self.hostname.split(".")[0]

    @classmethod
    def from_dict(cls, data: dict) -> "HostEntry":
        """Create HostEntry from a dictionary."""
        return cls(
            hostname=data.get("hostname", ""),
            user=data.get("user") or None,
            port=int(data["port"]) if data.get("port") else None,
            context=data.get("context") or None,
            password=data.get("password") or None,
            host=data.get("host") or None,
        )


@dataclass
class EditEntry:
    """Entry for batch edit operations."""

    hostname: str
    user: Optional[str] = None  # None = no change
    port: Optional[int] = None  # None = no change
    password: Optional[str] = None  # None = no change

    @classmethod
    def from_dict(cls, data: dict) -> "EditEntry":
        """Create EditEntry from a dictionary."""
        return cls(
            hostname=data.get("hostname", ""),
            user=data.get("user") or None,
            port=int(data["port"]) if data.get("port") else None,
            password=data.get("password") or None,
        )


def is_batch_file(arg: str) -> bool:
    """Detect if argument is a batch file based on extension.

    Args:
        arg: Command line argument (hostname or file path)

    Returns:
        True if arg ends with .txt, .csv, or .json (case-insensitive)
    """
    return arg.lower().endswith((".txt", ".csv", ".json"))


def parse_txt_file(path: Path) -> List[str]:
    """Parse a .txt file with one hostname per line.

    Args:
        path: Path to .txt file

    Returns:
        List of hostnames (empty lines and comments ignored)
    """
    hostnames = []
    with open(path, "r") as f:
        for line in f:
            line = line.strip()
            if line and not line.startswith("#"):
                hostnames.append(line)
    return hostnames


def parse_csv_file(path: Path, entry_cls: Type[T]) -> List[T]:
    """Parse a .csv file with headers into entry objects.

    Args:
        path: Path to .csv file
        entry_cls: Dataclass with from_dict() classmethod

    Returns:
        List of entry objects
    """
    entries = []
    with open(path, "r", newline="") as f:
        reader = csv.DictReader(f)
        for row in reader:
            entries.append(entry_cls.from_dict(row))  # type: ignore[attr-defined]
    return entries


def parse_json_file(path: Path, entry_cls: Type[T]) -> List[T]:
    """Parse a .json file (array of objects) into entry objects.

    Args:
        path: Path to .json file
        entry_cls: Dataclass with from_dict() classmethod

    Returns:
        List of entry objects
    """
    with open(path, "r") as f:
        data = json.load(f)

    if not isinstance(data, list):
        raise ValueError("JSON file must contain an array of objects")

    return [entry_cls.from_dict(item) for item in data]  # type: ignore[attr-defined]


def parse_batch_file(
    path: str, entry_cls: Type[T], txt_to_entry: Optional[Callable[[str], T]] = None
) -> List[T]:
    """Parse a batch file into entry objects based on extension.

    Args:
        path: Path to batch file (.txt, .csv, or .json)
        entry_cls: Dataclass with from_dict() classmethod
        txt_to_entry: Optional converter for .txt lines (if None, txt parsing fails)

    Returns:
        List of entry objects

    Raises:
        ValueError: If file format is unsupported or invalid
        FileNotFoundError: If file doesn't exist
    """
    file_path = Path(path)

    if not file_path.exists():
        raise FileNotFoundError(f"Batch file not found: {path}")

    # Security: Limit file size to prevent memory exhaustion
    file_size = file_path.stat().st_size
    if file_size > MAX_BATCH_FILE_SIZE:
        raise ValueError(
            f"Batch file too large ({file_size / 1024 / 1024:.1f} MB). "
            f"Maximum allowed: {MAX_BATCH_FILE_SIZE / 1024 / 1024:.0f} MB"
        )

    ext = file_path.suffix.lower()

    if ext == ".txt":
        if txt_to_entry is None:
            raise ValueError(".txt format requires a txt_to_entry converter")
        hostnames = parse_txt_file(file_path)
        return [txt_to_entry(h) for h in hostnames]
    elif ext == ".csv":
        return parse_csv_file(file_path, entry_cls)
    elif ext == ".json":
        return parse_json_file(file_path, entry_cls)
    else:
        raise ValueError(f"Unsupported file format: {ext}")


def display_batch_errors(result: BatchResult) -> None:
    """Display errors from a batch operation.

    Args:
        result: BatchResult with errors
    """
    if result.errors:
        for error in result.errors[:10]:
            console.print(f"  [red]{error}[/red]")
        if len(result.errors) > 10:
            console.print(f"  [dim]... and {len(result.errors) - 10} more[/dim]")


def validate_hostname(hostname: str) -> Optional[str]:
    """Validate a hostname/FQDN format.

    Args:
        hostname: Hostname to validate

    Returns:
        Error message if invalid, None if valid
    """
    if not hostname:
        return "hostname is required"
    if not hostname.strip():
        return "hostname cannot be empty"
    # Basic validation - hostname shouldn't have spaces or invalid chars
    if " " in hostname:
        return f"invalid hostname format: {hostname}"
    return None
