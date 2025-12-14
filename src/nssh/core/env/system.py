"""Shell/OS helpers shared by core modules."""

from __future__ import annotations

import shutil
import subprocess
from pathlib import Path
from typing import List, Optional


def check_command(cmd: str) -> bool:
    """Return True if ``cmd`` is discoverable on PATH."""

    return shutil.which(cmd) is not None


def run_command(
    cmd: List[str],
    input_text: Optional[str] = None,
    error_context: str = "Command failed",
) -> subprocess.CompletedProcess:
    """Run a subprocess with standardised error handling."""

    try:
        return subprocess.run(
            cmd,
            capture_output=True,
            text=True,
            check=True,
            input=input_text,
        )
    except subprocess.CalledProcessError as exc:
        raise RuntimeError(f"{error_context}: {exc.stderr}") from exc


def set_secure_permissions(path: Path) -> None:
    """Apply 0600 permissions to ``path`` (best-effort)."""

    path.chmod(0o600)
