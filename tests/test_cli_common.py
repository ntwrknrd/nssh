from __future__ import annotations

from pathlib import Path

import pytest

from nssh.cli.common import selectors


class DummyParser:
    def __init__(self, include_files: list[Path]):
        self._include_files = include_files

    def find_include_files(self):
        return list(self._include_files)


def test_require_fzf_errors_when_missing(monkeypatch):
    monkeypatch.setattr(selectors, "check_fzf", lambda: False)
    with pytest.raises(SystemExit) as exc:
        selectors.require_fzf()
    assert exc.value.code == 1


def test_fzf_select_returns_selection(monkeypatch):
    monkeypatch.setattr(selectors, "check_fzf", lambda: True)
    monkeypatch.setattr(
        selectors, "_fzf_select_single", lambda options, prompt, query=None: options[1]
    )
    result = selectors.fzf_select(["a", "b", "c"], "Pick one")
    assert result == ["b"]


def test_fzf_select_multi_returns_selections(monkeypatch):
    monkeypatch.setattr(selectors, "check_fzf", lambda: True)
    monkeypatch.setattr(
        selectors,
        "_fzf_select_multi",
        lambda options, prompt, query=None: [options[0], options[2]],
    )
    result = selectors.fzf_select(["a", "b", "c"], "Pick some", multi=True)
    assert result == ["a", "c"]


def test_fzf_select_handles_cancel_with_exit(monkeypatch):
    monkeypatch.setattr(selectors, "check_fzf", lambda: True)
    monkeypatch.setattr(
        selectors, "_fzf_select_single", lambda options, prompt, query=None: ""
    )
    with pytest.raises(SystemExit) as exc:
        selectors.fzf_select(["a"], "Pick one")
    assert exc.value.code == 0


def test_fzf_select_handles_cancel_with_exception(monkeypatch):
    monkeypatch.setattr(selectors, "check_fzf", lambda: True)
    monkeypatch.setattr(
        selectors, "_fzf_select_single", lambda options, prompt, query=None: ""
    )
    with pytest.raises(selectors.FzfCancelled):
        selectors.fzf_select(["a"], "Pick one", exit_on_cancel=False)


def test_select_include_file_with_argument(monkeypatch, tmp_path):
    home = tmp_path / "home"
    ssh_dir = home / ".ssh"
    conf_d = ssh_dir / "conf.d"
    conf_d.mkdir(parents=True)
    target = conf_d / "work.conf"
    target.write_text("# test")

    monkeypatch.setattr(Path, "home", lambda: home)

    # Mock CredentialManager to return context with git_include_file
    class MockCredentialManager:
        def list_contexts(self):
            return [{"name": "work", "git_include_file": "work.conf"}]

    import nssh.cli.common.selectors as sel_module

    monkeypatch.setattr(sel_module, "CredentialManager", MockCredentialManager)
    monkeypatch.setattr(sel_module, "ssh_include_dir", lambda: conf_d)

    parser = DummyParser([])
    result = selectors.select_include_file(parser, "work")
    assert result == target


def test_select_include_file_missing_file(monkeypatch, tmp_path):
    home = tmp_path / "home"
    (home / ".ssh").mkdir(parents=True)
    monkeypatch.setattr(Path, "home", lambda: home)

    # Mock CredentialManager to return empty contexts
    class MockCredentialManager:
        def list_contexts(self):
            return []

    import nssh.cli.common.selectors as sel_module

    monkeypatch.setattr(sel_module, "CredentialManager", MockCredentialManager)

    parser = DummyParser([])
    with pytest.raises(SystemExit) as exc:
        selectors.select_include_file(parser, "missing")
    assert exc.value.code == 1


def test_select_include_file_requires_include(monkeypatch):
    parser = DummyParser([])
    monkeypatch.setattr(selectors, "check_fzf", lambda: True)
    monkeypatch.setattr(
        selectors, "_fzf_select_single", lambda options, prompt, query=None: ""
    )
    with pytest.raises(SystemExit) as exc:
        selectors.select_include_file(parser)
    # When no files exist, user can still create new context, so cancel returns 0
    assert exc.value.code == 0


def test_select_include_file_all_option(monkeypatch, tmp_path):
    files = [tmp_path / "one.conf", tmp_path / "two.conf"]
    for path in files:
        path.write_text("# test")

    parser = DummyParser(files)
    monkeypatch.setattr(selectors, "check_fzf", lambda: True)
    monkeypatch.setattr(
        selectors,
        "_fzf_select_single",
        lambda options, prompt, query=None: "[All files]",
    )

    result = selectors.select_include_file(parser, allow_all=True)
    assert result == files


def test_select_include_file_specific_choice(monkeypatch, tmp_path):
    files = [tmp_path / "one.conf", tmp_path / "two.conf"]
    for path in files:
        path.write_text("# test")

    parser = DummyParser(files)
    monkeypatch.setattr(selectors, "check_fzf", lambda: True)
    monkeypatch.setattr(
        selectors, "_fzf_select_single", lambda options, prompt, query=None: "two.conf"
    )

    result = selectors.select_include_file(parser)
    assert result == files[1]


def test_select_include_file_cancel(monkeypatch, tmp_path):
    files = [tmp_path / "one.conf"]
    for path in files:
        path.write_text("# test")

    parser = DummyParser(files)
    monkeypatch.setattr(selectors, "check_fzf", lambda: True)
    monkeypatch.setattr(
        selectors, "_fzf_select_single", lambda options, prompt, query=None: ""
    )

    with pytest.raises(SystemExit) as exc:
        selectors.select_include_file(parser)
    assert exc.value.code == 0
