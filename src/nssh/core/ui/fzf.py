"""fzf-related helpers kept separate from general utilities."""

from __future__ import annotations

import subprocess
import sys
from typing import List, Optional

from nssh.core.env.system import check_command


def check_fzf() -> bool:
    """Return True when the ``fzf`` executable is available."""

    return check_command("fzf")


def fzf_select(options: List[str], prompt: str = "Select:") -> Optional[str]:
    """Launch ``fzf`` with JSON-free input and return the selected option."""

    try:
        result = subprocess.run(
            ["fzf", "--prompt", f"{prompt} ", "--height", "40%", "--reverse"],
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
