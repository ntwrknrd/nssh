#!/usr/bin/env python3
"""Minimal recording check for nssh wrapper - optimized for speed.

This script is called by the nssh wrapper to check if recording should be
enabled for a given host. It avoids importing heavy CLI dependencies (typer,
rich) to minimize overhead during SSH connection setup.
"""

from __future__ import annotations

import os
import sys
from typing import Sequence


def _normalize_args(argv: Sequence[str] | None = None) -> list[str]:
    return list(sys.argv[1:] if argv is None else argv)


def main(argv: Sequence[str] | None = None) -> None:
    """Check recording configuration and output plan for shell wrapper."""
    # Fast path: if NSSH_RECORD=0, skip all config loading
    if os.environ.get("NSSH_RECORD", "1") == "0":
        print("enabled=0")
        print("reason=recording disabled by NSSH_RECORD=0")
        print("append=1")
        return

    # Only import recording module when actually needed
    from nssh.core.recording import manager as recording

    args = _normalize_args(argv)
    hostname = args[0] if args else "check-host"

    # Compute recording plan with directory preparation and sequence allocation
    plan_obj = recording._compute_plan(
        hostname,
        prepare_dirs=True,
        allocate_sequence=True,
    )

    # Output machine-readable key=value pairs for shell wrapper
    print(f"enabled={'1' if plan_obj.enabled else '0'}")
    if plan_obj.reason:
        print(f"reason={plan_obj.reason}")
    if plan_obj.cast_path:
        print(f"cast_path={plan_obj.cast_path}")
    if plan_obj.append:
        print("append=1")
    else:
        print("append=0")
    if plan_obj.title:
        print(f"title={plan_obj.title}")
    if plan_obj.asciinema_bin:
        print(f"asciinema_bin={plan_obj.asciinema_bin}")
    if plan_obj.lock_directory:
        print(f"lock_dir={plan_obj.lock_directory}")
    if plan_obj.sequence is not None:
        print(f"sequence={plan_obj.sequence}")
    if plan_obj.session_label:
        print(f"session_label={plan_obj.session_label}")


if __name__ == "__main__":
    main()
