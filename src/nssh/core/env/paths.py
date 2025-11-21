"""Dynamic path helpers for credential/config artifacts."""

from __future__ import annotations

import os
from pathlib import Path
from typing import Dict

from nssh.core.env.settings import default_config_path, load_toml_config

_DEFAULT_CREDENTIAL_FILE = "~/.ssh/nssh_credentials.age"
_DEFAULT_AGE_KEY = "~/.config/age/keys.txt"
_DEFAULT_BACKUP_DIR = "~/.ssh/backups"
_DEFAULT_SSH_CONFIG = "~/.ssh/config"
_DEFAULT_SSH_INCLUDE_DIR = "~/.ssh/conf.d"
_DEFAULT_HOST_INDEX = "~/.ssh/.nssh_host_index"
_DEFAULT_SHARE_DIR = "~/.local/share/nssh"
_DEFAULT_FISH_FUNCTIONS_DIR = "~/.config/fish/functions"
_DEFAULT_FISH_COMPLETIONS_DIR = "~/.config/fish/completions"


def _load_config() -> Dict[str, object]:
    return load_toml_config(default_config_path())


def _encryption_config() -> Dict[str, str]:
    config = _load_config()
    if not isinstance(config, dict):
        return {}
    encryption_section = config.get("encryption", {})
    return encryption_section if isinstance(encryption_section, dict) else {}


def _ssh_config() -> Dict[str, str]:
    """Load [ssh] section from config.toml."""
    config = _load_config()
    if not isinstance(config, dict):
        return {}
    ssh_section = config.get("ssh", {})
    return ssh_section if isinstance(ssh_section, dict) else {}


def _self_config() -> Dict[str, str]:
    config = _load_config()
    if not isinstance(config, dict):
        return {}
    self_section = config.get("self", {})
    return self_section if isinstance(self_section, dict) else {}


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


def ssh_config_path() -> Path:
    """Return path to main SSH config file.

    Priority:
    1. NSSH_SSH_CONFIG environment variable
    2. [ssh] config_file in config.toml
    3. Default: ~/.ssh/config
    """
    explicit = os.getenv("NSSH_SSH_CONFIG")
    if explicit:
        return Path(explicit).expanduser()
    config_value = _ssh_config().get("config_file", _DEFAULT_SSH_CONFIG)
    return Path(config_value).expanduser()


def ssh_include_dir() -> Path:
    """Return path to SSH include directory for context-specific files.

    Priority:
    1. NSSH_SSH_CONF_INCLUDE environment variable
    2. [ssh] include_dir in config.toml
    3. Default: ~/.ssh/conf.d
    """
    explicit = os.getenv("NSSH_SSH_CONF_INCLUDE")
    if explicit:
        return Path(explicit).expanduser()
    config_value = _ssh_config().get("include_dir", _DEFAULT_SSH_INCLUDE_DIR)
    return Path(config_value).expanduser()


def host_index_path() -> Path:
    explicit = os.getenv("NSSH_HOST_INDEX")
    if explicit:
        return Path(explicit).expanduser()
    config_value = _ssh_config().get("host_index", _DEFAULT_HOST_INDEX)
    return Path(config_value).expanduser()


def share_assets_dir() -> Path:
    explicit = os.getenv("NSSH_SHARE_DIR")
    if explicit:
        return Path(explicit).expanduser()
    config_value = _self_config().get("share_dir", _DEFAULT_SHARE_DIR)
    return Path(config_value).expanduser()


def fish_functions_dir() -> Path:
    explicit = os.getenv("NSSH_FISH_FUNCTIONS_DIR")
    if explicit:
        return Path(explicit).expanduser()
    config_value = _self_config().get("fish_functions_dir", _DEFAULT_FISH_FUNCTIONS_DIR)
    return Path(config_value).expanduser()


def fish_completions_dir() -> Path:
    explicit = os.getenv("NSSH_FISH_COMPLETIONS_DIR")
    if explicit:
        return Path(explicit).expanduser()
    config_value = _self_config().get(
        "fish_completions_dir", _DEFAULT_FISH_COMPLETIONS_DIR
    )
    return Path(config_value).expanduser()


def project_root() -> Path:
    """Find nssh project root by walking up to find pyproject.toml.

    Returns:
        Path to project root (directory containing pyproject.toml)
        Falls back to current directory if not found
    """
    current = Path.cwd()

    # Walk up the directory tree looking for pyproject.toml
    for parent in [current, *current.parents]:
        if (parent / "pyproject.toml").exists():
            return parent

    # Fallback to current directory if pyproject.toml not found
    return current
