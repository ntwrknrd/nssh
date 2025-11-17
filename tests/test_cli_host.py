from __future__ import annotations

import os
from dataclasses import dataclass
from pathlib import Path

import pytest
from typer.testing import CliRunner

from nssh.cli.host import app
import nssh.cli.host.context as host_context


class DummyCredentialManager:
    """In-memory stand-in for CredentialManager used in CLI tests."""

    def __init__(self) -> None:
        self.contexts: dict[str, dict[str, object]] = {}
        self.host_credentials: dict[str, list[dict[str, str]]] = {}

    def get_context(self, include_file: str | None) -> dict | None:
        if not include_file:
            return None
        return self.contexts.get(include_file)

    def add_host_credential(self, hostname: str, username: str, password: str) -> None:
        self.host_credentials.setdefault(hostname, []).append(
            {"username": username, "password": password}
        )

    def get_host_credentials(self, hostname: str) -> list[dict[str, str]] | None:
        return self.host_credentials.get(hostname)

    def delete_host_all_credentials(self, hostname: str) -> None:
        self.host_credentials.pop(hostname, None)

    def set_context(
        self,
        include_file: str,
        *,
        name: str,
        username: str | None = None,
        password: str | None = None,
    ) -> None:
        context: dict[str, object] = {"name": name, "git_include_file": include_file}
        if username:
            context["credential"] = {"username": username, "password": password or ""}
        self.contexts[include_file] = context


@dataclass
class HostCliEnv:
    runner: CliRunner
    env: dict[str, str]
    include_file: Path
    home: Path
    creds: DummyCredentialManager


@pytest.fixture
def host_cli_env(tmp_path, monkeypatch) -> HostCliEnv:
    """Prepare a temporary HOME with ~/.ssh config/include files."""
    home = tmp_path / "home"
    ssh_dir = home / ".ssh"
    ssh_dir.mkdir(parents=True)
    include_file = ssh_dir / "work.conf"
    include_file.write_text("# Managed by tests\n")
    config_path = ssh_dir / "config"
    config_path.write_text("Include work.conf\n")

    backups_dir = ssh_dir / "backups"
    backups_dir.mkdir()

    # Ensure Path.home() and HOME env point to the temp directory.
    monkeypatch.setenv("HOME", str(home))
    monkeypatch.setenv("XDG_CONFIG_HOME", str(home / ".config"))
    monkeypatch.setattr(Path, "home", lambda: home)

    # Redirect backup directory used by SSHConfigParser.
    monkeypatch.setenv("NSSH_BACKUP_DIR", str(backups_dir))

    dummy_creds = DummyCredentialManager()
    monkeypatch.setattr(host_context, "CredentialManager", lambda: dummy_creds)

    env = os.environ.copy()
    env["HOME"] = str(home)
    env["XDG_CONFIG_HOME"] = str(home / ".config")

    return HostCliEnv(
        runner=CliRunner(),
        env=env,
        include_file=include_file,
        home=home,
        creds=dummy_creds,
    )


def test_host_add_list_remove_flow(host_cli_env: HostCliEnv):
    """End-to-end smoke test for add -> list -> rm commands."""
    runner = host_cli_env.runner

    result = runner.invoke(
        app,
        [
            "add",
            "web.example.com",
            "--hostname",
            "web",
            "--user",
            "deploy",
            "--port",
            "2222",
            "--key",
            "--file",
            "work.conf",
            "--no-test",
        ],
        env=host_cli_env.env,
        input="\n\n\n\n",
    )
    assert result.exit_code == 0, result.output

    contents = host_cli_env.include_file.read_text()
    assert "Host web" in contents
    assert "HostName web.example.com" in contents
    assert "Port 2222" in contents

    list_result = runner.invoke(app, ["list"], env=host_cli_env.env)
    assert list_result.exit_code == 0, list_result.output
    assert "web" in list_result.stdout
    assert "SSH Host List" in list_result.stdout

    rm_result = runner.invoke(app, ["rm", "web", "--force"], env=host_cli_env.env)
    assert rm_result.exit_code == 0, rm_result.output
    assert "removed" in rm_result.stdout.lower()
    assert "Host web" not in host_cli_env.include_file.read_text()


def test_host_update_switches_authentication(host_cli_env: HostCliEnv):
    """nssh host update --auth rewrites the config entry."""
    host_cli_env.include_file.write_text(
        "Host api\n"
        "  HostName api.example.com\n"
        "  User deploy\n"
        "  PreferredAuthentications password\n"
        "  PubkeyAuthentication no\n"
    )

    runner = host_cli_env.runner
    result = runner.invoke(
        app,
        ["update", "api", "--auth", "publickey"],
        env=host_cli_env.env,
        input="\n",
    )
    assert result.exit_code == 0, result.output

    updated = host_cli_env.include_file.read_text()
    assert "PubkeyAuthentication yes" in updated
    assert "PasswordAuthentication no" in updated


def test_host_add_password_uses_context_credentials(
    host_cli_env: HostCliEnv, monkeypatch
):
    """When context creds exist, password auth reuses them instead of prompting."""
    host_cli_env.creds.set_context(
        "work.conf", name="work", username="netop", password="ctx-secret"
    )

    monkeypatch.setattr(
        "nssh.cli.common.workflows.select_via_fzf",
        lambda options, prompt: options[0],
    )

    result = host_cli_env.runner.invoke(
        app,
        [
            "add",
            "db.example.com",
            "--hostname",
            "db",
            "--user",
            "netop",
            "--port",
            "2222",
            "--password",
            "--file",
            "work.conf",
            "--no-test",
        ],
        env=host_cli_env.env,
        input="\n\n\n\n",
    )
    assert result.exit_code == 0, result.output
    contents = host_cli_env.include_file.read_text()
    assert "PreferredAuthentications password" in contents
    # Should not store credentials because context supplied them
    assert host_cli_env.creds.get_host_credentials("db") is None


def test_host_add_password_custom_stores_credentials(
    host_cli_env: HostCliEnv, monkeypatch
):
    """Custom password path saves credentials through the manager."""

    monkeypatch.setattr(
        "nssh.cli.common.workflows.select_via_fzf",
        lambda options, prompt: "custom - Custom password (prompt and store)",
    )
    monkeypatch.setattr(
        "nssh.cli.host.add.prompt_password_with_confirmation",
        lambda prompt_text: "s3cret!",
    )

    result = host_cli_env.runner.invoke(
        app,
        [
            "add",
            "cache.example.com",
            "--hostname",
            "cache",
            "--user",
            "deploy",
            "--password",
            "--file",
            "work.conf",
            "--no-test",
        ],
        env=host_cli_env.env,
        input="\n\n\n\n",
    )
    assert result.exit_code == 0, result.output
    stored = host_cli_env.creds.get_host_credentials("cache")
    assert stored and stored[0]["password"] == "s3cret!"


def test_host_update_compat_triggers_auto_fix(host_cli_env: HostCliEnv, monkeypatch):
    """nssh host update --compat invokes the iterative compatibility workflow."""
    host_cli_env.include_file.write_text(
        "Host legacy\n"
        "  HostName legacy.example.com\n"
        "  User ops\n"
        "  PubkeyAuthentication no\n"
        "  PreferredAuthentications password\n"
    )

    calls: dict[str, str] = {}

    def fake_iterative(parser, hostname, max_iterations=5, verbose=True):
        calls["hostname"] = hostname
        return {
            "success": True,
            "iterations": 1,
            "fixes_applied": ["kex"],
            "final_test_result": {
                "exit_code": 0,
                "stderr": "",
                "stdout": "",
                "success": True,
            },
            "stopped_reason": "connection_succeeded",
        }

    monkeypatch.setattr(
        "nssh.cli.host.compat.iterative_compatibility_fix",
        fake_iterative,
    )

    result = host_cli_env.runner.invoke(
        app,
        ["update", "legacy", "--compat"],
        env=host_cli_env.env,
    )
    assert result.exit_code == 0, result.output
    assert (
        "Compatibility fixes applied" in result.stdout or "✓ Success" in result.stdout
    )
    assert calls["hostname"] == "legacy"
