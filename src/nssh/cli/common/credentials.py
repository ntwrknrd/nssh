"""Unified credential and context helpers for nssh CLI commands.

This module consolidates helpers previously split across:
- nssh.cli.cred.common
- nssh.cli.host.context
"""

from __future__ import annotations

from typing import Any, Dict, List, cast

from nssh.cli import click
from nssh.core.auth.credentials import CredentialManager
from nssh.core.env.paths import host_index_path
from nssh.core.ssh.config import SSHConfigParser
from nssh.core.ui.console import get_console

console = get_console()
ContextStore = Dict[str, Any]


def ensure_context(ctx: click.Context) -> ContextStore:
    """Ensure the Click context has an object store initialized."""
    if ctx.obj is None:
        ctx.obj = {}
    return cast(ContextStore, ctx.obj)


def get_parser(ctx: click.Context) -> SSHConfigParser:
    """Get or create the shared SSHConfigParser from context."""
    store = ensure_context(ctx)
    parser = store.get("parser")
    if parser is None:
        parser = SSHConfigParser()
        store["parser"] = parser
    return cast(SSHConfigParser, parser)


def get_manager(ctx: click.Context) -> CredentialManager:
    """Get or create the shared CredentialManager from context.

    Creates the manager lazily on first access and caches it.
    Exits with error if CredentialManager initialization fails.
    """
    store = ensure_context(ctx)
    manager = store.get("cm")
    if manager is None:
        try:
            manager = CredentialManager()
        except RuntimeError as exc:
            console.print(f"[red]Error: {exc}[/red]")
            raise SystemExit(1)
        store["cm"] = manager
    return cast(CredentialManager, manager)


def complete_hostname(
    ctx: click.Context, param: click.Parameter, incomplete: str
) -> List[str]:
    """Autocomplete hostnames from the cached host index."""
    results: List[str] = []
    try:
        index_path = host_index_path()
        if not index_path.exists():
            return results
        with open(index_path) as handle:
            for raw in handle:
                if "|" not in raw:
                    continue
                hostname = raw.split("|", 1)[0].strip()
                if hostname.startswith(incomplete):
                    results.append(hostname)
    except Exception:
        pass
    return results


def complete_config_file(
    ctx: click.Context, param: click.Parameter, incomplete: str
) -> List[str]:
    """Autocomplete SSH config file names from include directory."""
    results = []
    try:
        parser = SSHConfigParser()
        for file_path in parser.find_include_files():
            filename = file_path.name
            if filename.startswith(incomplete):
                results.append(filename)
    except Exception:
        pass
    return results


def complete_context(
    ctx: click.Context, param: click.Parameter, incomplete: str
) -> List[str]:
    """Autocomplete context names from the credential store."""
    results = []
    try:
        cm = CredentialManager()
        contexts = cm.list_contexts()
        for entry in contexts:
            name = entry.get("name", "")
            if name.startswith(incomplete):
                results.append(name)
    except Exception:
        pass
    return results
