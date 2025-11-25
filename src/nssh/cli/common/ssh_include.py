"""Shared helper to ensure ~/.ssh/config includes the conf.d directory."""

from __future__ import annotations

from nssh.cli.common import ui
from nssh.cli.common.prompt import confirm
from nssh.cli.common.workflows import confirm_or_exit
from nssh.core.env.paths import ssh_config_path, ssh_include_dir
from nssh.core.ssh.config import SSHConfigParser
from nssh.core.ssh.include import (
    apply_conf_d_include_plan,
    plan_conf_d_include,
)
from nssh.core.ui.console import get_console

console = get_console()


def ensure_conf_d_include(
    *,
    dry_run: bool = False,
    create_if_missing: bool = False,
    preview_title: str = "SSH config change preview",
    abort_on_decline: bool = False,
) -> bool:
    """
    Ensure ~/.ssh/config contains `Include ~/.ssh/conf.d/*`.

    Args:
        dry_run: Preview actions without writing changes.
        create_if_missing: Create a minimal config if none exists.
        preview_title: Panel title for the preview display.
        abort_on_decline: Exit the command when the user declines a change (instead of continuing).

    Returns:
        True if include already existed or was added/created, False otherwise.
    """
    ssh_config = ssh_config_path()
    ssh_conf_d = ssh_include_dir()
    include_pattern = str(ssh_conf_d / "*")

    plan = plan_conf_d_include(
        ssh_config, ssh_conf_d, create_if_missing=create_if_missing
    )

    if plan is None:
        console.print("[dim]No SSH config found; conf.d include not added[/dim]")
        return False

    if not plan.changed:
        return True

    # Preview
    idx = plan.insert_index
    lines = plan.original_lines if plan.original_lines else plan.new_lines

    preview_before = lines[max(0, idx - 3) : idx]
    preview_after = lines[idx : idx + 3]
    preview_lines: list[str] = []
    if idx > 3:
        preview_lines.append("[dim]...[/dim]")
    preview_lines += preview_before
    preview_lines.append(f"[green]Include {include_pattern}[/green]")
    preview_lines += preview_after
    if idx + 3 < len(lines):
        preview_lines.append("[dim]...[/dim]")

    ui.show_panel(
        preview_title,
        "\n".join(preview_lines) or "[dim](file is empty)[/dim]",
        style="yellow",
    )

    if dry_run:
        console.print(f"[dim]Would add: Include {include_pattern}[/dim]")
        return True

    prompt = (
        f"[cyan]Create {ssh_config} with Include {include_pattern}?[/cyan]"
        if plan.created
        else f"[cyan]Add Include {include_pattern} to ~/.ssh/config?[/cyan]"
    )
    if abort_on_decline:
        confirm_or_exit(prompt, default=True)
    else:
        if not confirm(prompt, default=True):
            console.print("[dim]Skipped adding Include directive[/dim]")
            return False

    backup_path = None
    if not plan.created:
        parser = SSHConfigParser(config_file=ssh_config)
        backup_path = parser.create_backup(ssh_config)
        assert backup_path and backup_path.exists(), "Backup was not created"
        console.print(f"[dim]Backup created: {backup_path}[/dim]")

    apply_conf_d_include_plan(
        plan, ssh_config, backup_func=None  # backup already handled above if needed
    )

    console.print(f"[green]✓[/green] Added Include directive for {ssh_conf_d}/")
    return True
