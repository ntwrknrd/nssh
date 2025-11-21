#!/usr/bin/env python3
"""Self CLI package wiring for installing nssh and optional shell helpers."""

from __future__ import annotations

from typing import Sequence

from nssh.cli import typer
from nssh.cli.common.app import run_cli
from nssh.cli.common.help import UsageRow, UsageSection, render_usage
from nssh import __version__

from .cleanup import cleanup_command
from .install import install_command
from .reinstall import reinstall_command
from .status import status_command

APP_TITLE = "nssh self"
APP_SUBTITLE = "Install nssh CLI + optional shell helpers"

app = typer.Typer(add_help_option=False, rich_markup_mode=None)

app.command("install")(install_command)
app.command("cleanup")(cleanup_command)
app.command("status")(status_command)
app.command("reinstall")(reinstall_command)


@app.command("version")
def version_command() -> None:
    typer.echo(f"nssh {__version__}")


def _usage_sections() -> list[UsageSection]:
    return [
        UsageSection(
            "Commands",
            rows=[
                UsageRow(
                    "nssh self install [OPTIONS]",
                    "Install CLI and optional shell helpers",
                ),
                UsageRow(
                    "nssh self cleanup [OPTIONS]",
                    "Remove files installed by self",
                ),
                UsageRow(
                    "nssh self status",
                    "Show installation status and discover existing files",
                ),
                UsageRow(
                    "nssh self reinstall [OPTIONS]",
                    "Clear cache, reinstall package, refresh files",
                ),
                UsageRow(
                    "nssh self version",
                    "Show installed CLI version",
                ),
            ],
        ),
        UsageSection(
            "Options",
            rows=[
                UsageRow(
                    "--install-shell-helpers",
                    "Install optional bash/zsh/fish wrapper functions",
                ),
                UsageRow(
                    "--install-fish-completions",
                    "Install fish completion files",
                ),
                UsageRow(
                    "--append-shell-snippet PATH",
                    "Append sourcing snippet to shell rc/profile",
                ),
                UsageRow(
                    "--symlink",
                    "Symlink helpers instead of copying (default: copy)",
                ),
                UsageRow(
                    "--dry-run",
                    "Preview install actions without writing (default: write)",
                ),
                UsageRow(
                    "--skip-uv",
                    "Skip uv reinstall; only refresh managed files",
                ),
                UsageRow(
                    "--dev",
                    "Auto-bump pyproject patch version before reinstall",
                ),
                UsageRow("--force, -f", "Overwrite without prompting"),
            ],
        ),
    ]


def print_usage():
    """Print usage information"""
    render_usage(APP_TITLE, APP_SUBTITLE, _usage_sections())


def main(argv: Sequence[str] | None = None) -> None:
    run_cli(
        app,
        cli_name=APP_TITLE,
        usage_cb=print_usage,
        completion_prefix="SELF",
        argv=argv,
        show_usage_if_no_args=False,
    )


def cli_main() -> None:
    """Entry point for python -m usage."""
    main()


if __name__ == "__main__":  # pragma: no cover
    cli_main()
