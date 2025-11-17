from __future__ import annotations

import os


from nssh.cli.common import app as cli_app


def test_should_handle_completion_detects_typer(monkeypatch):
    monkeypatch.setenv("_TYPER_COMPLETE", "fish_complete")
    assert cli_app._should_handle_completion("host", "nssh host")


def test_should_handle_completion_detects_custom_prefix(monkeypatch):
    monkeypatch.setenv("_NSSH_HOST_COMPLETE", "bash_complete")
    assert cli_app._should_handle_completion("host", "nssh host")


def test_should_handle_completion_detects_cli_name(monkeypatch):
    monkeypatch.setenv("_DEMO_TOOL_COMPLETE", "zsh_source")
    assert cli_app._should_handle_completion("", "demo-tool")


def test_should_handle_completion_false_when_env_clear(monkeypatch):
    for key in list(os.environ):
        if key.endswith("_COMPLETE") or key == "_TYPER_COMPLETE":
            monkeypatch.delenv(key, raising=False)
    assert not cli_app._should_handle_completion("host", "nssh host")
