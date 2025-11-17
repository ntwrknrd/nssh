"""Compatibility fix helpers shared by host commands."""

from __future__ import annotations

import tempfile
from pathlib import Path
from typing import List, Optional, Tuple

from nssh.cli.common.ui import show_panel
from nssh.core.ui.console import get_console
from nssh.core.ssh.config import SSHConfigParser
from nssh.core.ssh.fixer import (
    COMPAT_CONFIGS,
    iterative_compatibility_fix,
)

console = get_console()


def apply_and_display_compat_fixes(
    parser: SSHConfigParser,
    hostname: str,
    max_iterations: int = 5,
    show_header: bool = True,
    auth_changed: Optional[str] = None,
) -> Tuple[bool, List[str]]:
    if show_header:
        if auth_changed:
            title = f"Update SSH Host: {hostname}"
            body = "Changed authentication, now auto-fixing compatibility"
        else:
            title = f"Auto-Fix Compatibility: {hostname}"
            body = "Detecting and fixing SSH compatibility issues"
        show_panel(title, body)

    result = iterative_compatibility_fix(
        parser, hostname, max_iterations=max_iterations, verbose=True
    )

    console.print("\n" + "=" * 60)
    console.print("[bold]Results[/bold]")
    console.print("=" * 60)

    if result["success"]:
        console.print("\n[bold green]✓ Success![/bold green]")
        if auth_changed:
            console.print(f"Authentication changed to: {auth_changed}")
        console.print(
            f"Compatibility fixes applied in {result['iterations']} iteration(s)"
        )
        if result["fixes_applied"]:
            console.print("\nCompatibility options added:")
            for compat_type in result["fixes_applied"]:
                console.print(f"  • {COMPAT_CONFIGS[compat_type]['name']}")
        return True, result["fixes_applied"]

    console.print("\n[bold yellow]⚠ Partial success[/bold yellow]")
    if auth_changed:
        console.print(f"Authentication changed to: {auth_changed}")
    console.print(
        f"Compatibility fixing stopped after {result['iterations']} iteration(s): {result['stopped_reason'].replace('_', ' ')}"
    )

    if result["fixes_applied"]:
        console.print("\nCompatibility options attempted:")
        for compat_type in result["fixes_applied"]:
            console.print(f"  • {COMPAT_CONFIGS[compat_type]['name']}")

    test_result = result["final_test_result"]
    debug_dir = Path("/tmp/nssh")
    debug_dir.mkdir(exist_ok=True)
    debug_file = tempfile.mktemp(
        suffix=".txt", prefix="nssh-debug-", dir=str(debug_dir)
    )

    with open(debug_file, "w") as handle:
        handle.write(f"Hostname: {hostname}\n")
        handle.write(f"Exit code: {test_result['exit_code']}\n")
        handle.write(f"Iterations: {result['iterations']}\n")
        handle.write(f"Stopped reason: {result['stopped_reason']}\n\n")
        handle.write("STDERR:\n")
        handle.write(test_result["stderr"])
        handle.write("\n\nSTDOUT:\n")
        handle.write(test_result["stdout"])

    console.print(f"\n[dim]Debug info written to: {debug_file}[/dim]")
    console.print("[dim]Try: ssh -v {hostname}[/dim]")

    return False, result["fixes_applied"]
