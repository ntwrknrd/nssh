#!/usr/bin/env python3
"""Asset management and file installation helpers for self."""

from __future__ import annotations

import os
import shutil
from importlib import resources
from pathlib import Path
from typing import TYPE_CHECKING, Any, Optional, Protocol, cast

if TYPE_CHECKING:  # pragma: no cover - typing-time import only
    from importlib.resources.abc import Traversable
else:  # pragma: no cover - runtime compatibility shim
    try:
        from importlib.resources.abc import Traversable
    except ModuleNotFoundError:

        class Traversable(Protocol):
            def joinpath(self, *names: str) -> "Traversable": ...

            def is_file(self) -> bool: ...

            def open(self, *args: Any, **kwargs: Any): ...


from nssh.cli import typer
from nssh.cli.self.manifest import InstallManifest
from nssh.cli.self.system import rel_home
from nssh.cli.common import ui
from nssh.cli.common.prompt import confirm
from nssh.core.ui.console import get_console

console = get_console()

ASSET_PACKAGE = "nssh.assets"
PROFILE_MARKER = "# >>> nssh integration >>>"


def get_asset(category: str, name: str) -> Traversable:
    """Load an asset from the nssh.assets package.

    Args:
        category: Asset category (e.g., "scripts", "completions")
        name: Asset filename

    Returns:
        Traversable reference to the asset file

    Raises:
        FileNotFoundError: If asset doesn't exist
    """
    root = resources.files(ASSET_PACKAGE)
    target = root.joinpath(category).joinpath(name)
    if not target.is_file():  # pragma: no cover - defensive
        raise FileNotFoundError(f"Missing asset: {category}/{name}")
    return target


def write_file(src: Path, dest: Path, *, executable: bool) -> None:
    """Copy a file and optionally make it executable.

    Args:
        src: Source file path
        dest: Destination file path
        executable: Whether to set executable permissions (0o755)
    """
    dest.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(src, dest)
    if executable:
        dest.chmod(0o755)


def install_resource(
    category: str,
    name: str,
    dest: Path,
    *,
    executable: bool,
    symlink: bool,
    dry_run: bool,
    force: bool,
    manifest: Optional[InstallManifest] = None,
) -> bool:
    """Install a resource and optionally track it in manifest.

    Args:
        category: Asset category
        name: Asset name
        dest: Destination path
        executable: Whether to make file executable
        symlink: Whether to symlink instead of copy
        dry_run: Preview mode (don't write files)
        force: Overwrite without prompting
        manifest: Optional manifest to track installation

    Returns:
        True if installed, False if skipped.
    """
    asset_ref = get_asset(category, name)
    action = "Symlink" if symlink else "Copy"

    if dest.exists() or dest.is_symlink():
        if not force:
            overwrite = confirm(
                f"[dim]{dest} already exists[/dim]. [yellow]Overwrite?[/yellow] [yellow][y/n][/yellow]",
                default=False,
            )
            if not overwrite:
                console.print(f"[yellow]Skipping[/yellow] [dim]{dest}[/dim]")
                return False
        if not dry_run:
            if dest.is_dir():
                raise typer.BadParameter(f"Cannot overwrite directory: {dest}")
            dest.unlink()

    console.print(f"{action} [cyan]{name}[/cyan] -> [green]{dest}[/green]")
    if dry_run:
        return True

    if symlink:
        dest.parent.mkdir(parents=True, exist_ok=True)
        try:
            src_candidate = cast(os.PathLike[str], asset_ref)
            src_path = Path(os.fspath(src_candidate))
        except TypeError as exc:  # pragma: no cover - zip import edge
            raise typer.BadParameter(
                "Cannot create symlinks when assets are loaded from a zip archive"
            ) from exc
        dest.symlink_to(src_path)
        if manifest:
            manifest.add_file(dest, "symlink", f"{category}/{name}", src_path)
        return True

    with resources.as_file(asset_ref) as src_path:
        write_file(src_path, dest, executable=executable)
        if manifest:
            manifest.add_file(dest, "file", f"{category}/{name}")
    return True


def append_profile_snippet(
    profile: Path,
    share_dir: Path,
    dry_run: bool,
    manifest: Optional[InstallManifest] = None,
) -> None:
    """Append shell integration snippet to a profile file.

    Args:
        profile: Path to shell profile file (e.g., ~/.bashrc, ~/.zshrc)
        share_dir: Directory containing shell integration script
        dry_run: Preview mode (don't modify files)
        manifest: Optional manifest to track modification
    """
    is_fish = profile.name.endswith(".fish") or "fish" in profile.parts

    if is_fish:
        shell_helper = share_dir / "nssh-shell-integration.fish"
        helper_text = rel_home(shell_helper)
        snippet = f"""
{PROFILE_MARKER}
if test -f {helper_text}
    source {helper_text}
end
# <<< nssh integration <<<
""".strip()
    else:
        shell_helper = share_dir / "nssh-shell-integration.sh"
        helper_text = rel_home(shell_helper)
        snippet = f"""
{PROFILE_MARKER}
if [ -f "{helper_text}" ]; then
    . "{helper_text}"
fi
# <<< nssh integration <<<
""".strip()

    if profile.exists():
        content = profile.read_text()
        if PROFILE_MARKER in content:
            console.print(f"[green]Shell snippet already present in {profile}[/green]")
            return

    ui.show_panel(
        "Append Shell Snippet",
        "Appending shell integration block",
        style="cyan",
        subtitle=str(profile),
    )
    if dry_run:
        ui.show_panel("Shell Snippet Preview", snippet, style="yellow")
        return

    profile.parent.mkdir(parents=True, exist_ok=True)

    # Track current line count for manifest
    line_start = len(profile.read_text().splitlines()) if profile.exists() else 0

    with profile.open("a") as fp:
        fp.write("\n" + snippet + "\n")

    line_end = line_start + len(snippet.splitlines()) + 1

    if manifest:
        manifest.add_profile_modification(profile, PROFILE_MARKER, line_start, line_end)
