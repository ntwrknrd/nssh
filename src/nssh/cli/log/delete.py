"""Delete command for nssh log."""

from __future__ import annotations

from pathlib import Path
from typing import Optional

from nssh.cli import click
from nssh.cli.common.banner import ABORT, FAIL, NOOP, OK, banner
from nssh.cli.common.prompt import confirm
from nssh.core.recording import manager as recording
from nssh.core.ui.console import get_console

from . import common


@click.command(short_help="Delete recorded sessions")
@click.option(
    "--select",
    "-s",
    default=None,
    help="Filter by regex pattern",
)
@click.option(
    "--older-than",
    default=None,
    type=int,
    help="Delete recordings older than N days",
)
@click.option("-y", "--yes", is_flag=True, default=False, help="Skip confirmation")
@common.dry_run_option
def delete_command(
    select: Optional[str],
    older_than: Optional[int],
    yes: bool,
    dry_run: bool,
) -> None:
    """Delete recorded sessions.

    Three mutually exclusive modes:
      1. --select PATTERN     : Delete recordings matching regex pattern
      2. --older-than DAYS    : Delete recordings older than N days
      3. Interactive (default): Select recordings via fzf (no mode flags)

    Cannot combine modes - use only one mode flag at a time.
    """
    with banner("DELETE RECORDINGS", OK) as set_outcome:
        _delete_impl(select, older_than, yes, dry_run, set_outcome)


def _delete_impl(
    select: Optional[str],
    older_than: Optional[int],
    yes: bool,
    dry_run: bool,
    set_outcome,
) -> None:
    """Internal implementation for delete command."""
    console = get_console()
    settings = recording.load_recording_settings()

    # Validate mutual exclusion
    modes_specified = sum(
        [
            older_than is not None,
            select is not None,
        ]
    )

    if modes_specified > 1:
        console.print(
            "[red]Error: Cannot combine modes. "
            "Use one of: --select, --older-than, or interactive (no flags)[/red]"
        )
        set_outcome(FAIL)
        raise SystemExit(1)

    # Mode 1: --older-than (batch age-based cleanup)
    if older_than is not None:
        if older_than <= 0:
            raise click.BadParameter("--older-than must be a positive number")

        summary = recording.cleanup_old_recordings(
            max_age_days=older_than, settings=settings, dry_run=dry_run
        )

        if summary is None:
            console.print("[green]✓[/green] No recordings to clean up")
            set_outcome(NOOP)
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
            set_outcome(NOOP)
        return

    # Mode 2: --select pattern (batch regex delete)
    if select:
        sessions = common.load_sessions(settings)
        filtered = common.select_sessions_by_pattern(sessions, select)

        if not filtered:
            console.print(f"[yellow]No recordings match '{select}'[/yellow]")
            set_outcome(FAIL)
            raise SystemExit(1)

        console.print(f"Found {len(filtered)} recording(s) matching '{select}'")

        if not yes and not dry_run:
            if not confirm(f"Delete all {len(filtered)}?", default=False):
                set_outcome(ABORT)
                raise SystemExit(0)

        for session in filtered:
            common.delete_recording(
                session.cast_path, settings.directory, dry_run, console
            )

        if dry_run:
            set_outcome(NOOP)
        return

    # Mode 3: Interactive fzf multi-select (default)
    targets = common.resolve_recording_paths_multi(settings)

    if not yes and not dry_run:
        if len(targets) == 1:
            display = str(targets[0]).replace(str(Path.home()), "~", 1)
            confirmed = confirm(f"Delete {display}?", default=True)
        else:
            confirmed = confirm(f"Delete {len(targets)} recording(s)?", default=False)
        if not confirmed:
            set_outcome(ABORT)
            raise SystemExit(0)

    for target in targets:
        common.delete_recording(target, settings.directory, dry_run, console)

    if dry_run:
        set_outcome(NOOP)
