from __future__ import annotations

import importlib
import sys
import types
from types import ModuleType
from typing import Dict, Generator, List, Tuple

import pytest
from click.testing import CliRunner

import nssh.core.connect as connect_module
import nssh.cli.main as main_module
from nssh.cli import click

StubbedCli = Tuple[CliRunner, ModuleType, List[List[str]]]


def _build_stub_app(commands: Dict[str, str]) -> click.Group:
    """Build a stub Click group with the given commands."""

    @click.group()
    def app():
        pass

    for name, label in commands.items():

        @app.command(name=name)
        def _cmd(label: str = label) -> None:
            click.echo(label)

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
        stub_app = _build_stub_app(commands)
        stub_module.app = stub_app  # type: ignore[attr-defined]

        def _usage(label: str = usage_label) -> None:
            click.echo(label)

        stub_module.print_usage = _usage  # type: ignore[attr-defined]

        # Add main() for lazy dispatch
        def _main(argv=None, app=stub_app) -> None:
            from nssh.cli.common.app import run_cli

            run_cli(
                app,
                cli_name="stub",
                usage_cb=lambda: None,
                argv=argv,
            )

        stub_module.main = _main  # type: ignore[attr-defined]
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
    _, main, _ = stubbed_cli
    # Uses lazy dispatch to stub module's main()
    main.main(["host", "list"])


def test_cred_add_command(stubbed_cli: StubbedCli) -> None:
    _, main, _ = stubbed_cli
    main.main(["cred", "add"])


def test_log_list_command(stubbed_cli: StubbedCli) -> None:
    _, main, _ = stubbed_cli
    main.main(["log", "list"])


def test_bare_connect_arguments(stubbed_cli: StubbedCli) -> None:
    _, main, calls = stubbed_cli
    main.main(["myrouter"])
    assert calls[-1] == ["myrouter"]


def test_bare_connect_passes_options(stubbed_cli: StubbedCli) -> None:
    _, main, calls = stubbed_cli
    main.main(["myrouter", "-p", "2200"])
    assert calls[-1] == ["myrouter", "-p", "2200"]


def test_subcommand_name_without_args_shows_help(stubbed_cli: StubbedCli) -> None:
    """When a subcommand name is used alone, show subcommand help."""
    _, main, calls = stubbed_cli
    # 'nssh host' with no args should invoke the host subcommand (which shows help)
    # This will raise SystemExit(1) from run_cli when no args are passed
    with pytest.raises(SystemExit):
        main.main(["host"])
    # connect should NOT have been called
    assert calls == []


def test_subcommand_name_with_explicit_hostname_marker(stubbed_cli: StubbedCli) -> None:
    """nssh -- host should connect to 'host' (explicit hostname marker)."""
    _, main, calls = stubbed_cli
    main.main(["--", "host"])
    assert calls[-1] == ["host"]


def test_subcommand_name_with_ssh_args(stubbed_cli: StubbedCli) -> None:
    """nssh -- host + -v passes args to connect (connect.py handles + splitting)."""
    _, main, calls = stubbed_cli
    main.main(["--", "host", "+", "-v"])
    # main.py passes all args to connect; connect.py's _split_extra_args handles +
    assert calls[-1] == ["host", "+", "-v"]


def test_subcommand_help_flag_runs_subcommand(stubbed_cli: StubbedCli) -> None:
    """nssh host -h should run the host subcommand (which handles -h)."""
    _, main, _ = stubbed_cli
    # -h is a flag, so it's treated as subcommand invocation
    with pytest.raises(SystemExit) as exc:
        main.main(["host", "-h"])
    assert exc.value.code == 0


def test_top_level_command_list_matches_subcommands() -> None:
    # Verify TOP_LEVEL_COMMANDS includes all lazy-loaded subcommands plus static commands
    expected_subcommands = set(main_module._SUBCOMMAND_MODULES.keys())
    static_commands = {"version", "help", "__list-subcommands"}
    expected = expected_subcommands | static_commands
    assert set(main_module.TOP_LEVEL_COMMANDS) == expected
