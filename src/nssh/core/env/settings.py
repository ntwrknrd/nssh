"""Centralized configuration utilities for nssh."""

from __future__ import annotations

import importlib
import os
from dataclasses import dataclass, field
from pathlib import Path
from typing import Dict, Optional

try:  # Python 3.11+: stdlib tomllib
    import tomllib  # type: ignore[attr-defined]
except ModuleNotFoundError:  # pragma: no cover
    tomllib = importlib.import_module("tomli")  # type: ignore[assignment]


# -----------------------------------------------------------------------------
# Config Dataclasses
# -----------------------------------------------------------------------------


@dataclass
class DefaultsConfig:
    """User-customizable defaults for CLI operations."""

    context: Optional[str] = None  # Default context for new hosts
    user: Optional[str] = None  # Default SSH username


@dataclass
class HostAddConfig:
    """Configuration for `nssh host add` command behavior."""

    test_connection: bool = True  # Run connectivity test by default
    prompt_for_credential: bool = False  # Skip "Store credential?" prompt


@dataclass
class BatchConfig:
    """Configuration for batch file operations."""

    continue_on_error: bool = True  # Don't stop batch on individual failures


@dataclass
class BackupConfig:
    """Configuration for backup file management."""

    max_files: int = 10  # Maximum backup files to retain per source file


@dataclass
class ContextConfig:
    """Per-context configuration settings.

    Note: Domain matching is handled by the credential manager, not TOML config.
    """

    name: str  # Context name (from config section key)
    key_file: Optional[str] = None  # Path to age key file
    test_connection: Optional[bool] = None  # Override test_connection per context


@dataclass
class NsshConfig:
    """Complete nssh configuration loaded from config.toml."""

    defaults: DefaultsConfig = field(default_factory=DefaultsConfig)
    host_add: HostAddConfig = field(default_factory=HostAddConfig)
    batch: BatchConfig = field(default_factory=BatchConfig)
    backup: BackupConfig = field(default_factory=BackupConfig)
    contexts: Dict[str, ContextConfig] = field(default_factory=dict)

    def get_default_context(self) -> Optional[str]:
        """Get the default context, respecting env var override."""
        env_context = os.getenv("NSSH_DEFAULT_CONTEXT")
        if env_context:
            return env_context
        return self.defaults.context

    def get_default_user(self) -> Optional[str]:
        """Get the default user from env var or config."""
        env_user = os.getenv("NSSH_DEFAULT_USER")
        if env_user:
            return env_user
        return self.defaults.user


def expand_path(value: str) -> Path:
    """Expand ~ and environment variables, returning an absolute path."""

    return Path(os.path.expandvars(os.path.expanduser(value))).resolve()


def default_config_path(
    *,
    env_var: str = "NSSH_CONFIG",
    xdg_var: str = "XDG_CONFIG_HOME",
    relative: str = "nssh/config.toml",
) -> Path:
    """Compute the config path honoring env + XDG overrides."""

    explicit = os.getenv(env_var)
    if explicit:
        return expand_path(explicit)

    xdg_root = os.getenv(xdg_var)
    if xdg_root:
        return Path(xdg_root).expanduser() / relative

    return Path.home() / ".config" / relative


def default_state_root(
    *,
    xdg_var: str = "XDG_STATE_HOME",
    relative: str = "nssh",
) -> Path:
    """Return the base directory for mutable state (recordings, locks)."""

    xdg_state = os.getenv(xdg_var)
    if xdg_state:
        return Path(xdg_state).expanduser() / relative
    return Path.home() / ".local" / "state" / relative


def default_data_root(
    *,
    xdg_var: str = "XDG_DATA_HOME",
    relative: str = "nssh",
) -> Path:
    """Return base directory for user data (credentials, backups)."""

    xdg_data = os.getenv(xdg_var)
    if xdg_data:
        return Path(xdg_data).expanduser() / relative
    return Path.home() / ".local" / "share" / relative


def load_toml_config(path: Optional[Path] = None) -> Dict[str, object]:
    """Load a TOML config file, returning an empty dict if missing/invalid."""

    config_path = path or default_config_path()
    if not config_path.exists():
        return {}
    try:
        with open(config_path, "rb") as handle:
            return tomllib.load(handle)
    except Exception:  # pragma: no cover - defensive fallback
        return {}


def load_nssh_config(path: Optional[Path] = None) -> NsshConfig:
    """Load and parse nssh configuration from config.toml.

    Parses the following sections:
    - [defaults] -> DefaultsConfig
    - [host.add] -> HostAddConfig
    - [batch] -> BatchConfig
    - [contexts.*] -> Dict[str, ContextConfig]

    Returns:
        NsshConfig with all sections populated (using defaults for missing values)
    """
    raw = load_toml_config(path)

    # Parse [defaults] section
    defaults_raw = raw.get("defaults", {})
    defaults = DefaultsConfig(
        context=defaults_raw.get("context") if isinstance(defaults_raw, dict) else None,
        user=defaults_raw.get("user") if isinstance(defaults_raw, dict) else None,
    )

    # Parse [host.add] section (nested under [host])
    host_raw = raw.get("host", {})
    host_add_raw = host_raw.get("add", {}) if isinstance(host_raw, dict) else {}
    host_add = HostAddConfig(
        test_connection=(
            host_add_raw.get("test_connection", True)
            if isinstance(host_add_raw, dict)
            else True
        ),
        prompt_for_credential=(
            host_add_raw.get("prompt_for_credential", False)
            if isinstance(host_add_raw, dict)
            else False
        ),
    )

    # Parse [batch] section
    batch_raw = raw.get("batch", {})
    batch = BatchConfig(
        continue_on_error=(
            batch_raw.get("continue_on_error", True)
            if isinstance(batch_raw, dict)
            else True
        ),
    )

    # Parse [backup] section
    backup_raw = raw.get("backup", {})
    backup = BackupConfig(
        max_files=(
            backup_raw.get("max_files", 10) if isinstance(backup_raw, dict) else 10
        ),
    )

    # Parse [contexts.*] sections
    contexts: Dict[str, ContextConfig] = {}
    contexts_raw = raw.get("contexts", {})
    if isinstance(contexts_raw, dict):
        for name, ctx_data in contexts_raw.items():
            if isinstance(ctx_data, dict):
                contexts[name] = ContextConfig(
                    name=name,
                    key_file=ctx_data.get("key_file"),
                    test_connection=ctx_data.get("test_connection"),
                )

    return NsshConfig(
        defaults=defaults,
        host_add=host_add,
        batch=batch,
        backup=backup,
        contexts=contexts,
    )


# Module-level cached config instance
_cached_config: Optional[NsshConfig] = None


def get_config(*, reload: bool = False) -> NsshConfig:
    """Get the nssh configuration, caching the result.

    Args:
        reload: Force reload from disk (default: False)

    Returns:
        Cached or freshly loaded NsshConfig
    """
    global _cached_config
    if _cached_config is None or reload:
        _cached_config = load_nssh_config()
    return _cached_config
