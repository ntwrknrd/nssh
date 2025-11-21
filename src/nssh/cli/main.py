from __future__ import annotations

import os
import sys
from dataclasses import dataclass
from typing import Any, Callable, Dict, Sequence

from nssh import __version__

APP_TITLE = "nssh"
APP_SUBTITLE = "SSH tooling for network operators"

_connection_context = {"allow_extra_args": True, "ignore_unknown_options": True}

TOP_LEVEL_COMMANDS = [
    "host",
    "cred",
    "log",
    "benchmark",
    "self",
    "version",
    "help",
    "__list-subcommands",
]


@dataclass(frozen=True)
class _CliBundle:
    app: Any
    subcommand_usage: Dict[str, Callable[[], None]]


_CLI_BUNDLE: _CliBundle | None = None


def _run_connect(args: Sequence[str]) -> None:
    from nssh.core import connect as connect_module

    connect_module.main(argv=list(args))


def _maybe_handle_subcommand_usage(args: list[str]) -> None:
    if not args:
        return

    command = args[0]
    usage_cb = _get_cli_bundle().subcommand_usage.get(command)
    if usage_cb is None:
        return

    if len(args) == 1:
        usage_cb()
        raise SystemExit(1)

    if any(arg in {"-h", "--help"} for arg in args[1:]):
        usage_cb()
        raise SystemExit(0)


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


def _build_cli_bundle() -> _CliBundle:
    from nssh.cli import typer as _typer

    globals()["typer"] = _typer  # Expose for modules that expect it
    globals()["_typer"] = _typer  # Satisfy postponed annotations referencing _typer
    from nssh.cli.benchmark import (
        app as benchmark_app,
        print_usage as benchmark_print_usage,
    )
    from nssh.cli.self import (
        app as self_app,
        print_usage as self_print_usage,
    )
    from nssh.cli.cred import app as cred_app, print_usage as cred_print_usage
    from nssh.cli.host import app as host_app, print_usage as host_print_usage
    from nssh.cli.log import app as log_app, print_usage as log_print_usage

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

    app.add_typer(host_app, name="host")
    app.add_typer(cred_app, name="cred")
    app.add_typer(log_app, name="log")
    app.add_typer(benchmark_app, name="benchmark")
    app.add_typer(self_app, name="self")

    @app.command("__list-subcommands", hidden=True)
    def list_subcommands_command() -> None:
        _typer.echo("\n".join(TOP_LEVEL_COMMANDS))

    subcommand_usage = {
        "host": host_print_usage,
        "cred": cred_print_usage,
        "log": log_print_usage,
        "benchmark": benchmark_print_usage,
        "self": self_print_usage,
    }

    return _CliBundle(app=app, subcommand_usage=subcommand_usage)


def _get_cli_bundle() -> _CliBundle:
    global _CLI_BUNDLE
    if _CLI_BUNDLE is None:
        _CLI_BUNDLE = _build_cli_bundle()
    return _CLI_BUNDLE


def main(argv: Sequence[str] | None = None) -> None:
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

    # Handle completion requests
    if (
        os.getenv("_NSSH_COMPLETE")
        or os.getenv("_TYPER_COMPLETE")
        or os.getenv("_TYPER_COMPLETE_FISH_ACTION")
    ):
        # Explicitly set program name for Typer completion
        _get_cli_bundle().app(prog_name="nssh")
        return

    # Handle subcommand usage
    if args:
        _maybe_handle_subcommand_usage(args)
    if not args:
        print_usage()
        raise SystemExit(1)

    bundle = _get_cli_bundle()
    from nssh.cli.common.app import run_cli as _run_cli

    _run_cli(
        bundle.app,
        cli_name=APP_TITLE,
        usage_cb=print_usage,
        completion_prefix="NSSH",
        argv=args,
        show_usage_if_no_args=False,
    )


if __name__ == "__main__":
    main()
