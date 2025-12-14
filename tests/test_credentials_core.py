from __future__ import annotations

import json
from collections.abc import Callable, Mapping
from pathlib import Path

import types

import pytest

from nssh.core.auth.credentials import CredentialManager

CredentialData = Mapping[str, object]
CredentialFactory = Callable[[Path, CredentialData], CredentialManager]


@pytest.fixture()
def credential_manager_factory(
    monkeypatch: pytest.MonkeyPatch,
) -> CredentialFactory:
    """Return a factory that yields a CredentialManager backed by temp files."""

    def _factory(tmp_path: Path, data: CredentialData) -> CredentialManager:
        # Pretend required binaries exist so __init__ succeeds.
        monkeypatch.setattr("nssh.core.auth.credentials.check_command", lambda _: True)

        fake_age_key = tmp_path / "keys.txt"
        fake_age_key.write_text("AGE-SECRET-KEY-1.....")

        # Stub out run_command to avoid invoking external binaries when helper methods run.
        monkeypatch.setattr(
            "nssh.core.auth.credentials.run_command",
            lambda *args, **kwargs: types.SimpleNamespace(stdout="AGE-PUB-KEY"),
        )

        manager = CredentialManager(
            credential_file=tmp_path / "nssh_credentials.age",
            age_key=fake_age_key,
        )

        # Short-circuit decryption so higher-level helpers read the provided fixture data.
        monkeypatch.setattr(manager, "decrypt_credentials", lambda: data)
        return manager

    return _factory


@pytest.fixture()
def credential_store_env(
    monkeypatch: pytest.MonkeyPatch, tmp_path: Path
) -> types.SimpleNamespace:
    """Provision a CredentialManager wired to temp files and fake age commands."""

    monkeypatch.setattr("nssh.core.auth.credentials.check_command", lambda _: True)

    credential_file = tmp_path / "nssh_credentials.age"
    age_key = tmp_path / "keys.txt"
    age_key.write_text("AGE-SECRET-KEY-1.....")

    def _fake_run(
        cmd: list[str] | tuple[str, ...],
        input_text: str | None = None,
        **_: object,
    ) -> types.SimpleNamespace:
        if cmd and cmd[0] == "age-keygen":
            return types.SimpleNamespace(stdout="AGE-PUB-KEY")
        if cmd and cmd[0] == "age" and "-d" in cmd:
            if credential_file.exists():
                return types.SimpleNamespace(stdout=credential_file.read_text())
            return types.SimpleNamespace(
                stdout=json.dumps({"contexts": {}, "hosts": {}})
            )
        if cmd and cmd[0] == "age" and "-r" in cmd:
            target = Path(cmd[-1])
            target.write_text(input_text or "")
            return types.SimpleNamespace(stdout="")
        raise AssertionError(f"Unexpected command: {cmd}")

    monkeypatch.setattr("nssh.core.auth.credentials.run_command", _fake_run)

    permission_calls: list[Path] = []

    def _record_permissions(path: Path) -> None:
        permission_calls.append(path)

    monkeypatch.setattr(
        "nssh.core.auth.credentials.set_secure_permissions", _record_permissions
    )

    manager = CredentialManager(credential_file=credential_file, age_key=age_key)

    return types.SimpleNamespace(
        manager=manager,
        credential_file=credential_file,
        permission_calls=permission_calls,
    )


def test_resolve_credential_prefers_host_records(
    tmp_path: Path, credential_manager_factory: CredentialFactory
) -> None:
    data = {
        "contexts": {
            "work": {
                "git_include_file": "work_hosts",
                "credential": {"username": "ctx-user", "password": "ctx-pass"},
            }
        },
        "hosts": {
            "router1": {
                "credentials": [
                    {"username": "host-user", "password": "host-pass"},
                ]
            }
        },
    }

    cm = credential_manager_factory(tmp_path, data)
    resolved = cm.resolve_credential("router1", "work_hosts")
    assert resolved is not None
    username, password = resolved

    assert username == "host-user"
    assert password == "host-pass"


def test_resolve_credential_falls_back_to_context_user(
    tmp_path: Path, credential_manager_factory: CredentialFactory
) -> None:
    data = {
        "contexts": {
            "work": {
                "git_include_file": "work_hosts",
                "credential": {"username": "ctx-user", "password": "ctx-pass"},
            }
        },
        "hosts": {
            "router1": {
                "credentials": [
                    {"username": "host-user", "password": "host-pass"},
                ]
            }
        },
    }

    cm = credential_manager_factory(tmp_path, data)
    result = cm.resolve_credential("router1", "work_hosts", username="ctx-user")
    assert result is not None
    assert result == ("ctx-user", "ctx-pass")


def test_encrypt_credentials_round_trip(
    credential_store_env: types.SimpleNamespace,
) -> None:
    env = credential_store_env
    payload = {
        "contexts": {"lab": {"git_include_file": "lab", "credential": None}},
        "hosts": {},
    }

    assert env.manager.encrypt_credentials(payload) is True

    on_disk = json.loads(env.credential_file.read_text())
    assert on_disk == payload
    assert env.permission_calls[-1] == env.credential_file


def test_encrypt_credentials_creates_backups(
    credential_store_env: types.SimpleNamespace,
) -> None:
    env = credential_store_env
    first: CredentialData = {"contexts": {}, "hosts": {}}
    second: CredentialData = {
        "contexts": {},
        "hosts": {"edge": {"credentials": []}},
    }

    env.manager.encrypt_credentials(first)
    env.manager.encrypt_credentials(second)

    backups = list(env.credential_file.parent.glob("nssh_credentials.age.bak.*"))
    assert backups, "Expected at least one backup file"


def test_encrypt_credentials_errors_when_locked(
    credential_store_env: types.SimpleNamespace,
) -> None:
    env = credential_store_env
    env.manager._lock_timeout = 0.1

    with env.manager._exclusive_lock():
        with pytest.raises(RuntimeError):
            env.manager.encrypt_credentials({"contexts": {}, "hosts": {}})


def test_context_lifecycle_persists_to_disk(
    credential_store_env: types.SimpleNamespace,
) -> None:
    env = credential_store_env

    assert env.manager.create_context("work", "work_hosts") is True
    env.manager.add_context_credential("work", "alice", "s3cret")

    contexts = env.manager.list_contexts()
    assert contexts[0]["name"] == "work"
    assert contexts[0]["credential_count"] == 1
    assert contexts[0]["credential"]["username"] == "alice"

    assert env.manager.delete_context("work") is True
    assert env.manager.delete_context("work") is False


def test_create_context_normalizes_include_filename(
    credential_store_env: types.SimpleNamespace,
) -> None:
    env = credential_store_env

    assert env.manager.create_context("abs", "/Users/example/.ssh/work_hosts") is True

    on_disk = json.loads(env.credential_file.read_text())
    assert on_disk["contexts"]["abs"]["git_include_file"] == "work_hosts"


def test_get_context_matches_basename_for_absolute_paths(
    tmp_path: Path, credential_manager_factory: CredentialFactory
) -> None:
    data = {
        "contexts": {
            "work": {
                "git_include_file": "/Users/example/.ssh/work_hosts",
                "credential": {"username": "chris.jones", "password": "pw"},
            }
        },
        "hosts": {},
    }

    cm = credential_manager_factory(tmp_path, data)
    context = cm.get_context("work_hosts")

    assert context is not None
    assert context["name"] == "work"
    assert context["credential"]["username"] == "chris.jones"


def test_host_credential_operations_enforce_uniqueness(
    credential_store_env: types.SimpleNamespace,
) -> None:
    env = credential_store_env

    env.manager.add_host_credential("router1", "root", "pw1")
    with pytest.raises(ValueError):
        env.manager.add_host_credential("router1", "root", "pw2")

    assert env.manager.delete_host_credential("router1", "root") is True
    assert env.manager.delete_host_credential("router1", "root") is False
    assert env.manager.delete_host_all_credentials("router1") is False


def test_encrypt_credentials_validates_structure(
    credential_store_env: types.SimpleNamespace,
) -> None:
    env = credential_store_env
    with pytest.raises(ValueError):
        env.manager.encrypt_credentials({"contexts": [], "hosts": []})


def test_context_overwrite_requires_flag(
    credential_store_env: types.SimpleNamespace,
) -> None:
    env = credential_store_env
    env.manager.create_context("lab", "lab_hosts")
    env.manager.add_context_credential("lab", "alice", "oldpw")

    with pytest.raises(ValueError):
        env.manager.add_context_credential("lab", "bob", "newpw")

    env.manager.add_context_credential("lab", "bob", "newpw", overwrite=True)

    contexts = env.manager.list_contexts()
    assert contexts[0]["credential"]["username"] == "bob"
