from __future__ import annotations

import os
import subprocess
import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[1]
HELP_DIR = REPO_ROOT / "docs" / "examples" / "help"


HELP_CASES = {
    "nssh.txt": [sys.executable, "-m", "nssh.cli.main", "--help"],
    "nssh-cred.txt": [sys.executable, "-m", "nssh.cli.cred", "--help"],
    "nssh-host.txt": [sys.executable, "-m", "nssh.cli.host", "--help"],
    "nssh-benchmark.txt": [
        sys.executable,
        "-m",
        "nssh.cli.benchmark",
        "--help",
    ],
    "nssh-log.txt": [sys.executable, "-m", "nssh.cli.log", "--help"],
    "nssh-install-shell.txt": [
        sys.executable,
        "-m",
        "nssh.cli.install_shell",
        "--help",
    ],
}


def _capture_command(command: list[str]) -> str:
    env = os.environ.copy()
    env.setdefault("COLUMNS", "80")
    result = subprocess.run(
        command,
        cwd=REPO_ROOT,
        capture_output=True,
        text=True,
        env=env,
    )
    if result.returncode != 0:
        raise AssertionError(
            f"Command {command} failed with {result.returncode}: {result.stderr}"
        )
    combined = result.stdout + result.stderr
    return combined.replace("\r\n", "\n")


def _read_snapshot(name: str) -> str:
    path = HELP_DIR / name
    if not path.exists():
        raise AssertionError(f"Missing help snapshot: {path}")
    return path.read_text().replace("\r\n", "\n")


def test_cli_help_snapshots_are_current():
    for snapshot, command in HELP_CASES.items():
        expected = _read_snapshot(snapshot)
        observed = _capture_command(command)
        assert (
            observed == expected
        ), f"Help output for {command} drifted from {snapshot}. Update docs/examples/help/{snapshot} if intentional."
