"""Typer wiring for the recording plan helper."""

from __future__ import annotations

from typing import Sequence

from nssh.cli import typer
from nssh.cli.common.app import run_cli
from nssh.cli.common.help import UsageRow, UsageSection, render_usage
from nssh.core.recording import check as check_core

APP_TITLE = "nssh recording-check"
APP_SUBTITLE = "Inspect wrapper recording plans"

app = typer.Typer(
    add_help_option=False,
    invoke_without_command=True,
    rich_markup_mode=None,
)


@app.callback()
def invoke_plan(
    hostname: str = typer.Argument(
        "check-host",
        help="Hostname (or placeholder) to query recording state for",
    )
) -> None:
    check_core.main(argv=[hostname])


def _usage_sections() -> list[UsageSection]:
    return [
        UsageSection(
            "Usage",
            rows=[
                UsageRow(
                    "nssh recording-check [HOSTNAME]",
                    "Print key=value plan describing wrapper recording behavior",
                )
            ],
        )
    ]


def print_usage() -> None:
    render_usage(APP_TITLE, APP_SUBTITLE, _usage_sections())


def main(argv: Sequence[str] | None = None) -> None:
    run_cli(
        app,
        cli_name=APP_TITLE,
        usage_cb=print_usage,
        completion_prefix="RECORDING_CHECK",
        argv=argv,
        show_usage_if_no_args=False,
    )


def cli_main() -> None:  # pragma: no cover - convenience shim
    main()


if __name__ == "__main__":  # pragma: no cover
    cli_main()
