"""Delete command for nssh log."""

from __future__ import annotations

from datetime import datetime
from pathlib import Path
from typing import Optional

from nssh.cli import typer
from nssh.core.recording import manager as recording
from nssh.core.ui.console import get_console

from . import common


def delete_command(
    file: Optional[Path] = common.RECORDING_FILE_OPTION,
    host: Optional[str] = typer.Option(
        None, "--host", "-h", help="Delete recordings for this host"
    ),
    date: Optional[str] = typer.Option(
        None,
        "--date",
        help="Filter by date (YYYY-MM-DD, default: today with --host). Use '*' for all dates",
    ),
    older_than: Optional[int] = typer.Option(
        None,
        "--older-than",
        help="Delete recordings older than N days",
    ),
    dry_run: bool = common.DRY_RUN_OPTION,
) -> None:
    """Delete recorded sessions."""
    console = get_console()
    settings = recording.load_recording_settings()

    if file:
        # Direct file deletion
        target = Path(file).expanduser()
        if not target.exists():
            raise typer.BadParameter(f"File does not exist: {target}")
        common.delete_recording(target, settings.directory, dry_run, console)
        return

    if older_than is not None:
        # Age-based cleanup (replaces cleanup command)
        if older_than <= 0:
            raise typer.BadParameter("--older-than must be a positive number")

        summary = recording.cleanup_old_recordings(
            max_age_days=older_than, settings=settings, dry_run=dry_run
        )

        if summary is None:
            console.print("[green]No recordings to clean up[/green]")
            return

        mode = (
            "[yellow]DRY RUN[/yellow]" if summary.dry_run else "[green]DELETED[/green]"
        )
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
        return

    if host:
        # Bulk delete by host (and optionally date)
        effective_date = date
        if effective_date is None:
            effective_date = datetime.now().strftime("%Y-%m-%d")

        sessions = common.load_sessions(settings)
        filtered = common.filter_sessions_by_host(sessions, host, effective_date)

        if not filtered:
            if effective_date == "*":
                console.print(f"[yellow]No recordings found for host '{host}'[/yellow]")
            else:
                console.print(
                    f"[yellow]No recordings found for host '{host}' on {effective_date}[/yellow]"
                )
            raise typer.Exit(code=1)

        for session in filtered:
            common.delete_recording(
                session.cast_path, settings.directory, dry_run, console
            )
        return

    # Interactive fzf selection
    target = common.resolve_recording_path(file, date, settings)
    common.delete_recording(target, settings.directory, dry_run, console)
