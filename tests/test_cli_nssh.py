from __future__ import annotations

import importlib
import sys
import types
from types import ModuleType
from typing import Dict, Generator, List, Tuple

import pytest
from typer.testing import CliRunner

import nssh.core.connect as connect_module
import nssh.cli.main as main_module
from nssh.cli import typer

StubbedCli = Tuple[CliRunner, ModuleType, List[List[str]]]


def _build_stub_app(commands: Dict[str, str]) -> typer.Typer:
    app = typer.Typer()
    for name, label in commands.items():

        @app.command(name=name)
        def _cmd(label: str = label) -> None:
            typer.echo(label)

    return app


@pytest.fixture()
def stubbed_cli(monkeypatch: pytest.MonkeyPatch) -> Generator[StubbedCli, None, None]:
    runner = CliRunner()

    called: List[List[str]] = []

    def fake_connect_main(argv: List[str] | None = None) -> None:
        called.append(list(argv or []))

    monkeypatch.setattr(connect_module, "main", fake_connect_main)

    replacements = {
        "nssh.cli.host": ({"list": "HOST LIST"}, "HOST HELP"),
        "nssh.cli.cred": ({"add": "CRED ADD"}, "CRED HELP"),
        "nssh.cli.log": ({"list": "LOG LIST"}, "LOG HELP"),
    }

    original_modules = {}
    for module_name, (commands, usage_label) in replacements.items():
        original_modules[module_name] = sys.modules.get(module_name)
        stub_module = types.ModuleType(module_name)
        stub_module.app = _build_stub_app(commands)  # type: ignore[attr-defined]

        def _usage(label: str = usage_label) -> None:
            typer.echo(label)

        stub_module.print_usage = _usage  # type: ignore[attr-defined]
        sys.modules[module_name] = stub_module

    reloaded_main = importlib.reload(main_module)
    yield runner, reloaded_main, called
    for module_name, original in original_modules.items():
        if original is not None:
            sys.modules[module_name] = original
        else:
            sys.modules.pop(module_name, None)
    importlib.reload(main_module)


def test_host_list_command(stubbed_cli: StubbedCli) -> None:
    runner, main, _ = stubbed_cli
    app = main._get_cli_bundle().app
    result = runner.invoke(app, ["host", "list"])
    assert result.exit_code == 0


def test_cred_add_command(stubbed_cli: StubbedCli) -> None:
    runner, main, _ = stubbed_cli
    app = main._get_cli_bundle().app
    result = runner.invoke(app, ["cred", "add"])
    assert result.exit_code == 0


def test_log_list_command(stubbed_cli: StubbedCli) -> None:
    runner, main, _ = stubbed_cli
    app = main._get_cli_bundle().app
    result = runner.invoke(app, ["log", "list"])
    assert result.exit_code == 0


def test_bare_connect_arguments(stubbed_cli: StubbedCli) -> None:
    _, main, calls = stubbed_cli
    main.main(["myrouter"])
    assert calls[-1] == ["myrouter"]


def test_bare_connect_passes_options(stubbed_cli: StubbedCli) -> None:
    _, main, calls = stubbed_cli
    main.main(["myrouter", "-p", "2200"])
    assert calls[-1] == ["myrouter", "-p", "2200"]


def test_escape_hatch_allows_reserved_names(stubbed_cli: StubbedCli) -> None:
    _, main, calls = stubbed_cli
    main.main(["--", "host"])
    assert calls[-1] == ["host"]


def test_subcommand_help_short_flag(
    stubbed_cli: StubbedCli, capsys: pytest.CaptureFixture[str]
) -> None:
    _, main, _ = stubbed_cli
    with pytest.raises(SystemExit) as exc:
        main.main(["host", "-h"])
    assert exc.value.code == 0
    captured = capsys.readouterr()
    assert "HOST HELP" in captured.out


def test_subcommand_without_args_shows_usage(
    stubbed_cli: StubbedCli, capsys: pytest.CaptureFixture[str]
) -> None:
    _, main, _ = stubbed_cli
    with pytest.raises(SystemExit) as exc:
        main.main(["host"])
    assert exc.value.code == 1
    captured = capsys.readouterr()
    assert "HOST HELP" in captured.out


def test_top_level_command_list_matches_cli() -> None:
    bundle = main_module._get_cli_bundle()
    command_names = {cmd.name for cmd in bundle.app.registered_commands if cmd.name}
    group_names = {
        group.name
        for group in getattr(bundle.app, "registered_groups", [])
        if group.name
    }
    actual = command_names | group_names
    assert set(main_module.TOP_LEVEL_COMMANDS) == actual
