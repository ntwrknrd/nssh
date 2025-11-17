"""Centralized configuration utilities for nssh."""

from __future__ import annotations

import importlib
import os
from pathlib import Path
from typing import Dict, Optional

try:  # Python 3.11+: stdlib tomllib
    import tomllib  # type: ignore[attr-defined]
except ModuleNotFoundError:  # pragma: no cover
    tomllib = importlib.import_module("tomli")  # type: ignore[assignment]


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
