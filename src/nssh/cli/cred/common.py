"""Common helpers for the nssh cred CLI package."""

from __future__ import annotations

from typing import Any, Dict, List, cast

from nssh.cli import typer
from nssh.core.ui.console import get_console
from nssh.core.auth.credentials import CredentialManager
from nssh.core.env.paths import age_key_path, host_index_path
from nssh.core.env.system import check_command

console = get_console()
ContextStore = Dict[str, Any]


def get_manager(ctx: typer.Context) -> CredentialManager:
    """Return the shared CredentialManager stored on the Typer context."""

    if ctx.obj is None:
        raise RuntimeError("Credential manager context not initialized")
    context_data = cast(ContextStore, ctx.obj)
    return cast(CredentialManager, context_data["cm"])


def complete_context(incomplete: str) -> List[str]:
    """Autocomplete context names from the credential store."""

    try:
        key_path = age_key_path()
        if not check_command("age") or not key_path.exists():
            return []
        cm = CredentialManager(age_key=key_path)
        contexts = cm.list_contexts()
        return [
            entry["name"]
            for entry in contexts
            if entry.get("name", "").startswith(incomplete)
        ]
    except Exception:
        return []


def complete_hostname(incomplete: str) -> List[str]:
    """Autocomplete hostnames from the cached host index."""

    try:
        index_path = host_index_path()
        if not index_path.exists():
            return []
        matches: List[str] = []
        with open(index_path) as host_index:
            for line in host_index:
                if "|" not in line:
                    continue
                hostname = line.split("|")[0].strip()
                if hostname.startswith(incomplete):
                    matches.append(hostname)
        return matches
    except Exception:
        return []
