#!/usr/bin/env python3
"""Connect helper that streams credentials through inherited file descriptors."""

import os
import shutil
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Dict, List, Optional, Sequence, Tuple

from nssh.core.diag import timing as timing_core
from nssh.core.auth.credentials import CredentialManager
from nssh.core.env.paths import host_index_path
from nssh.core.ssh.config import SSHConfigParser
from nssh.core.ssh.fixer import detect_auth_type

LOGGER = timing_core.get_logger()
DEBUG_ENABLED = os.getenv(timing_core.TIMING_ENV_FLAG, "0") == "1"


class ConnectError(RuntimeError):
    """Base error that carries an exit code for CLI handling."""

    exit_code: int = 1

    def __init__(self, message: str, *, exit_code: int | None = None) -> None:
        super().__init__(message)
        if exit_code is not None:
            self.exit_code = exit_code


class MultipleMatchesError(ConnectError):
    exit_code = 2

    def __init__(self, matches: Dict[str, str]):
        super().__init__("Multiple matches")
        self.matches = matches


class NoMatchesError(ConnectError):
    exit_code = 3


class CredentialExpectationError(ConnectError):
    exit_code = 1


@dataclass(frozen=True)
class HostMatch:
    hostname: str
    filepath: str


@dataclass(frozen=True)
class CredentialResult:
    username: Optional[str]
    password: Optional[str]


def log_debug(message: str) -> None:
    """Emit structured log lines when timing is enabled."""

    LOGGER.emit_log(f"connect.py - {message}")


def find_exact_match_in_index(
    search_term: str, index_path: Path
) -> Optional[Tuple[str, str]]:
    """
    Find exact hostname match in index file

    Args:
        search_term: Hostname to search for
        index_path: Path to index file

    Returns:
        Tuple of (hostname, filepath) if found, None otherwise
    """
    log_debug("START: index file exact match")

    if not index_path.exists():
        log_debug("END: index file exact match (no index)")
        return None

    try:
        with open(index_path, "r") as f:
            for line in f:
                line = line.strip()
                if "|" not in line:
                    continue

                hostname, filepath = line.split("|", 1)
                if hostname == search_term:
                    log_debug("END: index file exact match (found)")
                    return (hostname, filepath)

        log_debug("END: index file exact match (not found)")
        return None
    except Exception as e:
        log_debug(f"END: index file exact match (error: {e})")
        return None


def _parse_all_hosts(parser: SSHConfigParser) -> Dict[str, str]:
    config_files = [parser.config_file]
    config_files.extend(parser.find_include_files())

    all_hosts: Dict[str, str] = {}
    for config_file in config_files:
        if not config_file.exists():
            continue

        _, hosts = parser.parse_ssh_config(config_file)
        for hostname, host_lines in hosts:
            aliases = parser.extract_aliases(host_lines) or [hostname]
            for alias in aliases:
                if "?" in alias or "*" in alias:
                    continue
                all_hosts[alias] = str(config_file)
    return all_hosts


def find_host_match(search_term: str) -> HostMatch:
    """
    Find hostname match - exact or fuzzy

    Args:
        search_term: Hostname or partial hostname to search for

    Returns:
        Tuple of (hostname, filepath)

    Raises:
        SystemExit: If no match found or multiple matches (needs fzf)
    """
    with timing_core.stage("host-selection"):
        # Try index file first for exact match
        index_path = host_index_path()
        result = find_exact_match_in_index(search_term, index_path)

        if result:
            return HostMatch(*result)

        parser = SSHConfigParser()
        with timing_core.stage("config-parse"):
            all_hosts = _parse_all_hosts(parser)

        # Check for exact match (in case index is stale)
        if search_term in all_hosts:
            return HostMatch(search_term, all_hosts[search_term])

        # Find partial matches (case-insensitive)
        matches = {
            h: f for h, f in all_hosts.items() if search_term.lower() in h.lower()
        }

        if len(matches) == 0:
            raise NoMatchesError(f"No hosts matching '{search_term}'")
        elif len(matches) == 1:
            # Single match - use it
            hostname = list(matches.keys())[0]
            return HostMatch(hostname, matches[hostname])
        else:
            # Multiple matches - handled by CLI for fzf
            raise MultipleMatchesError(matches)


def resolve_credential_for_host(
    hostname: str, filepath: str, username: Optional[str] = None
) -> CredentialResult:
    """
    Resolve credentials for a hostname

    Args:
        hostname: Hostname to resolve credentials for
        filepath: Full path to SSH config file
        username: Optional username to match

    Returns:
        Tuple of (username, password) if found, (None, None) otherwise
    """
    with timing_core.stage("credential-vault", detail=hostname):
        log_debug("START: credential resolution")

        try:
            # Extract git include file name from filepath
            git_include_file = Path(filepath).name if filepath else None

            # Create credential manager (includes decryption)
            log_debug("START: CredentialManager creation")
            cm = CredentialManager()
            log_debug("END: CredentialManager creation")

            # Resolve credential
            result = cm.resolve_credential(
                hostname=hostname, git_include_file=git_include_file, username=username
            )

            log_debug("END: credential resolution")

            if result:
                return CredentialResult(*result)
            else:
                # No credential found - check if one was expected
                log_debug("START: check auth type")
                parser = SSHConfigParser()
                host_info = parser.find_host_in_files(hostname)

                if host_info:
                    _, host_lines = host_info
                    auth_type = detect_auth_type(host_lines)
                    log_debug(f"END: check auth type (type={auth_type})")

                    # If password or keyboard-interactive auth, credential was expected
                    if auth_type in ["password", "keyboard-interactive"]:
                        if username:
                            message = (
                                f"No credential found for user '{username}' on host '{hostname}'. "
                                f"Host is configured for {auth_type} authentication."
                            )
                        else:
                            message = (
                                f"No credential found for host '{hostname}'. "
                                f"Host is configured for {auth_type} authentication."
                            )
                        raise CredentialExpectationError(message)

                return CredentialResult(None, None)

        except ConnectError:
            raise
        except Exception as e:
            raise ConnectError(str(e))


def _normalize_args(argv: Sequence[str] | None = None) -> List[str]:
    return list(sys.argv[1:] if argv is None else argv)


def _select_host_with_fzf(matches: Dict[str, str]) -> HostMatch:
    if not matches:
        raise ConnectError("No matches available for selection")

    if shutil.which("fzf") is None:
        raise ConnectError("Multiple matches found but 'fzf' is not installed")

    hostnames = sorted(matches.keys())
    candidates = "\n".join(hostnames)

    try:
        result = subprocess.run(
            [
                "fzf",
                "--prompt",
                "Select host: ",
                "--height",
                "40%",
                "--layout",
                "reverse",
            ],
            input=candidates,
            text=True,
            capture_output=True,
            check=False,
        )
    except OSError as exc:  # pragma: no cover - system call failure
        raise ConnectError(f"Failed to launch fzf: {exc}") from exc

    if result.returncode != 0:
        raise ConnectError("Host selection cancelled", exit_code=1)

    selection = result.stdout.strip()
    if not selection:
        raise ConnectError("Host selection cancelled", exit_code=1)

    filepath = matches.get(selection)
    if filepath is None:
        raise ConnectError(f"Invalid host selected: {selection}")

    return HostMatch(selection, filepath)


def _split_extra_args(args: List[str]) -> Tuple[List[str], List[str]]:
    if "--" not in args:
        return args, []

    idx = args.index("--")
    # Include the -- separator in the ssh_args for PTY connector
    return args[:idx], args[idx:]


def _split_connection_args(args: List[str]) -> Tuple[List[str], List[str]]:
    base, trailing = _split_extra_args(args)

    if len(base) <= 1:
        return base, trailing

    # All args after hostname are SSH passthrough args
    ssh_args = base[1:] + trailing
    return [base[0]], ssh_args


def _run_pty_connector(
    *,
    hostname: str,
    requested_username: Optional[str],
    resolved_username: Optional[str],
    password: Optional[str],
    ssh_args: Sequence[str],
) -> int:
    """Run PTY connector for interactive SSH session with optional recording."""
    from nssh.core.connector import run_with_pty
    from nssh.core.recording import manager as recording

    with timing_core.stage("connection-orchestration", detail=hostname):
        ssh_username = requested_username or resolved_username
        normalized_args: Sequence[str] = tuple(ssh_args) if ssh_args else ()

    with timing_core.stage("recording-setup", detail=hostname):
        plan = recording._compute_plan(
            hostname,
            prepare_dirs=True,
            allocate_sequence=True,
        )

    try:
        return run_with_pty(
            hostname=hostname,
            username=ssh_username,
            password=password,
            ssh_args=normalized_args,
            recording_plan=plan,
        )
    except RuntimeError as exc:
        print(f"Error: {exc}", file=sys.stderr)
        return 1


def main(argv: Sequence[str] | None = None):
    """
    Main entry point - unified host selection and credential resolution

    Args (via command line):
        search_term: Hostname or partial hostname to search for
        username: Optional username for credential resolution
        -- [SSH_ARGS]: Optional SSH arguments to pass through

    Exit codes:
        0: Success
        1: Error
    """

    raw_args = _normalize_args(argv)

    args, ssh_passthrough_args = _split_connection_args(raw_args)

    with timing_core.run_span("connect-workflow"):
        # CLI initialization: argument parsing and validation
        with timing_core.stage("cli-startup", detail="ssh"):
            if not args:
                print(
                    "Usage: nssh [USER@]HOST [SSH_ARGS...]",
                    file=sys.stderr,
                )
                sys.exit(1)

            search_term = args[0]
            username = None

            # Strip user@ prefix if present (for consistency with nssh-select)
            if "@" in search_term and not search_term.startswith("@"):
                parts = search_term.split("@", 1)
                if len(parts) == 2 and parts[1]:
                    # user@hostname format
                    username = parts[0]
                    search_term = parts[1]

        # Find hostname (exact or fuzzy)
        try:
            host_match = find_host_match(search_term)
        except MultipleMatchesError as exc:
            host_match = _select_host_with_fzf(exc.matches)
        except ConnectError as exc:
            print(f"Error: {exc}", file=sys.stderr)
            sys.exit(exc.exit_code)

        hostname, filepath = host_match.hostname, host_match.filepath

        # Resolve credentials
        try:
            credential_result = resolve_credential_for_host(
                hostname, filepath, username
            )
        except ConnectError as exc:
            print(f"Error: {exc}", file=sys.stderr)
            sys.exit(exc.exit_code)

        resolved_username = credential_result.username
        password = credential_result.password

        # Run PTY connector for interactive SSH session
        exit_code = _run_pty_connector(
            hostname=hostname,
            requested_username=username,
            resolved_username=resolved_username,
            password=password,
            ssh_args=ssh_passthrough_args,
        )
        sys.exit(exit_code)


def cli():
    """Entry point used by installed console scripts."""
    main()


if __name__ == "__main__":
    main()
