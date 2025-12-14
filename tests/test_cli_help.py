from __future__ import annotations

import os
import subprocess
import sys
from pathlib import Path

import click
import pytest

REPO_ROOT = Path(__file__).resolve().parents[1]
HELP_DIR = REPO_ROOT / "docs" / "examples" / "help"

# CLI command groups and their module paths
CLI_GROUPS = {
    "host": "nssh.cli.host",
    "log": "nssh.cli.log",
    "ctx": "nssh.cli.ctx",
    "self": "nssh.cli.self",
    "benchmark": "nssh.cli.benchmark",
}


def _get_module_app(module_path: str):
    """Import a CLI module and return its app object."""
    import importlib

    module = importlib.import_module(module_path)
    return module.app


def _discover_subcommands(
    app: click.Group, group_name: str, cmd_prefix: list[str] | None = None
) -> dict[str, list[str]]:
    """Recursively discover all subcommands from a Click group.

    Returns a mapping of snapshot path to subcommand parts (without group prefix).
    """
    if cmd_prefix is None:
        cmd_prefix = []

    cases = {}
    for name in app.commands:
        cmd = app.commands[name]
        # Build snapshot path: group/subcommand.txt or group/nested/subcommand.txt
        path_parts = [group_name, *cmd_prefix, name]
        snapshot_path = "/".join(path_parts) + ".txt"
        # Command parts are just the subcommand names (no group prefix)
        subcommand_parts = [*cmd_prefix, name]
        cases[snapshot_path] = subcommand_parts

        if isinstance(cmd, click.Group):
            # Recurse into nested groups
            cases.update(_discover_subcommands(cmd, group_name, subcommand_parts))

    return cases


def _discover_all_commands() -> dict[str, list[str]]:
    """Build complete mapping of snapshot paths to commands."""
    cases = {
        # Main entry point
        "nssh.txt": [sys.executable, "-m", "nssh.cli.main", "--help"],
        # Single command (no subcommands)
        "cp.txt": [sys.executable, "-m", "nssh.cli.main", "cp", "--help"],
    }

    # Add group-level help and subcommands
    for group_name, module_path in CLI_GROUPS.items():
        # Group help (e.g., host.txt)
        cases[f"{group_name}.txt"] = [sys.executable, "-m", module_path, "--help"]

        # Discover subcommands
        app = _get_module_app(module_path)
        subcommands = _discover_subcommands(app, group_name)
        for snapshot_path, subcommand_parts in subcommands.items():
            cases[snapshot_path] = [
                sys.executable,
                "-m",
                module_path,
                *subcommand_parts,
                "--help",
            ]

    return cases


def _command_to_user_string(command: list[str]) -> str:
    """Convert internal command to user-facing nssh command string.

    Example: [python, -m, nssh.cli.host, add, --help] -> "nssh host add --help"
    """
    # Skip python executable and -m flag
    parts = command[2:]  # Start after [python, -m]

    # Convert module path to command name
    module = parts[0]  # e.g., "nssh.cli.host" or "nssh.cli.main"
    if module == "nssh.cli.main":
        cmd_parts = ["nssh"]
    else:
        # nssh.cli.host -> ["nssh", "host"]
        group = module.split(".")[-1]
        cmd_parts = ["nssh", group]

    # Add remaining arguments (subcommands, --help, etc.)
    cmd_parts.extend(parts[1:])
    return " ".join(cmd_parts)


def _capture_command(command: list[str]) -> str:
    """Execute a command and capture its output."""
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


def _capture_command_with_header(command: list[str]) -> str:
    """Execute command and return output with command header."""
    user_cmd = _command_to_user_string(command)
    output = _capture_command(command)
    return f"$ {user_cmd}\n{output}"


def _read_snapshot(name: str) -> str:
    """Read a help snapshot file."""
    path = HELP_DIR / name
    if not path.exists():
        raise AssertionError(f"Missing help snapshot: {path}")
    return path.read_text().replace("\r\n", "\n")


def _write_snapshot(name: str, content: str) -> None:
    """Write a help snapshot file."""
    path = HELP_DIR / name
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(content)


# Discover all commands at module load time
HELP_CASES = _discover_all_commands()


@pytest.mark.parametrize("snapshot,command", list(HELP_CASES.items()))
def test_cli_help_snapshot(snapshot: str, command: list[str]):
    """Verify help output matches stored snapshot."""
    expected = _read_snapshot(snapshot)
    observed = _capture_command_with_header(command)
    assert observed == expected, (
        f"Help output drifted from {snapshot}.\n" f"Run: python tests/test_cli_help.py"
    )


def regenerate_all_snapshots() -> None:
    """Regenerate all help snapshots (utility function)."""
    for snapshot_path, command in HELP_CASES.items():
        output = _capture_command_with_header(command)
        _write_snapshot(snapshot_path, output)
        print(f"  Updated: {snapshot_path}")


# ---------------------------------------------------------------------------
# Help synchronization tests - ensure Click options appear in subcommand help
# ---------------------------------------------------------------------------

# Single commands that show options in their own help (not groups)
SINGLE_COMMANDS = {
    "cp": "nssh.cli.cp",
}


def _get_command_options(cmd) -> set[str]:
    """Extract all option names from a Click command."""
    options = set()
    for param in cmd.params:
        if isinstance(param, click.Option):
            for opt in param.opts:
                options.add(opt)
    return options


def _capture_command_help(module_path: str) -> str:
    """Capture help output from a single CLI command."""
    if module_path == "nssh.cli.cp":
        command = [sys.executable, "-m", "nssh.cli.main", "cp", "--help"]
    else:
        command = [sys.executable, "-m", module_path, "--help"]
    env = os.environ.copy()
    env.setdefault("COLUMNS", "120")
    result = subprocess.run(
        command,
        cwd=REPO_ROOT,
        capture_output=True,
        text=True,
        env=env,
    )
    return result.stdout + result.stderr


@pytest.mark.parametrize("name,module_path", list(SINGLE_COMMANDS.items()))
def test_single_command_options_appear_in_help(name: str, module_path: str):
    """Single commands (not groups) show their options in help output."""
    app = _get_module_app(module_path)
    options = _get_command_options(app)
    help_output = _capture_command_help(module_path)

    missing = []
    for opt in options:
        if opt not in help_output:
            missing.append(opt)

    assert not missing, (
        f"Options missing from {name} help output: {missing}\n"
        f"Defined options: {sorted(options)}"
    )


if __name__ == "__main__":
    print("Regenerating all help snapshots...")
    regenerate_all_snapshots()
    print("Done!")
