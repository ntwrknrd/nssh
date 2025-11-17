"""Shared CLI helper namespace with lazy submodule loading."""

from __future__ import annotations

from importlib import import_module
from typing import Any

_SUBMODULES = ["ui", "prompt", "selectors", "help", "app", "workflows"]
__all__ = list(_SUBMODULES)


def __getattr__(name: str) -> Any:  # pragma: no cover - import indirection
    if name in _SUBMODULES:
        module = import_module(f"nssh.cli.common.{name}")
        globals()[name] = module
        return module
    raise AttributeError(f"module 'nssh.cli.common' has no attribute '{name}'")


def __dir__() -> list[str]:  # pragma: no cover - introspection helper
    return sorted(set(globals().keys()) | set(_SUBMODULES))
