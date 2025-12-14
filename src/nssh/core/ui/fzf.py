"""fzf-related helpers kept separate from general utilities."""

from __future__ import annotations

import subprocess
import sys
from typing import List, Optional

from nssh.core.env.system import check_command


def check_fzf() -> bool:
    """Return True when the ``fzf`` executable is available."""

    return check_command("fzf")


def fzf_select(
    options: List[str], prompt: str = "Select:", query: Optional[str] = None
) -> Optional[str]:
    """Launch ``fzf`` with JSON-free input and return the selected option."""

    cmd = ["fzf", "--prompt", f"{prompt} ", "--height", "40%", "--reverse"]
    if query:
        cmd.extend(["--query", query])

    try:
        result = subprocess.run(
            cmd,
            input="\n".join(options),
            text=True,
            capture_output=True,
        )
    except Exception as exc:  # pragma: no cover - subprocess creation edge cases
        print(f"Error running fzf: {exc}", file=sys.stderr)
        return None

    if result.returncode == 0:
        return result.stdout.strip()
    return None


def fzf_select_multi(
    options: List[str], prompt: str = "Select:", query: Optional[str] = None
) -> List[str]:
    """Launch ``fzf`` with multi-select and return selected options."""

    cmd = [
        "fzf",
        "--prompt",
        f"{prompt} ",
        "--height",
        "40%",
        "--reverse",
        "--multi",
    ]
    if query:
        cmd.extend(["--query", query])

    try:
        result = subprocess.run(
            cmd,
            input="\n".join(options),
            text=True,
            capture_output=True,
        )
    except Exception as exc:  # pragma: no cover - subprocess creation edge cases
        print(f"Error running fzf: {exc}", file=sys.stderr)
        return []

    if result.returncode == 0:
        return [line for line in result.stdout.strip().split("\n") if line]
    return []
