from __future__ import annotations

import os
from contextlib import contextmanager
from pathlib import Path

import pytest

from nssh.core import connect


@pytest.fixture(autouse=True)
def disable_timing(monkeypatch):
    @contextmanager
    def _noop(*_args, **_kwargs):
        yield

    monkeypatch.setattr(connect.timing_core, "stage", _noop)
    monkeypatch.setattr(connect.timing_core, "run_span", _noop)


def test_find_host_match_prefers_index(tmp_path, monkeypatch):
    ssh_dir = tmp_path / ".ssh"
    ssh_dir.mkdir()
    index = ssh_dir / ".nssh_host_index"
    index.write_text("router1|/tmp/work_hosts\n")

    monkeypatch.setattr(connect.Path, "home", lambda: tmp_path)

    match = connect.find_host_match("router1")
    assert match.hostname == "router1"
    assert match.filepath == "/tmp/work_hosts"


def test_find_host_match_uses_partial_match_when_unique(tmp_path, monkeypatch):
    monkeypatch.setattr(connect.Path, "home", lambda: tmp_path)

    config = tmp_path / "config"
    include = tmp_path / "work_hosts"
    config.write_text("")
    include.write_text("")

    class DummyParser:
        def __init__(self):
            self.config_file = config

        def find_include_files(self):
            return [include]

        def parse_ssh_config(self, path):
            hosts = {
                config: ([], [("alpha", ["Host alpha\n"])]),
                include: ([], [("router-main", ["Host router-main\n"])]),
            }
            return hosts[path]

        @staticmethod
        def extract_aliases(host_lines):
            if not host_lines:
                return []
            text = host_lines[0].strip()
            if not text.lower().startswith("host "):
                return []
            return text[5:].strip().split()

    monkeypatch.setattr(connect, "SSHConfigParser", DummyParser)

    match = connect.find_host_match("router")
    assert match.hostname == "router-main"
    assert match.filepath == str(include)


def test_find_host_match_signals_missing_host(tmp_path, monkeypatch, capsys):
    monkeypatch.setattr(connect.Path, "home", lambda: tmp_path)

    class DummyParser:
        def __init__(self):
            self.config_file = tmp_path / "config"

        def find_include_files(self):
            return []

        def parse_ssh_config(self, _path):
            return [], [("alpha", ["Host alpha\n"])]

        @staticmethod
        def extract_aliases(host_lines):
            if not host_lines:
                return []
            text = host_lines[0].strip()
            if not text.lower().startswith("host "):
                return []
            return text[5:].strip().split()

    monkeypatch.setattr(connect, "SSHConfigParser", DummyParser)

    with pytest.raises(connect.NoMatchesError) as exc:
        connect.find_host_match("missing")

    assert "No hosts matching" in str(exc.value)
    assert exc.value.exit_code == 3


def test_find_host_match_signals_multiple_matches(tmp_path, monkeypatch, capsys):
    monkeypatch.setattr(connect.Path, "home", lambda: tmp_path)

    config = tmp_path / "config"
    config.write_text("")

    class DummyParser:
        def __init__(self):
            self.config_file = config

        def find_include_files(self):
            return []

        def parse_ssh_config(self, _path):
            return [], [
                ("router-east", ["Host router-east\n"]),
                ("router-west", ["Host router-west\n"]),
            ]

        @staticmethod
        def extract_aliases(host_lines):
            if not host_lines:
                return []
            text = host_lines[0].strip()
            if not text.lower().startswith("host "):
                return []
            return text[5:].strip().split()

    monkeypatch.setattr(connect, "SSHConfigParser", DummyParser)

    with pytest.raises(connect.MultipleMatchesError) as exc:
        connect.find_host_match("router")

    assert exc.value.exit_code == 2
    assert exc.value.matches["router-east"] == str(config)


def test_resolve_credential_for_host_returns_secret(monkeypatch):
    class DummyManager:
        def resolve_credential(self, *, hostname, git_include_file, username=None):
            assert hostname == "router1"
            assert git_include_file == "work_hosts"
            assert username == "alice"
            return ("alice", "pw")

    monkeypatch.setattr(connect, "CredentialManager", lambda: DummyManager())

    result = connect.resolve_credential_for_host("router1", "/tmp/work_hosts", "alice")
    assert result.username == "alice"
    assert result.password == "pw"


def test_resolve_credential_for_host_exits_when_password_expected(monkeypatch, capsys):
    class DummyManager:
        def resolve_credential(self, **_):
            return None

    class DummyParser:
        def find_host_in_files(self, hostname):  # noqa: ARG002
            return Path("/tmp/work_hosts"), ["Host router1\n"]

    monkeypatch.setattr(connect, "CredentialManager", lambda: DummyManager())
    monkeypatch.setattr(connect, "SSHConfigParser", lambda: DummyParser())
    monkeypatch.setattr(connect, "detect_auth_type", lambda _lines: "password")

    with pytest.raises(connect.CredentialExpectationError) as exc:
        connect.resolve_credential_for_host("router1", "/tmp/work_hosts")
    assert "No credential found" in str(exc.value)


def test_emit_password_token_writes_to_fd(monkeypatch):
    read_fd, write_fd = os.pipe()
    try:
        monkeypatch.setenv("NSSH_PASS_FD", str(write_fd))
        token = connect._emit_password_token("supersecret")
        assert token.startswith(connect.PASSWORD_FD_PREFIX)

        data = os.read(read_fd, 1024).decode().strip()
        assert data == "supersecret"
    finally:
        try:
            os.close(read_fd)
        except OSError:
            pass
