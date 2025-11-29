"""Compatibility fix helpers shared by host commands."""

from __future__ import annotations

import tempfile
from pathlib import Path
from typing import Any, Dict, Optional

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
) -> Dict[str, Any]:
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

    console.print()
    if result["success"]:
        iterations = result["iterations"]
        console.print(
            f"[green]✓[/green] Compatibility fixes applied ({iterations} iteration{'s' if iterations != 1 else ''})"
        )
        if auth_changed:
            console.print(f"  [dim]Authentication: {auth_changed}[/dim]")
        if result["fixes_applied"]:
            for compat_type in result["fixes_applied"]:
                console.print(f"  [dim]- {COMPAT_CONFIGS[compat_type]['name']}[/dim]")

        # Add helpful message if test succeeded via KEX but failed auth
        if result.get("stopped_reason") == "auth_failed_after_kex_success":
            console.print(
                "\n[dim]Note: Test connection failed at authentication (expected with password-only hosts)[/dim]"
            )
            console.print(
                "[dim]Compatibility fix succeeded - nssh connections will work normally[/dim]"
            )

        return result

    iterations = result["iterations"]
    reason = result["stopped_reason"].replace("_", " ")
    console.print(
        f"[yellow]![/yellow] Compatibility fixes incomplete ({iterations} iteration{'s' if iterations != 1 else ''}): {reason}"
    )
    if auth_changed:
        console.print(f"  [dim]Authentication: {auth_changed}[/dim]")
    if result["fixes_applied"]:
        for compat_type in result["fixes_applied"]:
            console.print(f"  [dim]- {COMPAT_CONFIGS[compat_type]['name']}[/dim]")

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

    console.print(f"\n[yellow]![/yellow] Debug info: {debug_file}")
    console.print(f"[yellow]![/yellow] Try: ssh -v {hostname}")

    return result
