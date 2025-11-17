#!/usr/bin/env python3
"""Installer CLI for deploying bundled nssh shell helpers."""

from __future__ import annotations

import os
import shutil
from importlib import resources
from pathlib import Path
from typing import Annotated, Optional, Sequence, cast, Any, Protocol, TYPE_CHECKING

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
from nssh.cli.common import ui
from nssh.cli.common.app import run_cli
from nssh.cli.common.help import UsageRow, UsageSection, render_usage
from nssh.cli.common.prompt import confirm
from nssh.core.ui.console import get_console

console = get_console()
app = typer.Typer(add_help_option=False, rich_markup_mode=None)

ASSET_PACKAGE = "nssh.assets"
DEFAULT_BIN_DIR = Path(os.environ.get("NSSH_BIN_DIR", Path.home() / ".local/bin"))
DEFAULT_SHARE_DIR = Path(
    os.environ.get("NSSH_SHARE_DIR", Path.home() / ".local/share/nssh")
)


def _xdg_config_home() -> Path:
    raw = os.environ.get("XDG_CONFIG_HOME")
    return Path(raw) if raw else Path.home() / ".config"


DEFAULT_COMPLETIONS_DIR = _xdg_config_home() / "fish" / "completions"
DEFAULT_FISH_FUNCTIONS_DIR = _xdg_config_home() / "fish" / "functions"

WRAPPER_TARGET = "nssh"
WRAPPER_FILENAME = "nssh-wrapper.sh"
PROFILE_MARKER = "# >>> nssh integration >>>"


def _asset(category: str, name: str) -> Traversable:
    root = resources.files(ASSET_PACKAGE)
    target = root.joinpath(category).joinpath(name)
    if not target.is_file():  # pragma: no cover - defensive
        raise FileNotFoundError(f"Missing asset: {category}/{name}")
    return target


def _write_file(src: Path, dest: Path, *, executable: bool) -> None:
    dest.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(src, dest)
    if executable:
        dest.chmod(0o755)


def _install_resource(
    category: str,
    name: str,
    dest: Path,
    *,
    executable: bool,
    symlink: bool,
    dry_run: bool,
    force: bool,
) -> None:
    asset_ref = _asset(category, name)
    action = "Symlink" if symlink else "Copy"
    if dest.exists() or dest.is_symlink():
        if not force:
            overwrite = confirm(
                f"[dim]{dest} already exists[/dim]. [yellow]Overwrite?[/yellow] [yellow][y/n][/yellow]",
                default=False,
            )
            if not overwrite:
                console.print(f"[yellow]Skipping[/yellow] [dim]{dest}[/dim]")
                return
        if not dry_run:
            if dest.is_dir():
                raise typer.BadParameter(f"Cannot overwrite directory: {dest}")
            dest.unlink()

    console.print(f"{action} [cyan]{name}[/cyan] -> [green]{dest}[/green]")
    if dry_run:
        return

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
        return

    with resources.as_file(asset_ref) as src_path:
        _write_file(src_path, dest, executable=executable)


def _install_wrapper_link(
    wrapper_src: Path,
    link_path: Path,
    *,
    dry_run: bool,
    force: bool,
) -> None:
    ui.show_panel(
        "Wrapper Symlink",
        f"[cyan]{link_path}[/cyan] → [green]{wrapper_src}[/green]",
        style="cyan",
    )

    if link_path.exists() or link_path.is_symlink():
        if link_path.is_dir():
            raise typer.BadParameter(f"Cannot overwrite directory: {link_path}")
        if not force:
            overwrite = confirm(
                f"[dim]{link_path} already exists[/dim]. [yellow]Overwrite?[/yellow] [yellow][y/n][/yellow]",
                default=False,
            )
            if not overwrite:
                console.print(
                    f"[yellow]Skipping wrapper link[/yellow] [dim]{link_path}[/dim]"
                )
                return
        if not dry_run:
            link_path.unlink()

    if dry_run:
        return

    link_path.parent.mkdir(parents=True, exist_ok=True)
    link_path.symlink_to(wrapper_src)


def _rel_home(path: Path) -> str:
    text = str(path)
    home = str(Path.home())
    if text.startswith(home):
        return text.replace(home, "~", 1)
    return text


def _append_profile_snippet(profile: Path, share_dir: Path, dry_run: bool) -> None:
    shell_helper = share_dir / "nssh-shell-integration.sh"
    helper_text = _rel_home(shell_helper)
    snippet = f"""
{PROFILE_MARKER}
if [ -f \"{helper_text}\" ]; then
    . \"{helper_text}\"
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
    with profile.open("a") as fp:
        fp.write("\n" + snippet + "\n")


def _install_fish_function(
    functions_dir: Path,
    share_dir: Path,
    *,
    dry_run: bool,
    symlink: bool,
    force: bool,
) -> None:
    target = functions_dir / "nssh.fish"
    ui.show_panel("Fish Function", f"→ [green]{target}[/green]", style="cyan")
    _install_resource(
        "scripts",
        "nssh-shell-integration.fish",
        target,
        executable=True,
        symlink=symlink,
        dry_run=dry_run,
        force=force,
    )


def _install_completions(
    completion_dir: Path,
    *,
    dry_run: bool,
    symlink: bool,
    force: bool,
) -> None:
    files = ["nssh.fish"]
    ui.show_panel(
        "Fish Completions",
        f"→ [green]{completion_dir}[/green]",
        style="cyan",
    )
    for name in files:
        _install_resource(
            "completions",
            name,
            completion_dir / name,
            executable=True,
            symlink=symlink,
            dry_run=dry_run,
            force=force,
        )


@app.command()
def install(
    bin_dir: Annotated[
        Path,
        typer.Option(
            "--bin-dir", help="Directory for executables (default: ~/.local/bin)"
        ),
    ] = DEFAULT_BIN_DIR,
    share_dir: Annotated[
        Path,
        typer.Option(
            "--share-dir",
            help="Directory for shared shell assets (default: ~/.local/share/nssh)",
        ),
    ] = DEFAULT_SHARE_DIR,
    shell_profile: Annotated[
        Optional[Path],
        typer.Option(
            "--shell-profile",
            help="Optional shell profile to append integration snippet",
        ),
    ] = None,
    fish_functions_dir: Annotated[
        Path,
        typer.Option(
            "--fish-functions-dir",
            help="Fish functions directory (default: ~/.config/fish/functions)",
        ),
    ] = DEFAULT_FISH_FUNCTIONS_DIR,
    fish_completions_dir: Annotated[
        Path,
        typer.Option(
            "--fish-completions-dir",
            help="Fish completions directory (default: ~/.config/fish/completions)",
        ),
    ] = DEFAULT_COMPLETIONS_DIR,
    install_fish: Annotated[
        bool,
        typer.Option(
            "--install-fish/--no-install-fish", help="Install fish function script"
        ),
    ] = True,
    install_completions: Annotated[
        bool,
        typer.Option(
            "--install-completions/--no-install-completions",
            help="Install fish completion files",
        ),
    ] = True,
    symlink: Annotated[
        bool,
        typer.Option(
            "--symlink/--copy",
            help="Symlink assets instead of copying (default: copy)",
        ),
    ] = False,
    dry_run: Annotated[
        bool,
        typer.Option("--dry-run/--no-dry-run", help="Preview actions without writing"),
    ] = False,
    force: Annotated[
        bool,
        typer.Option("-f", "--force", help="Overwrite files without prompting"),
    ] = False,
):
    """Install nssh wrapper + shell helpers into local paths."""

    ui.show_panel(
        "nssh install-shell",
        "Install nssh wrapper + shell helpers",
        style="cyan",
    )

    # Install wrapper + python shim into share dir and link from bin dir
    wrapper_share_path = share_dir / WRAPPER_FILENAME
    for script_name in [WRAPPER_FILENAME, "nssh-python-cli"]:
        _install_resource(
            "scripts",
            script_name,
            share_dir / script_name,
            executable=True,
            symlink=symlink,
            dry_run=dry_run,
            force=force,
        )
    _install_wrapper_link(
        wrapper_share_path,
        bin_dir / WRAPPER_TARGET,
        dry_run=dry_run,
        force=force,
    )

    # Install supporting scripts into share dir
    ui.show_panel(
        "Support Scripts",
        f"→ [green]{share_dir}[/green]",
        style="cyan",
    )
    for helper in [
        "nssh-shell-integration.sh",
        "nssh-shell-integration.fish",
        "nssh-timer.sh",
        "install-fish-completions.sh",
    ]:
        _install_resource(
            "scripts",
            helper,
            share_dir / helper,
            executable=True,
            symlink=symlink,
            dry_run=dry_run,
            force=force,
        )

    if shell_profile is not None:
        ui.show_panel(
            "Shell Profile",
            f"→ [green]{shell_profile.expanduser()}[/green]",
            style="cyan",
        )
        _append_profile_snippet(shell_profile.expanduser(), share_dir, dry_run)
    else:
        ui.show_panel(
            "Pro Tip",
            "Pass --shell-profile ~/.bashrc to append the sourcing snippet",
            style="yellow",
        )

    if install_fish:
        _install_fish_function(
            fish_functions_dir.expanduser(),
            share_dir,
            dry_run=dry_run,
            symlink=symlink,
            force=force,
        )

    if install_completions:
        _install_completions(
            fish_completions_dir.expanduser(),
            dry_run=dry_run,
            symlink=symlink,
            force=force,
        )

    ui.show_panel(
        "Installation Complete",
        "Ensure your PATH includes the bin directory and restart your shell.",
        style="green",
    )


APP_TITLE = "nssh install-shell"
APP_SUBTITLE = "Install nssh wrapper + shell helpers into local paths"


def _usage_sections() -> list[UsageSection]:
    return [
        UsageSection(
            "Usage",
            rows=[
                UsageRow(
                    "nssh install-shell [OPTIONS]",
                    "Install the wrapper script plus shell helpers into local paths",
                )
            ],
        ),
        UsageSection(
            "Options",
            rows=[
                UsageRow(
                    "--bin-dir PATH",
                    "Directory for executables (default: ~/.local/bin)",
                ),
                UsageRow(
                    "--share-dir PATH",
                    "Directory for shared shell assets (default: ~/.local/share/nssh)",
                ),
                UsageRow(
                    "--shell-profile PATH",
                    "Optional shell profile to append integration snippet",
                ),
                UsageRow(
                    "--fish-functions-dir PATH",
                    "Fish functions directory (default: ~/.config/fish/functions)",
                ),
                UsageRow(
                    "--fish-completions-dir PATH",
                    "Fish completions directory (default: ~/.config/fish/completions)",
                ),
                UsageRow(
                    "--install-fish / --no-install-fish",
                    "Toggle installing the fish function script (default: install)",
                ),
                UsageRow(
                    "--install-completions / --no-install-completions",
                    "Toggle installing fish completion files (default: install)",
                ),
                UsageRow(
                    "--symlink / --copy",
                    "Symlink assets instead of copying (default: copy)",
                ),
                UsageRow(
                    "--dry-run / --no-dry-run",
                    "Preview actions without writing (default: no-dry-run)",
                ),
                UsageRow(
                    "-f, --force / --no-force",
                    "Overwrite files without prompting (default: no-force)",
                ),
                UsageRow("-h, --help", "Show this message and exit"),
                UsageRow("-v, -V, --version", "Show version and exit"),
            ],
        ),
    ]


def print_usage():
    """Print usage information"""
    render_usage(APP_TITLE, APP_SUBTITLE, _usage_sections())


def main(argv: Sequence[str] | None = None) -> None:
    run_cli(
        app,
        cli_name=APP_TITLE,
        usage_cb=print_usage,
        completion_prefix="INSTALL_SHELL",
        argv=argv,
        show_usage_if_no_args=False,
    )


def cli_main() -> None:
    """Entry point for python -m usage."""
    main()


if __name__ == "__main__":  # pragma: no cover
    cli_main()
