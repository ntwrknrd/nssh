"""Shared context helpers for nssh host commands."""

from __future__ import annotations

from typing import Any, Dict, Iterator, cast

from nssh.cli import typer
from nssh.core.ui.console import get_console
from nssh.core.env.paths import host_index_path
from nssh.core.auth.credentials import CredentialManager
from nssh.core.ssh.config import SSHConfigParser

console = get_console()
ContextStore = Dict[str, Any]


def ensure_context(ctx: typer.Context) -> ContextStore:
    if ctx.obj is None:
        ctx.obj = {}
    return cast(ContextStore, ctx.obj)


def get_parser(ctx: typer.Context) -> SSHConfigParser:
    store = ensure_context(ctx)
    parser = store.get("parser")
    if parser is None:
        parser = SSHConfigParser()
        store["parser"] = parser
    return cast(SSHConfigParser, parser)


def get_manager(ctx: typer.Context) -> CredentialManager:
    store = ensure_context(ctx)
    manager = store.get("cm")
    if manager is None:
        try:
            manager = CredentialManager()
        except RuntimeError as exc:
            console.print(f"[red]Error: {exc}[/red]")
            raise typer.Exit(1)
        store["cm"] = manager
    return cast(CredentialManager, manager)


def complete_hostname(incomplete: str) -> Iterator[str]:
    try:
        index_path = host_index_path()
        if not index_path.exists():
            return
        with open(index_path) as handle:
            for raw in handle:
                if "|" not in raw:
                    continue
                hostname = raw.split("|", 1)[0].strip()
                if hostname.startswith(incomplete):
                    yield hostname
    except Exception:
        return


def complete_config_file(incomplete: str) -> Iterator[str]:
    try:
        parser = SSHConfigParser()
        for file_path in parser.find_include_files():
            filename = file_path.name
            if filename.startswith(incomplete):
                yield filename
    except Exception:
        return


def complete_context(incomplete: str) -> Iterator[str]:
    """Autocomplete context names from the credential store."""
    try:
        cm = CredentialManager()
        contexts = cm.list_contexts()
        for entry in contexts:
            name = entry.get("name", "")
            if name.startswith(incomplete):
                yield name
    except Exception:
        return
