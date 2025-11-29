"""nssh cp - Copy files to/from SSH hosts."""

from __future__ import annotations

import sys
from typing import Sequence, Tuple

from nssh.cli import click
from nssh.cli.common.app import run_cli
from nssh.cli.common.banner import FAIL, OK, banner
from nssh.cli.common.help import (
    OptionRow,
    OptionsPanel,
    UsageRow,
    UsageSection,
    render_usage,
)
from nssh.core.connect import (
    ConnectError,
    MultipleMatchesError,
    find_host_match,
    resolve_credential_for_host,
)
from nssh.core.connector.scp import run_scp
from nssh.core.diag import timing as timing_core
from nssh.core.ui.console import get_console

console = get_console()

APP_TITLE = "nssh cp"
APP_SUBTITLE = "Copy files to/from SSH hosts"


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
        raise click.BadParameter("Cannot copy between two remote hosts")
    if not source_is_remote and not dest_is_remote:
        raise click.BadParameter("One path must be remote (host:path format)")

    if source_is_remote:
        user, host, remote_path = _parse_remote_spec(source)
        return user, host, remote_path, dest, "pull"
    else:
        user, host, remote_path = _parse_remote_spec(dest)
        return user, host, remote_path, source, "push"


@click.command()
@click.argument("source", required=True)
@click.argument("dest", required=True)
@click.option("-r", "--recursive", is_flag=True, default=False, help="Copy directories")
@click.option(
    "-p", "--preserve", is_flag=True, default=False, help="Preserve times/modes"
)
@click.option("-q", "--quiet", is_flag=True, default=False, help="Disable progress")
@click.option("-v", "--verbose", is_flag=True, default=False, help="Verbose output")
@click.argument("scp_args", nargs=-1)
def app(
    source: str,
    dest: str,
    recursive: bool,
    preserve: bool,
    quiet: bool,
    verbose: bool,
    scp_args: Tuple[str, ...],
) -> None:
    """Copy files to/from SSH hosts.

    Direction is auto-detected based on which argument contains ':'

    Examples:
        nssh cp myhost:~/file.txt ./           # pull from remote
        nssh cp ./file.txt myhost:~/           # push to remote
        nssh cp -r myhost:~/dir ./local/       # recursive pull
    """
    with banner("COPY FILES", OK) as set_outcome:
        _app_impl(
            source, dest, recursive, preserve, quiet, verbose, scp_args, set_outcome
        )


def _app_impl(
    source: str,
    dest: str,
    recursive: bool,
    preserve: bool,
    quiet: bool,
    verbose: bool,
    scp_args: Tuple[str, ...],
    set_outcome,
) -> None:
    """Internal implementation for cp command."""
    # Detect direction and parse paths (with cli-startup timing)
    with timing_core.stage("cli-startup", detail="cp"):
        try:
            username, host_search, remote_path, local_path, direction = (
                _detect_direction(source, dest)
            )
        except click.BadParameter:
            raise
        except ValueError as exc:
            raise click.BadParameter(str(exc))

    # Resolve host - require exact match for safety
    try:
        host_match = find_host_match(host_search)
    except MultipleMatchesError as exc:
        console.print(f"[red]Error:[/red] '{host_search}' matches multiple hosts:")
        for match in exc.matches:
            console.print(f"  [dim]- {match}[/dim]")
        set_outcome(FAIL)
        raise SystemExit(1)
    except ConnectError as exc:
        console.print(f"[red]Error:[/red] {exc}")
        set_outcome(FAIL)
        raise SystemExit(exc.exit_code)

    # Require exact hostname match for file transfers
    if host_match.hostname != host_search:
        console.print(
            f"[red]Error:[/red] '{host_search}' is not an exact match. Did you mean:"
        )
        console.print(f"  [dim]- {host_match.hostname}[/dim]")
        set_outcome(FAIL)
        raise SystemExit(1)

    # Resolve credentials
    try:
        creds = resolve_credential_for_host(
            host_match.hostname, host_match.filepath, username
        )
    except ConnectError as exc:
        console.print(f"[red]Error:[/red] {exc}")
        set_outcome(FAIL)
        raise SystemExit(exc.exit_code)

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

    if exit_code != 0:
        set_outcome(FAIL)

    raise SystemExit(exit_code)


def _usage_sections() -> list[UsageSection]:
    return [
        UsageSection(
            "Usage",
            rows=[
                UsageRow(
                    "nssh cp [HOST]:src local",
                    "Pull from remote",
                ),
                UsageRow(
                    "nssh cp local [HOST]:dest",
                    "Push to remote",
                ),
            ],
        ),
    ]


def _options_panel():
    # cp is a single command - manually define concise options
    return OptionsPanel(
        options=[
            OptionRow("--recursive, -r", "Copy directories"),
            OptionRow("--preserve, -p", "Preserve times/modes"),
            OptionRow("--quiet, -q", "Disable progress"),
            OptionRow("--verbose, -v", "Verbose output"),
            OptionRow("+ [SCP_ARGS]", "Pass args to scp"),
        ]
    )


def print_usage() -> None:
    render_usage(
        APP_TITLE,
        APP_SUBTITLE,
        _usage_sections(),
        options_panel=_options_panel(),
        show_banner=False,
    )


def _preprocess_args(argv: Sequence[str] | None) -> list[str]:
    """Convert + separator to -- for Click compatibility."""
    args = list(argv) if argv is not None else sys.argv[1:]
    if "+" in args:
        idx = args.index("+")
        args[idx] = "--"
    return args


def main(argv: Sequence[str] | None = None) -> None:
    """Main entry point."""
    run_cli(
        app,
        cli_name=APP_TITLE,
        usage_cb=print_usage,
        argv=_preprocess_args(argv),
    )


if __name__ == "__main__":
    main()
