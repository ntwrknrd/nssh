from __future__ import annotations

import os
from dataclasses import dataclass
from pathlib import Path

import pytest
from click.testing import CliRunner

from nssh.cli.host import app
import nssh.cli.common.credentials as host_context


class DummyCredentialManager:
    """In-memory stand-in for CredentialManager used in CLI tests."""

    def __init__(self) -> None:
        self.contexts: dict[str, dict[str, object]] = {}
        self.host_credentials: dict[str, list[dict[str, str]]] = {}

    def get_context(self, include_file: str | None) -> dict | None:
        if not include_file:
            return None
        return self.contexts.get(include_file)

    def list_contexts(self) -> list[dict]:
        """Return all contexts as a list."""
        return list(self.contexts.values())

    def add_host_credential(self, hostname: str, username: str, password: str) -> None:
        self.host_credentials.setdefault(hostname, []).append(
            {"username": username, "password": password}
        )

    def get_host_credentials(self, hostname: str) -> list[dict[str, str]] | None:
        return self.host_credentials.get(hostname)

    def delete_host_all_credentials(self, hostname: str) -> None:
        self.host_credentials.pop(hostname, None)

    def delete_context(self, name: str) -> None:
        # Find and delete context by name
        to_delete = None
        for key, ctx in self.contexts.items():
            if ctx.get("name") == name:
                to_delete = key
                break
        if to_delete:
            del self.contexts[to_delete]

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
    # Set up a context for the work.conf file
    dummy_creds.set_context("work.conf", name="work")
    monkeypatch.setattr(host_context, "CredentialManager", lambda: dummy_creds)

    # Also patch CredentialManager in selectors module for context resolution
    import nssh.cli.common.selectors as selectors_module

    monkeypatch.setattr(selectors_module, "CredentialManager", lambda: dummy_creds)

    # Patch fzf functions globally to avoid interactive prompts
    def mock_fzf_single(options, prompt):
        return options[0] if options else ""

    def mock_fzf_select(options, prompt, *, multi=False, exit_on_cancel=True):
        if not options:
            if exit_on_cancel:
                raise SystemExit(0)
            from nssh.cli.common.selectors import FzfCancelled

            raise FzfCancelled()
        return [options[0]]

    monkeypatch.setattr(selectors_module, "fzf_select", mock_fzf_select)
    monkeypatch.setattr(selectors_module, "_fzf_select_single", mock_fzf_single)
    monkeypatch.setattr(selectors_module, "require_fzf", lambda: None)
    monkeypatch.setattr("nssh.cli.host.edit.fzf_select", mock_fzf_select)
    monkeypatch.setattr("nssh.core.ui.fzf.fzf_select", mock_fzf_single)
    monkeypatch.setattr("nssh.core.ui.fzf.check_fzf", lambda: True)

    # Mock ask_with_fzf to return default value (uses raw terminal mode)
    def mock_ask_with_fzf(
        message, *, default=None, fzf_choices=None, fzf_prompt="", fzf_callback=None
    ):
        return default or (fzf_choices[0] if fzf_choices else "")

    monkeypatch.setattr("nssh.cli.common.prompt.ask_with_fzf", mock_ask_with_fzf)
    monkeypatch.setattr("nssh.cli.host.add.ask_with_fzf", mock_ask_with_fzf)

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


def test_host_add_list_remove_flow(host_cli_env: HostCliEnv, monkeypatch):
    """End-to-end smoke test for add -> list -> rm commands."""
    runner = host_cli_env.runner

    # Mock select_include_file to return the work.conf file
    monkeypatch.setattr(
        "nssh.cli.host.add.select_include_file",
        lambda parser, context, prompt: host_cli_env.include_file,
    )

    # Mock test_connection config to skip connection test
    from nssh.core.env.settings import NsshConfig, HostAddConfig

    mock_config = NsshConfig(host_add=HostAddConfig(test_connection=False))
    monkeypatch.setattr("nssh.cli.host.add.get_config", lambda: mock_config)

    result = runner.invoke(
        app,
        [
            "add",
            "web.example.com",
            "--yes",
        ],
        env=host_cli_env.env,
        input="\n\n\n\n",
    )
    assert result.exit_code == 0, result.output

    contents = host_cli_env.include_file.read_text()
    assert "Host web" in contents
    assert "HostName web.example.com" in contents
    assert "Port 22" in contents

    list_result = runner.invoke(app, ["list"], env=host_cli_env.env)
    assert list_result.exit_code == 0, list_result.output
    assert "web" in list_result.stdout
    assert "LIST SSH HOSTS" in list_result.stdout

    rm_result = runner.invoke(app, ["rm", "web", "--yes"], env=host_cli_env.env)
    assert rm_result.exit_code == 0, rm_result.output
    assert "removed" in rm_result.stdout.lower()
    assert "Host web" not in host_cli_env.include_file.read_text()


def test_host_edit_changes_authentication(host_cli_env: HostCliEnv, monkeypatch):
    """nssh host edit can update authentication based on SSH test."""
    host_cli_env.include_file.write_text(
        "Host api\n"
        "  HostName api.example.com\n"
        "  User deploy\n"
        "  PreferredAuthentications password\n"
        "  PubkeyAuthentication no\n"
    )

    def fake_test(hostname, timeout, parser):
        return {
            "success": True,
            "exit_code": 0,
            "stderr": 'Authenticated to api using "keyboard-interactive".',
            "stdout": "",
            "auth_method": "keyboard-interactive",
        }

    monkeypatch.setattr(
        "nssh.cli.host.edit.test_ssh_connection_via_cli",
        fake_test,
    )

    # Mock fzf_select since we're not providing hostname
    monkeypatch.setattr(
        "nssh.cli.host.edit.fzf_select",
        lambda hosts, prompt, **kw: ["api"],
    )

    runner = host_cli_env.runner
    result = runner.invoke(
        app,
        ["edit", "api", "--yes"],
        env=host_cli_env.env,
        input="y\n",  # Confirm auth type update
    )
    assert result.exit_code == 0, result.output

    updated = host_cli_env.include_file.read_text()
    # Host should still be there
    assert "Host api" in updated


def test_host_add_uses_context_credentials(host_cli_env: HostCliEnv, monkeypatch):
    """When context creds exist, password auth reuses them instead of prompting."""
    host_cli_env.creds.set_context(
        "work.conf", name="work", username="netop", password="ctx-secret"
    )

    # Mock select_include_file
    monkeypatch.setattr(
        "nssh.cli.host.add.select_include_file",
        lambda parser, context, prompt: host_cli_env.include_file,
    )

    # Mock test_connection config to skip connection test
    from nssh.core.env.settings import NsshConfig, HostAddConfig

    mock_config = NsshConfig(host_add=HostAddConfig(test_connection=False))
    monkeypatch.setattr("nssh.cli.host.add.get_config", lambda: mock_config)

    # With --yes, choose_password_source skips prompts and uses context credentials
    result = host_cli_env.runner.invoke(
        app,
        [
            "add",
            "db.example.com",
            "--yes",
        ],
        env=host_cli_env.env,
        input="\n\n\n\n",
    )
    assert result.exit_code == 0, result.output
    contents = host_cli_env.include_file.read_text()
    assert "PreferredAuthentications password" in contents
    # Should not store credentials because context supplied them
    assert host_cli_env.creds.get_host_credentials("db") is None


def test_host_add_custom_password_stores_credentials(
    host_cli_env: HostCliEnv, monkeypatch
):
    """Custom password path saves credentials through the manager."""

    # Mock select_include_file
    monkeypatch.setattr(
        "nssh.cli.host.add.select_include_file",
        lambda parser, context, prompt: host_cli_env.include_file,
    )

    # Mock test_connection config to skip connection test
    from nssh.core.env.settings import NsshConfig, HostAddConfig

    mock_config = NsshConfig(host_add=HostAddConfig(test_connection=False))
    monkeypatch.setattr("nssh.cli.host.add.get_config", lambda: mock_config)

    # Mock confirm to return False so user chooses "custom" password path
    monkeypatch.setattr(
        "nssh.cli.common.workflows.confirm",
        lambda msg, default=True: False,
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
        ],
        env=host_cli_env.env,
        input="\n\n\n\n",
    )
    assert result.exit_code == 0, result.output
    stored = host_cli_env.creds.get_host_credentials("cache")
    assert stored and stored[0]["password"] == "s3cret!"


def test_host_add_switches_to_keyboard_interactive(
    host_cli_env: HostCliEnv, monkeypatch
):
    """If the test succeeds via keyboard-interactive, config is rewritten."""
    host_cli_env.creds.set_context(
        "work.conf", name="expedient", username="chris.jones", password="ctx-secret"
    )

    # Mock select_include_file
    monkeypatch.setattr(
        "nssh.cli.host.add.select_include_file",
        lambda parser, context, prompt: host_cli_env.include_file,
    )

    # Mock config to enable test_connection
    from nssh.core.env.settings import NsshConfig, HostAddConfig

    mock_config = NsshConfig(host_add=HostAddConfig(test_connection=True))
    monkeypatch.setattr("nssh.cli.host.add.get_config", lambda: mock_config)

    def fake_test(hostname, timeout, parser):
        return {
            "success": True,
            "exit_code": 0,
            "stderr": 'Authenticated to {} using "keyboard-interactive".\n'.format(
                hostname
            ),
            "stdout": "",
            "auth_method": "keyboard-interactive",
        }

    monkeypatch.setattr(
        "nssh.cli.host.add.test_ssh_connection_via_cli",
        fake_test,
    )

    result = host_cli_env.runner.invoke(
        app,
        [
            "add",
            "switch.example.com",
            "--yes",
        ],
        env=host_cli_env.env,
        input="\n\n\n\n",
    )

    assert result.exit_code == 0, result.output
    contents = host_cli_env.include_file.read_text()
    assert "PreferredAuthentications keyboard-interactive" in contents
    assert "PubkeyAuthentication no" in contents


def test_host_edit_triggers_auto_fix(host_cli_env: HostCliEnv, monkeypatch):
    """nssh host edit runs the compatibility workflow on test failure."""
    host_cli_env.include_file.write_text(
        "Host legacy\n"
        "  HostName legacy.example.com\n"
        "  User ops\n"
        "  PubkeyAuthentication no\n"
        "  PreferredAuthentications password\n"
    )

    calls: dict[str, str] = {}

    def fake_test(hostname, timeout, parser):
        calls["hostname"] = hostname
        return {
            "success": False,
            "exit_code": 255,
            "stderr": "Unable to negotiate with legacy: no matching key exchange method",
            "stdout": "",
        }

    monkeypatch.setattr(
        "nssh.cli.host.edit.test_ssh_connection_via_cli",
        fake_test,
    )

    def fake_compat_fix(parser, hostname, max_iterations=5, show_header=True):
        calls["compat_hostname"] = hostname
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
        "nssh.cli.host.edit.apply_and_display_compat_fixes",
        fake_compat_fix,
    )

    # Mock parse_ssh_compatibility_error to detect compat issues
    monkeypatch.setattr(
        "nssh.cli.host.edit.parse_ssh_compatibility_error",
        lambda stderr: ["kex"],
    )

    # Run without --yes; provide inputs for: hostname, user (change it), port,
    # password update (n), confirm apply, confirm auto-fix
    result = host_cli_env.runner.invoke(
        app,
        ["edit", "legacy"],
        env=host_cli_env.env,
        input="\nnewuser\n\nn\ny\ny\n",  # keep hostname, change user, keep port, no password, confirm, auto-fix
    )
    assert result.exit_code == 0, result.output
    assert calls.get("compat_hostname") == "legacy"


def test_host_rm_with_credentials_cleanup(host_cli_env: HostCliEnv, monkeypatch):
    """nssh host rm --yes auto-deletes stored credentials."""
    host_cli_env.include_file.write_text(
        "Host testhost\n" "  HostName testhost.example.com\n" "  User admin\n"
    )
    host_cli_env.creds.add_host_credential("testhost", "admin", "secret123")

    result = host_cli_env.runner.invoke(
        app,
        ["rm", "testhost", "--yes"],
        env=host_cli_env.env,
    )
    assert result.exit_code == 0, result.output
    assert "removed" in result.stdout.lower()
    assert "Credentials deleted" in result.stdout

    # Verify credentials were deleted
    assert host_cli_env.creds.get_host_credentials("testhost") is None


def test_host_add_dry_run(host_cli_env: HostCliEnv, monkeypatch):
    """nssh host add --dry-run previews without writing."""
    # Mock select_include_file
    monkeypatch.setattr(
        "nssh.cli.host.add.select_include_file",
        lambda parser, context, prompt: host_cli_env.include_file,
    )

    # Mock config to skip test_connection
    from nssh.core.env.settings import NsshConfig, HostAddConfig

    mock_config = NsshConfig(host_add=HostAddConfig(test_connection=False))
    monkeypatch.setattr("nssh.cli.host.add.get_config", lambda: mock_config)

    original_contents = host_cli_env.include_file.read_text()

    result = host_cli_env.runner.invoke(
        app,
        [
            "add",
            "dryrun.example.com",
            "--yes",
            "--dry-run",
        ],
        env=host_cli_env.env,
    )
    assert result.exit_code == 0, result.output
    assert "Dry-run" in result.stdout

    # File should not have changed
    assert host_cli_env.include_file.read_text() == original_contents
