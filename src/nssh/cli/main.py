from __future__ import annotations

import sys
from typing import Sequence

from nssh import __version__

APP_TITLE = "nssh"
APP_SUBTITLE = "SSH tooling for network operators"

# Mapping of subcommand names to their module paths for lazy loading
_SUBCOMMAND_MODULES = {
    "host": "nssh.cli.host",
    "ctx": "nssh.cli.ctx",
    "log": "nssh.cli.log",
    "benchmark": "nssh.cli.benchmark",
    "self": "nssh.cli.self",
    "cp": "nssh.cli.cp",
}

# Nested subcommands for each top-level command (used to disambiguate from hostnames)
_NESTED_SUBCOMMANDS: dict[str, set[str]] = {
    "host": {"add", "edit", "get", "list", "rm", "sort"},
    "ctx": {"add", "edit", "get", "list", "rm"},
    "log": {"list", "play", "delete", "upload", "export"},
    "benchmark": {"ssh", "scp"},
    "self": {"init", "status", "reinstall", "uninstall"},
    # cp has no nested subcommands - it takes positional args
}

TOP_LEVEL_COMMANDS = [
    "host",
    "ctx",
    "log",
    "benchmark",
    "self",
    "cp",
    "version",
    "help",
    "__list-subcommands",
]


def _run_connect(args: Sequence[str]) -> None:
    from nssh.core import connect as connect_module

    connect_module.main(argv=list(args))


def _looks_like_subcommand_invocation(command: str, sub_args: list[str]) -> bool:
    """Check if args indicate a CLI subcommand invocation vs a host connection.

    Returns True if this looks like a subcommand call (no args, nested subcommand, or flags).
    Returns False if this looks like a host connection.
    """
    if not sub_args:
        # No args after command name -> subcommand (show help)
        return True

    first_sub = sub_args[0]

    if first_sub.startswith("-"):
        # Flag like --help, -s, etc -> subcommand invocation
        return True

    # Check if it's a known nested subcommand
    nested = _NESTED_SUBCOMMANDS.get(command, set())
    if first_sub in nested:
        return True

    # For 'cp', any non-flag arg is a positional (source/dest path)
    if command == "cp":
        return True

    # Unknown arg after subcommand name -> treat as hostname
    # (e.g., nssh host somearg -> connect to "host" with "somearg" passed to ssh)
    return False


def _run_subcommand(command: str, args: Sequence[str]) -> None:
    """Lazily import and run a subcommand's main function."""
    import importlib

    module_name = _SUBCOMMAND_MODULES[command]
    module = importlib.import_module(module_name)
    module.main(args)


def _usage_sections():
    from nssh.cli.common.help import UsageRow, UsageSection

    return [
        UsageSection(
            "Usage",
            rows=[
                UsageRow("nssh [USER@]HOST", "Connect to host"),
                UsageRow("nssh [bold]cp[/bold]", "Copy files to/from hosts"),
                UsageRow("nssh [bold]host[/bold]", "Manage hosts and credentials"),
                UsageRow("nssh [bold]ctx[/bold]", "Manage credential contexts"),
                UsageRow("nssh [bold]log[/bold]", "Manage recordings"),
                UsageRow("nssh [bold]benchmark[/bold]", "Performance benchmarking"),
                UsageRow("nssh [bold]self[/bold]", "Manage nssh installation"),
            ],
        ),
    ]


def _options_panel():
    from nssh.cli.common.help import OptionRow, OptionsPanel

    # Main CLI has special syntax options - no Click commands to introspect
    return OptionsPanel(
        options=[
            OptionRow("--help, -h", "Show this help message"),
            OptionRow("--version, -v", "Show version number"),
            OptionRow("+ [SSH_ARGS]", "Pass additional arguments to ssh"),
        ]
    )


def print_usage() -> None:
    from nssh.cli.common.help import render_usage

    render_usage(
        APP_TITLE,
        APP_SUBTITLE,
        _usage_sections(),
        options_panel=_options_panel(),
        show_banner=False,
    )


def _build_completion_app():
    """Build full Click app for shell completion only."""
    import importlib

    from nssh.cli import click as _click

    @_click.group()
    def app():
        pass

    @app.command("help")
    def help_command() -> None:
        print_usage()
        raise SystemExit(0)

    @app.command("version", hidden=True)
    def version_command() -> None:
        _click.echo(f"nssh {__version__}")
        raise SystemExit(0)

    @app.command("__list-subcommands", hidden=True)
    def list_subcommands_command() -> None:
        _click.echo("\n".join(TOP_LEVEL_COMMANDS))

    # Add all subcommand apps for completion
    for name, module_path in _SUBCOMMAND_MODULES.items():
        module = importlib.import_module(module_path)
        app.add_command(module.app, name=name)

    return app


def main(argv: Sequence[str] | None = None) -> None:
    import os

    args = list(argv if argv is not None else sys.argv[1:])

    # Handle completion requests (requires full app)
    # Click uses _{PROG_NAME}_COMPLETE env var
    if os.getenv("_NSSH_COMPLETE"):
        _build_completion_app()(prog_name="nssh")
        return

    # No args - show usage
    if not args:
        print_usage()
        raise SystemExit(1)

    command = args[0]
    sub_args = args[1:]

    # Handle explicit hostname marker first: nssh -- hostname [+ ssh_args...]
    if command == "--":
        if not sub_args:
            print_usage()
            raise SystemExit(1)
        _run_connect(sub_args)
        return

    # Handle flags
    if command in {"help", "-h", "--help"}:
        print_usage()
        raise SystemExit(0)
    elif command in {"version", "-v", "--version"}:
        print(f"nssh {__version__}")
        raise SystemExit(0)
    elif command == "__list-subcommands":
        print("\n".join(TOP_LEVEL_COMMANDS))
        raise SystemExit(0)

    # Check if this looks like a subcommand invocation
    if command in _SUBCOMMAND_MODULES and _looks_like_subcommand_invocation(
        command, sub_args
    ):
        _run_subcommand(command, sub_args)
        return

    # Otherwise treat as host connection
    _run_connect(args)


if __name__ == "__main__":
    main()
