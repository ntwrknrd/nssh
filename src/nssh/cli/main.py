from __future__ import annotations

import sys
from typing import Sequence

from nssh import __version__

APP_TITLE = "nssh"
APP_SUBTITLE = "SSH tooling for network operators"

# Mapping of subcommand names to their module paths for lazy loading
_SUBCOMMAND_MODULES = {
    "host": "nssh.cli.host",
    "cred": "nssh.cli.cred",
    "log": "nssh.cli.log",
    "benchmark": "nssh.cli.benchmark",
    "self": "nssh.cli.self",
    "cp": "nssh.cli.cp",
}

TOP_LEVEL_COMMANDS = [
    "host",
    "cred",
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


def _get_subcommand_print_usage(command: str):
    """Lazily import and return the print_usage function for a subcommand."""
    import importlib

    module_name = _SUBCOMMAND_MODULES.get(command)
    if module_name is None:
        return None
    module = importlib.import_module(module_name)
    return getattr(module, "print_usage", None)


def _run_subcommand(command: str, args: Sequence[str]) -> None:
    """Lazily import and run a subcommand's main function."""
    import importlib

    module_name = _SUBCOMMAND_MODULES[command]
    module = importlib.import_module(module_name)
    module.main(args)


def _maybe_handle_subcommand_usage(command: str, args: list[str]) -> bool:
    """Handle subcommand help display. Returns True if handled."""
    if command not in _SUBCOMMAND_MODULES:
        return False

    # Show usage if no args or explicit help flag
    if not args or any(arg in {"-h", "--help"} for arg in args):
        usage_cb = _get_subcommand_print_usage(command)
        if usage_cb:
            usage_cb()
            raise SystemExit(0 if args else 1)
    return False


def _usage_sections():
    from nssh.cli.common.help import UsageRow, UsageSection

    return [
        UsageSection(
            "",
            rows=[
                UsageRow(
                    "nssh [USER@]HOST [SSH_ARGS...]",
                    "Connect to host",
                    examples=["('nssh -- host' if name == subcommand)"],
                    example_prefix="",
                ),
                UsageRow(
                    "nssh cp [USER@]HOST:PATH LOCAL",
                    "Copy files to/from SSH hosts",
                ),
                UsageRow("nssh host [subcommand]", "Manage SSH config entries"),
                UsageRow("nssh cred [subcommand]", "Manage encrypted credentials"),
                UsageRow("nssh log [subcommand]", "Manage recordings"),
                UsageRow(
                    "nssh benchmark [subcommand]",
                    "Performance benchmarking and analysis",
                ),
                UsageRow(
                    "nssh self [subcommand]",
                    "Manage CLI and optional shell helpers",
                ),
            ],
        ),
    ]


def print_usage() -> None:
    from nssh.cli.common.help import render_usage

    render_usage(APP_TITLE, APP_SUBTITLE, _usage_sections())


def _build_completion_app():
    """Build full Typer app for shell completion only."""
    import importlib

    from nssh.cli import typer as _typer

    app = _typer.Typer(
        add_help_option=False,
        invoke_without_command=False,
        rich_markup_mode=None,
    )

    @app.command("help")
    def help_command() -> None:
        print_usage()
        raise _typer.Exit(code=0)

    @app.command("version", hidden=True)
    def version_command() -> None:
        _typer.echo(f"nssh {__version__}")
        raise _typer.Exit(code=0)

    @app.command("__list-subcommands", hidden=True)
    def list_subcommands_command() -> None:
        _typer.echo("\n".join(TOP_LEVEL_COMMANDS))

    # Add all subcommand apps for completion
    for name, module_path in _SUBCOMMAND_MODULES.items():
        module = importlib.import_module(module_path)
        app.add_typer(module.app, name=name)

    return app


def main(argv: Sequence[str] | None = None) -> None:
    import os

    args = list(argv if argv is not None else sys.argv[1:])

    # Fast path: detect host connection and bypass CLI module imports
    # This optimization avoids importing typer/rich/click for simple connections
    if args:
        first_arg = args[0]
        if first_arg == "--":
            _run_connect(args[1:])
            return
        # Check if it's a host connection (not a CLI command or flag)
        if not first_arg.startswith("-") and first_arg not in TOP_LEVEL_COMMANDS:
            _run_connect(args)
            return

    # Handle completion requests (requires full app)
    if (
        os.getenv("_NSSH_COMPLETE")
        or os.getenv("_TYPER_COMPLETE")
        or os.getenv("_TYPER_COMPLETE_FISH_ACTION")
    ):
        _build_completion_app()(prog_name="nssh")
        return

    # No args - show usage
    if not args:
        print_usage()
        raise SystemExit(1)

    command = args[0]
    sub_args = args[1:]

    # Handle subcommand with lazy loading
    if command in _SUBCOMMAND_MODULES:
        _maybe_handle_subcommand_usage(command, sub_args)
        _run_subcommand(command, sub_args)
        return

    # Handle top-level commands
    if command in {"help", "-h", "--help"}:
        print_usage()
        raise SystemExit(0)
    elif command in {"version", "-V", "--version"}:
        print(f"nssh {__version__}")
        raise SystemExit(0)
    elif command == "__list-subcommands":
        print("\n".join(TOP_LEVEL_COMMANDS))
        raise SystemExit(0)

    # Unknown command - show usage
    print_usage()
    raise SystemExit(1)


if __name__ == "__main__":
    main()
