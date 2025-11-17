from __future__ import annotations

from pathlib import Path

import pytest
from typer import Exit

from nssh.cli.common import selectors


class DummyParser:
    def __init__(self, include_files: list[Path]):
        self._include_files = include_files

    def find_include_files(self):
        return list(self._include_files)


def test_require_fzf_errors_when_missing(monkeypatch):
    monkeypatch.setattr(selectors, "check_fzf", lambda: False)
    with pytest.raises(Exit) as exc:
        selectors.require_fzf()
    assert exc.value.exit_code == 1


def test_select_via_fzf_returns_selection(monkeypatch):
    monkeypatch.setattr(selectors, "check_fzf", lambda: True)
    monkeypatch.setattr(selectors, "fzf_select", lambda options, prompt: options[1])
    result = selectors.select_via_fzf(["a", "b", "c"], "Pick one")
    assert result == "b"


def test_select_via_fzf_handles_cancel(monkeypatch):
    monkeypatch.setattr(selectors, "check_fzf", lambda: True)
    monkeypatch.setattr(selectors, "fzf_select", lambda options, prompt: "")
    with pytest.raises(Exit) as exc:
        selectors.select_via_fzf(["a"], "Pick one")
    assert exc.value.exit_code == 0


def test_select_include_file_with_argument(monkeypatch, tmp_path):
    home = tmp_path / "home"
    ssh_dir = home / ".ssh"
    ssh_dir.mkdir(parents=True)
    target = ssh_dir / "work.conf"
    target.write_text("# test")

    monkeypatch.setattr(Path, "home", lambda: home)

    parser = DummyParser([])
    result = selectors.select_include_file(parser, "work.conf")
    assert result == target


def test_select_include_file_missing_file(monkeypatch, tmp_path):
    home = tmp_path / "home"
    (home / ".ssh").mkdir(parents=True)
    monkeypatch.setattr(Path, "home", lambda: home)

    parser = DummyParser([])
    with pytest.raises(Exit) as exc:
        selectors.select_include_file(parser, "missing.conf")
    assert exc.value.exit_code == 1


def test_select_include_file_requires_include(monkeypatch):
    parser = DummyParser([])
    with pytest.raises(Exit) as exc:
        selectors.select_include_file(parser)
    assert exc.value.exit_code == 1


def test_select_include_file_all_option(monkeypatch, tmp_path):
    files = [tmp_path / "one.conf", tmp_path / "two.conf"]
    for path in files:
        path.write_text("# test")

    parser = DummyParser(files)
    monkeypatch.setattr(selectors, "require_fzf", lambda: None)
    monkeypatch.setattr(selectors, "fzf_select", lambda options, prompt: "[All files]")

    result = selectors.select_include_file(parser, allow_all=True)
    assert result == files


def test_select_include_file_specific_choice(monkeypatch, tmp_path):
    files = [tmp_path / "one.conf", tmp_path / "two.conf"]
    for path in files:
        path.write_text("# test")

    parser = DummyParser(files)
    monkeypatch.setattr(selectors, "require_fzf", lambda: None)
    monkeypatch.setattr(selectors, "fzf_select", lambda options, prompt: str(files[1]))

    result = selectors.select_include_file(parser)
    assert result == files[1]


def test_select_include_file_cancel(monkeypatch, tmp_path):
    files = [tmp_path / "one.conf"]
    for path in files:
        path.write_text("# test")

    parser = DummyParser(files)
    monkeypatch.setattr(selectors, "require_fzf", lambda: None)
    monkeypatch.setattr(selectors, "fzf_select", lambda options, prompt: "")

    with pytest.raises(Exit) as exc:
        selectors.select_include_file(parser)
    assert exc.value.exit_code == 0
