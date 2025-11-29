"""nssh CLI package bootstrap helpers.

We expose Click and selected Rich classes but only import them on demand
so the bare ``nssh <host>`` fast-path avoids paying for CLI framework startup.
"""

from __future__ import annotations

from typing import Any

__all__ = [
    "click",
    "Console",
    "Prompt",
    "Confirm",
    "Panel",
    "Table",
    "Syntax",
]


def _fail_missing_dependency(exc: Exception) -> None:
    print(f"Error: Required library not found. {exc}")
    print("Install with: pip install rich click")
    raise SystemExit(1)


def _load_rich_attribute(name: str) -> Any:
    try:
        from rich.console import Console
        from rich.panel import Panel
        from rich.prompt import Confirm, Prompt
        from rich.syntax import Syntax
        from rich.table import Table
    except ImportError as exc:  # pragma: no cover - dependency missing
        _fail_missing_dependency(exc)

    globals().update(
        {
            "Console": Console,
            "Panel": Panel,
            "Confirm": Confirm,
            "Prompt": Prompt,
            "Syntax": Syntax,
            "Table": Table,
        }
    )
    return globals()[name]


def __getattr__(name: str) -> Any:  # pragma: no cover - import indirection
    if name == "click":
        try:
            import click as _click
        except ImportError as exc:
            _fail_missing_dependency(exc)
        globals()["click"] = _click
        return _click

    if name in {"Console", "Prompt", "Confirm", "Panel", "Table", "Syntax"}:
        return _load_rich_attribute(name)

    raise AttributeError(f"module 'nssh.cli' has no attribute '{name}'")


def __dir__() -> list[str]:  # pragma: no cover - introspection helper
    return sorted(set(globals().keys()) | set(__all__))
