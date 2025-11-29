from __future__ import annotations

from pathlib import Path
from typing import Any

import pytest

from nssh.core.ssh.config import SSHConfigParser
import nssh.core.ssh.fixer as fixer
from nssh.core.ssh.fixer import parse_ssh_compatibility_error


PASSWORD_HOST = """Host legacy
  HostName legacy.example.com
  User ops
  PubkeyAuthentication no
  PreferredAuthentications password
"""


PUBLICKEY_HOST = """Host legacy
  HostName legacy.example.com
  User ops
  PubkeyAuthentication yes
  PasswordAuthentication no
"""


def _prepare_ssh_config(tmp_path, monkeypatch, payload: str) -> Path:
    home = tmp_path / "home"
    ssh_dir = home / ".ssh"
    ssh_dir.mkdir(parents=True)
    include_file = ssh_dir / "work.conf"
    include_file.write_text(payload)
    config_path = ssh_dir / "config"
    config_path.write_text("Include work.conf\n")

    monkeypatch.setenv("HOME", str(home))
    monkeypatch.setenv("XDG_CONFIG_HOME", str(home / ".config"))
    monkeypatch.setattr(Path, "home", lambda: home)

    return include_file


def test_test_connection_uses_stored_credentials(tmp_path, monkeypatch) -> None:
    _prepare_ssh_config(tmp_path, monkeypatch, PASSWORD_HOST)

    class DummyManager:
        def resolve_credential(self, hostname, git_include_file=None, username=None):
            assert hostname == "legacy"
            assert git_include_file == "work.conf"
            assert username == "ops"
            return ("ops", "s3cret")

    events: dict[str, Any] = {}

    class DummyConnector:
        def __init__(
            self,
            *,
            hostname,
            username,
            password,
            ssh_args,
            stdout,
            attach_stdin,
            **_,
        ):
            events["connector"] = {
                "hostname": hostname,
                "username": username,
                "password": password,
                "ssh_args": list(ssh_args),
                "attach_stdin": attach_stdin,
            }
            self._stdout = stdout

        def run(self):
            self._stdout.write(b"SSH OK\n")
            return 0

    monkeypatch.setattr(fixer, "CredentialManager", lambda: DummyManager())
    monkeypatch.setattr(fixer, "PtyConnector", DummyConnector)

    parser = SSHConfigParser()
    result = fixer.test_ssh_connection_via_cli("legacy", timeout=7, parser=parser)

    assert result["success"] is True
    assert result["exit_code"] == 0
    assert result["stderr"].strip() == "SSH OK"
    assert events["connector"]["password"] == "s3cret"
    assert events["connector"]["attach_stdin"] is False
    assert events["connector"]["ssh_args"][0] == "-vv"
    assert "BatchMode=yes" not in " ".join(events["connector"]["ssh_args"])
    assert any(
        "PreferredAuthentications" in arg for arg in events["connector"]["ssh_args"]
    )


def test_test_connection_errors_without_credentials(tmp_path, monkeypatch) -> None:
    _prepare_ssh_config(tmp_path, monkeypatch, PASSWORD_HOST)

    class DummyManager:
        def resolve_credential(self, *args, **kwargs):
            return None

    monkeypatch.setattr(fixer, "CredentialManager", lambda: DummyManager())

    events: dict[str, Any] = {}

    class BatchConnector:
        def __init__(self, *, ssh_args, stdout, **_):
            events["ssh_args"] = list(ssh_args)
            self._stdout = stdout

        def run(self):
            self._stdout.write(b"Permission denied (password).")
            return 255

    monkeypatch.setattr(fixer, "PtyConnector", BatchConnector)

    parser = SSHConfigParser()
    result = fixer.test_ssh_connection_via_cli("legacy", parser=parser)

    assert result["success"] is False
    assert result["exit_code"] == 255
    assert result["stderr"].splitlines()[0].startswith("[nssh] No stored credential")
    assert "BatchMode=yes" in " ".join(events["ssh_args"])


def test_test_connection_maps_timeouts(tmp_path, monkeypatch):
    _prepare_ssh_config(tmp_path, monkeypatch, PUBLICKEY_HOST)

    monkeypatch.setattr(
        fixer, "CredentialManager", lambda: pytest.fail("Should not load credentials")
    )

    class TimeoutConnector:
        def __init__(
            self,
            *,
            stdout,
            **_,
        ):
            self._stdout = stdout

        def run(self):
            self._stdout.write(b"Connection timed out during banner exchange")
            return 255

    monkeypatch.setattr(fixer, "PtyConnector", TimeoutConnector)

    parser = SSHConfigParser()
    result = fixer.test_ssh_connection_via_cli("legacy", timeout=3, parser=parser)

    assert result["success"] is False
    assert result["exit_code"] == 124
    assert "timed out" in result["stderr"].lower()
    assert "banner" in result["stdout"].lower()


def test_test_connection_handles_keyboard_interactive_probe(tmp_path, monkeypatch):
    _prepare_ssh_config(tmp_path, monkeypatch, PASSWORD_HOST)

    class DummyManager:
        def resolve_credential(self, hostname, git_include_file=None, username=None):
            return ("ops", "s3cret")

    class CliMismatchConnector:
        def __init__(self, *, stdout, **_):
            self._stdout = stdout

        def run(self):
            self._stdout.write(
                b'Authenticated to legacy.example.com ([10.0.0.1]:22) using "keyboard-interactive".\n'
                b"> exit\n"
                b"% Internal error at line 1\n"
            )
            return 1

    monkeypatch.setattr(fixer, "CredentialManager", lambda: DummyManager())
    monkeypatch.setattr(fixer, "PtyConnector", CliMismatchConnector)

    parser = SSHConfigParser()
    result = fixer.test_ssh_connection_via_cli("legacy", parser=parser)

    assert result["success"] is True
    assert result["auth_method"] == "keyboard-interactive"
    assert result["stderr"].startswith(
        "[nssh] Remote CLI rejected probe command 'exit'; treating connection as successful."
    )


@pytest.mark.parametrize(
    "message,expected",
    [
        ("Unable to negotiate with host: no matching MACs found", "macs"),
        ("no matching cipher found", "ciphers"),
        ("Unable to negotiate 10.0.0.1: no matching host key", "hostkey"),
    ],
)
def test_parse_compatibility_patterns_cover_variants(message, expected):
    errors = parse_ssh_compatibility_error(message)
    assert expected in errors
