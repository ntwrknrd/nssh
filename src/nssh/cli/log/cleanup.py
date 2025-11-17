"""Cleanup command for nssh log."""

from __future__ import annotations

from nssh.cli import typer
from nssh.core.recording import manager as recording
from nssh.core.ui.console import get_console

from . import common


def cleanup_command(
    dry_run: bool = common.DRY_RUN_OPTION,
) -> None:
    """Remove old recordings based on max_age_days configuration."""
    console = get_console()
    settings = recording.load_recording_settings()

    if settings.max_age_days is None or settings.max_age_days <= 0:
        console.print(
            "[yellow]Cleanup disabled: max_age_days not configured or <= 0[/yellow]"
        )
        console.print(
            "Set [cyan]recording.max_age_days[/cyan] in [dim]~/.config/nssh/config.toml[/dim]"
        )
        raise typer.Exit(code=1)

    summary = recording.cleanup_old_recordings(settings=settings, dry_run=dry_run)

    if summary is None:
        console.print("[green]No recordings to clean up[/green]")
        return

    mode = "[yellow]DRY RUN[/yellow]" if summary.dry_run else "[green]CLEANUP[/green]"
    console.print(
        f"\n{mode} - Cutoff: [cyan]{summary.cutoff.strftime('%Y-%m-%d %H:%M:%S')}[/cyan]"
    )

    total_files = len(summary.removed_casts) + len(summary.removed_indexes)
    console.print(f"\n[bold]Files removed:[/bold] {total_files}")
    console.print(f"  Cast files:  {len(summary.removed_casts)}")
    console.print(f"  Index files: {len(summary.removed_indexes)}")

    if summary.removed_host_dirs:
        console.print(
            f"\n[bold]Empty directories removed:[/bold] {len(summary.removed_host_dirs)}"
        )

    if summary.dry_run:
        console.print(
            "\n[yellow]Run without --dry-run to actually delete files[/yellow]"
        )
