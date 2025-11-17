"""Tests for the Fish shell completion script."""

from pathlib import Path
import subprocess

import pytest


COMPLETION_FILE = Path("src/nssh/assets/completions/nssh.fish")


def is_fish_installed() -> bool:
    try:
        subprocess.run(
            ["fish", "--version"], capture_output=True, check=True, timeout=5
        )
        return True
    except (
        subprocess.CalledProcessError,
        FileNotFoundError,
        subprocess.TimeoutExpired,
    ):
        return False


pytestmark = pytest.mark.skipif(
    not is_fish_installed(), reason="Fish shell not installed"
)


def test_completion_file_exists():
    assert COMPLETION_FILE.exists(), "nssh.fish completion file missing"


def test_fish_completion_syntax():
    result = subprocess.run(
        ["fish", "--no-execute", str(COMPLETION_FILE)],
        capture_output=True,
        text=True,
        timeout=10,
    )
    assert result.returncode == 0, (
        f"\nFish syntax error in {COMPLETION_FILE.name}:\n"
        f"STDERR: {result.stderr}\nSTDOUT: {result.stdout}"
    )


def test_completion_delegates_to_typer():
    text = COMPLETION_FILE.read_text()
    assert "_NSSH_COMPLETE" in text, "Fish completion no longer calls Typer"
    # Typer uses long-form: complete --command nssh
    assert (
        "complete --command nssh" in text or "complete -c nssh" in text
    ), "Fish completion missing top-level registration"
    # Verify it uses Typer's Fish completion protocol
    assert (
        "_TYPER_COMPLETE_FISH_ACTION" in text
    ), "Missing Typer Fish completion protocol"


def test_no_extra_completion_files():
    completions_dir = COMPLETION_FILE.parent
    files = sorted(completions_dir.glob("*.fish"))
    assert files == [COMPLETION_FILE], "Found unexpected completion files"
