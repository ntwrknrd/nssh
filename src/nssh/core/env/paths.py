"""Dynamic path helpers for credential/config artifacts."""

from __future__ import annotations

import os
from pathlib import Path
from typing import Dict

from nssh.core.env.settings import default_config_path, load_toml_config

_DEFAULT_CREDENTIAL_FILE = "~/.ssh/nssh_credentials.age"
_DEFAULT_AGE_KEY = "~/.config/age/keys.txt"
_DEFAULT_BACKUP_DIR = "~/.ssh/backups"


def _load_config() -> Dict[str, object]:
    return load_toml_config(default_config_path())


def _encryption_config() -> Dict[str, str]:
    config = _load_config()
    if not isinstance(config, dict):
        return {}
    encryption_section = config.get("encryption", {})
    return encryption_section if isinstance(encryption_section, dict) else {}


def credential_file_path() -> Path:
    explicit = os.getenv("NSSH_CRED_FILE")
    if explicit:
        return Path(explicit).expanduser()
    config_value = _encryption_config().get("credential_file", _DEFAULT_CREDENTIAL_FILE)
    return Path(config_value).expanduser()


def age_key_path() -> Path:
    explicit = os.getenv("NSSH_AGE_KEY")
    if explicit:
        return Path(explicit).expanduser()
    config_value = _encryption_config().get("age_key", _DEFAULT_AGE_KEY)
    return Path(config_value).expanduser()


def backup_directory() -> Path:
    return Path(os.getenv("NSSH_BACKUP_DIR", _DEFAULT_BACKUP_DIR)).expanduser()
