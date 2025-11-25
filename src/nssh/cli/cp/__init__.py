"""nssh cp - Copy files to/from SSH hosts."""

from __future__ import annotations

import sys
from typing import List, Optional, Sequence

from nssh.cli import typer
from nssh.cli.common.app import run_cli
from nssh.cli.common.help import UsageRow, UsageSection, render_usage
from nssh.core.connect import (
    ConnectError,
    MultipleMatchesError,
    find_host_match,
    resolve_credential_for_host,
)
from nssh.core.connector.scp import run_scp
from nssh.core.diag import timing as timing_core

APP_TITLE = "nssh cp"
APP_SUBTITLE = "Copy files to/from SSH hosts"

app = typer.Typer(add_help_option=False, rich_markup_mode=None)


def _parse_remote_spec(spec: str) -> tuple[str | None, str, str]:
    """Parse '[user@]host:path' -> (user, host, path)."""
    if ":" not in spec:
        raise ValueError("Not a remote spec")
    host_part, _, path = spec.partition(":")

    # Parse optional user@ prefix
    if "@" in host_part and not host_part.startswith("@"):
        user, _, host = host_part.partition("@")
    else:
        user, host = None, host_part

    return user, host, path


def _detect_direction(source: str, dest: str) -> tuple[str | None, str, str, str, str]:
    """Detect transfer direction from arguments.

    Returns:
        Tuple of (username, host_search, remote_path, local_path, direction)
        where direction is "pull" or "push"
    """
    source_is_remote = ":" in source
    dest_is_remote = ":" in dest

    if source_is_remote and dest_is_remote:
        raise typer.BadParameter("Cannot copy between two remote hosts")
    if not source_is_remote and not dest_is_remote:
        raise typer.BadParameter("One path must be remote (host:path format)")

    if source_is_remote:
        user, host, remote_path = _parse_remote_spec(source)
        return user, host, remote_path, dest, "pull"
    else:
        user, host, remote_path = _parse_remote_spec(dest)
        return user, host, remote_path, source, "push"


@app.callback(invoke_without_command=True)
def cp_command(
    ctx: typer.Context,
    source: str = typer.Argument(..., help="Source path (local or host:path)"),
    dest: str = typer.Argument(..., help="Destination path (local or host:path)"),
    recursive: bool = typer.Option(
        False, "-r", "--recursive", help="Copy directories recursively"
    ),
    preserve: bool = typer.Option(
        False, "-p", "--preserve", help="Preserve modification times and modes"
    ),
    quiet: bool = typer.Option(False, "-q", "--quiet", help="Disable progress meter"),
    verbose: bool = typer.Option(False, "-v", "--verbose", help="Verbose mode"),
    scp_args: Optional[List[str]] = typer.Argument(
        None, help="Additional scp arguments (after --)"
    ),
) -> None:
    """Copy files to/from SSH hosts.

    Direction is auto-detected based on which argument contains ':'

    Examples:
        nssh cp myhost:~/file.txt ./           # pull from remote
        nssh cp ./file.txt myhost:~/           # push to remote
        nssh cp -r myhost:~/dir ./local/       # recursive pull
    """
    # Detect direction and parse paths (with cli-startup timing)
    with timing_core.stage("cli-startup", detail="cp"):
        try:
            username, host_search, remote_path, local_path, direction = (
                _detect_direction(source, dest)
            )
        except typer.BadParameter:
            raise
        except ValueError as exc:
            raise typer.BadParameter(str(exc))

    # Resolve host - require exact match for safety
    try:
        host_match = find_host_match(host_search)
    except MultipleMatchesError as exc:
        print(f"Error: '{host_search}' matches multiple hosts:", file=sys.stderr)
        for match in exc.matches:
            print(f"  - {match}", file=sys.stderr)
        raise typer.Exit(1)
    except ConnectError as exc:
        print(f"Error: {exc}", file=sys.stderr)
        raise typer.Exit(exc.exit_code)

    # Require exact hostname match for file transfers
    if host_match.hostname != host_search:
        print(
            f"Error: '{host_search}' is not an exact match. Did you mean:",
            file=sys.stderr,
        )
        print(f"  - {host_match.hostname}", file=sys.stderr)
        raise typer.Exit(1)

    # Resolve credentials
    try:
        creds = resolve_credential_for_host(
            host_match.hostname, host_match.filepath, username
        )
    except ConnectError as exc:
        print(f"Error: {exc}", file=sys.stderr)
        raise typer.Exit(exc.exit_code)

    # Build scp paths - use username from credential resolution or explicit
    scp_user = username or creds.username
    if scp_user:
        remote_host_spec = f"{scp_user}@{host_match.hostname}"
    else:
        remote_host_spec = host_match.hostname

    if direction == "pull":
        scp_source = f"{remote_host_spec}:{remote_path}"
        scp_dest = local_path
    else:
        scp_source = local_path
        scp_dest = f"{remote_host_spec}:{remote_path}"

    # Build scp args
    args: list[str] = []
    if recursive:
        args.append("-r")
    if preserve:
        args.append("-p")
    if quiet:
        args.append("-q")
    if verbose:
        args.append("-v")
    if scp_args:
        args.extend(scp_args)

    # Run scp
    exit_code = run_scp(
        source=scp_source,
        dest=scp_dest,
        password=creds.password,
        scp_args=args if args else None,
    )

    raise typer.Exit(exit_code)


def _usage_sections() -> list[UsageSection]:
    return [
        UsageSection(
            "Usage",
            rows=[
                UsageRow(
                    "nssh cp [bold][USER@]HOST:PATH[/bold] [bold]LOCAL[/bold]",
                    "Pull file from remote host",
                ),
                UsageRow(
                    "nssh cp [bold]LOCAL[/bold] [bold][USER@]HOST:PATH[/bold]",
                    "Push file to remote host",
                ),
            ],
        ),
        UsageSection(
            "Options",
            rows=[
                UsageRow("-r, --recursive", "Copy directories recursively"),
                UsageRow("-p, --preserve", "Preserve modification times and modes"),
                UsageRow("-q, --quiet", "Disable progress meter"),
                UsageRow("-v, --verbose", "Verbose mode"),
                UsageRow("-- [SCP_ARGS]", "Pass additional arguments to scp"),
            ],
        ),
        UsageSection(
            "Examples",
            rows=[
                UsageRow(
                    "nssh cp myhost:~/config.txt ./",
                    "Pull single file",
                ),
                UsageRow(
                    "nssh cp -r myhost:~/logs ./backup/",
                    "Pull directory recursively",
                ),
                UsageRow(
                    "nssh cp ./script.sh admin@myhost:~/",
                    "Push file as specific user",
                ),
            ],
        ),
    ]


def print_usage() -> None:
    render_usage(APP_TITLE, APP_SUBTITLE, _usage_sections())


def main(argv: Sequence[str] | None = None) -> None:
    """Main entry point."""
    run_cli(
        app,
        cli_name=APP_TITLE,
        usage_cb=print_usage,
        completion_prefix="CP",
        argv=argv,
    )


if __name__ == "__main__":
    main()
