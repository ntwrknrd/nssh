"""Shared usage/help rendering utilities for nssh CLI packages."""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import TYPE_CHECKING, Sequence

from rich.console import RenderableType
from rich.padding import Padding
from rich.panel import Panel
from rich.text import Text

from nssh.cli.common import ui
from nssh.core.ui.console import get_console

if TYPE_CHECKING:
    from nssh.cli import click

# Fixed column width for alignment across all commands
LABEL_COL_WIDTH = 36
# Maximum description width to prevent overly wide panels
MAX_DESCRIPTION_WIDTH = 45


@dataclass(slots=True)
class UsageRow:
    """One labeled row inside a usage section."""

    label: str
    description: RenderableType | None = None
    examples: Sequence[str] = field(default_factory=list)
    description_style: str | None = "dim"
    example_prefix: str = "Example"


@dataclass(slots=True)
class OptionRow:
    """One option inside an options panel."""

    label: str
    description: str


@dataclass(slots=True)
class OptionsPanel:
    """Container for options displayed in a bordered panel."""

    options: Sequence[OptionRow] = field(default_factory=list)
    title: str = "Options"


@dataclass(slots=True)
class UsageSection:
    """Container for a titled set of usage rows."""

    title: str
    rows: Sequence[UsageRow] = field(default_factory=list)
    body: RenderableType | None = None
    body_style: str | None = None
    inline: bool = False  # If True, render label and description on same line


def _to_text(content: str, *, style: str | None = None) -> Text:
    text = Text.from_markup(content)
    if style:
        text.stylize(style)
    return text


def _strip_markup(text: str) -> str:
    """Remove Rich markup tags to get plain text length.

    Only strips tags that look like Rich style tags (e.g., [bold], [/bold], [dim italic])
    but preserves text that looks like CLI arguments (e.g., [OPTIONS], [HOST]).
    Also converts escaped brackets \\[ to [ for accurate length calculation.
    """
    import re

    # First, protect escaped brackets by converting to placeholder
    result = text.replace("\\[", "\x00LBRACKET\x00")
    # Match Rich style tags: [style] or [/style] where style is lowercase with optional spaces
    # This preserves uppercase placeholders like [OPTIONS], [HOST_OR_FILE]
    result = re.sub(r"\[/?[a-z][a-z ]*\]", "", result)
    # Restore escaped brackets as literal [
    result = result.replace("\x00LBRACKET\x00", "[")
    return result


def _truncate_description(text: str, max_width: int = MAX_DESCRIPTION_WIDTH) -> str:
    """Truncate description to max width with ellipsis."""
    if len(text) <= max_width:
        return text
    return text[: max_width - 3] + "..."


def _build_options_lines(panel: OptionsPanel) -> list[str]:
    """Build lines for options panel content."""
    lines: list[str] = []
    for opt in panel.options:
        label_plain = _strip_markup(opt.label)
        padding = " " * max(2, LABEL_COL_WIDTH - len(label_plain))
        desc = _truncate_description(opt.description)
        lines.append(f"  {opt.label}{padding}[dim]{desc}[/dim]")
    return lines


def _render_panel(
    title: str, lines: list[str], *, content_width: int | None = None
) -> Panel:
    """Create a panel with consistent styling.

    Args:
        title: Panel title.
        lines: Content lines (with markup).
        content_width: If provided, pad lines to this width for consistent panel sizing.
    """
    # Pad lines to content_width if specified
    if content_width:
        padded_lines = []
        for line in lines:
            plain_len = len(_strip_markup(line))
            if plain_len < content_width:
                padded_lines.append(line + " " * (content_width - plain_len))
            else:
                padded_lines.append(line)
        lines = padded_lines

    content = Text.from_markup("\n".join(lines))
    content.no_wrap = True
    content.overflow = "ellipsis"

    return Panel(
        content,
        title=f"[bold]{title}[/bold]",
        title_align="left",
        border_style="dim",
        padding=(0, 1),
        expand=False,
    )


def _build_usage_lines(section: UsageSection) -> list[str]:
    """Build lines for usage section content."""
    lines: list[str] = []
    for row in section.rows:
        if row.label and row.description:
            label_plain = _strip_markup(row.label)
            padding = " " * max(2, LABEL_COL_WIDTH - len(label_plain))
            if isinstance(row.description, str):
                desc = _truncate_description(row.description)
                style = row.description_style or ""
                if style:
                    lines.append(f"  {row.label}{padding}[{style}]{desc}[/{style}]")
                else:
                    lines.append(f"  {row.label}{padding}{desc}")
        elif row.label:
            lines.append(f"  {row.label}")
    return lines


def _max_line_width(lines: list[str]) -> int:
    """Calculate max plain-text width of lines (excluding markup)."""
    return max(len(_strip_markup(line)) for line in lines) if lines else 0


def _render_section_stacked(section: UsageSection) -> None:
    """Render a section with description on a separate line (original behavior)."""
    console = get_console()

    if section.title:
        console.print(f"[bold]{section.title}:[/bold]")

    if section.body is not None:
        content: RenderableType
        if isinstance(section.body, str):
            content = _to_text(section.body, style=section.body_style)
        else:
            content = section.body
        console.print(Padding(content, (0, 0, 0, 2)))

    for row in section.rows:
        if row.label:
            console.print(f"  {row.label}")
        if row.description is not None:
            if isinstance(row.description, str):
                console.print(
                    Padding(
                        _to_text(row.description, style=row.description_style),
                        (0, 0, 0, 4),
                    )
                )
            else:
                console.print(Padding(row.description, (0, 0, 0, 4)))
        for example in row.examples:
            if row.example_prefix:
                text = f"{row.example_prefix}: {example}"
            else:
                text = example
            console.print(Padding(_to_text(text, style="dim"), (0, 0, 0, 4)))


def render_usage(
    app_title: str,
    subtitle: str,
    sections: Sequence[UsageSection],
    *,
    options_panel: OptionsPanel | None = None,
    footer: RenderableType | None = None,
    show_banner: bool = True,
) -> None:
    """Render the shared Rich layout for CLI help output."""
    console = get_console()

    if show_banner:
        ui.show_banner(subtitle)
    else:
        console.print()  # Add blank line before first panel when no banner

    # Build all panel content first to calculate consistent width
    usage_lines: list[str] = []
    usage_title = "Usage"
    for section in sections:
        if section.title in ("Commands", "Usage"):
            usage_title = "Usage" if section.title == "Commands" else section.title
            usage_lines = _build_usage_lines(section)
        else:
            _render_section_stacked(section)

    options_lines: list[str] = []
    if options_panel and options_panel.options:
        options_lines = _build_options_lines(options_panel)

    # Calculate max content width across both panels
    max_content_width = max(
        _max_line_width(usage_lines),
        _max_line_width(options_lines),
    )

    # Render panels with consistent width by padding content
    if usage_lines:
        console.print(
            _render_panel(usage_title, usage_lines, content_width=max_content_width)
        )

    if options_lines and options_panel:
        console.print()
        console.print(
            _render_panel(
                options_panel.title, options_lines, content_width=max_content_width
            )
        )

    if footer:
        console.print(f"\n{footer}")


def build_options_panel(cmd: "click.Command") -> OptionsPanel:
    """Build options panel by introspecting a Click command.

    Args:
        cmd: Click Command object to introspect.

    Returns:
        OptionsPanel with options extracted from the command.
    """
    from nssh.cli import click as _click

    rows: list[OptionRow] = []
    for param in cmd.params:
        if isinstance(param, _click.Option):
            # Skip --help, we add it manually at the end
            if param.name == "help":
                continue
            label = _format_option_label(param)
            help_text = param.help or ""
            rows.append(OptionRow(label, help_text))

    # Add --help at the end with hint about command-specific options
    rows.append(OptionRow("--help, -h", "Print command-specific help"))

    return OptionsPanel(options=rows)


def _format_option_label(param: "click.Option") -> str:
    """Format a Click Option into a display label.

    Produces labels like:
        --select, -s PATTERN
        --dry-run
        --yes, -y
    """
    # Sort opts: long form first, then short
    opts = sorted(param.opts, key=lambda x: (len(x), x), reverse=True)

    # Build the base label (e.g., "--select, -s")
    label = ", ".join(opts)

    # Add metavar for non-flag options
    if not param.is_flag:
        metavar = param.metavar or param.type.name.upper()
        if metavar and metavar != "TEXT":
            label = f"{label} {metavar}"

    return label


def render_command_help(
    cmd: "click.Command",
    cmd_name: str,
    parent_title: str,
    parent_subtitle: str,
) -> None:
    """Render styled help for a subcommand.

    Args:
        cmd: Click Command object.
        cmd_name: Command name (e.g., "list").
        parent_title: Parent group title (e.g., "nssh host").
        parent_subtitle: Parent group subtitle.
    """
    from nssh.cli import click as _click

    # Build usage label
    usage_label = f"{parent_title} [bold]{cmd_name}[/bold]"
    for param in cmd.params:
        if isinstance(param, _click.Argument):
            metavar = param.metavar or param.name.upper()
            if param.required:
                usage_label += f" {metavar}"
            else:
                usage_label += f" [{metavar}]"

    description = cmd.short_help or cmd.help or ""
    section = UsageSection("Usage", rows=[UsageRow(usage_label, description)])

    render_usage(
        parent_title,
        parent_subtitle,
        [section],
        options_panel=build_options_panel(cmd),
        show_banner=False,
    )


def styled_group(
    name: str | None = None,
    styled_title: str = "",
    styled_subtitle: str = "",
    **kwargs,
):
    """Decorator to create a Click group with styled subcommand help.

    Usage:
        @styled_group(styled_title="nssh host", styled_subtitle="Manage SSH hosts")
        def app():
            pass

    Subcommands will automatically get styled help when --help is invoked.
    """
    from nssh.cli import click as _click

    class StyledGroup(_click.Group):
        """Click Group that renders styled help for subcommands."""

        def __init__(self, *args, **kw):
            super().__init__(*args, **kw)
            self._styled_title = styled_title
            self._styled_subtitle = styled_subtitle

        def get_command(self, ctx, cmd_name):
            """Wrap command to inject styled help."""
            cmd = super().get_command(ctx, cmd_name)
            if cmd is not None:
                # Ensure -h works as --help alias
                cmd.context_settings = cmd.context_settings or {}
                cmd.context_settings.setdefault("help_option_names", ["-h", "--help"])

                # Wrap the command's format_help
                def styled_format_help(ctx, formatter):
                    # Render our styled help instead
                    render_command_help(
                        cmd,
                        cmd_name,
                        self._styled_title,
                        self._styled_subtitle,
                    )

                cmd.format_help = styled_format_help
            return cmd

    kwargs.setdefault("cls", StyledGroup)
    # Enable -h for the group itself
    context_settings = kwargs.get("context_settings", {})
    context_settings.setdefault("help_option_names", ["-h", "--help"])
    kwargs["context_settings"] = context_settings
    return _click.group(name=name, **kwargs)


def build_usage_sections(
    app: "click.Group",
    cli_prefix: str,
) -> list[UsageSection]:
    """Build usage sections by introspecting Click group commands.

    Args:
        app: Click group to introspect.
        cli_prefix: Command prefix for display (e.g., "nssh log").

    Returns:
        List containing a single UsageSection with command rows.

    Note:
        Commands should define `short_help` for concise descriptions.
        Falls back to truncated `help` (docstring) if not provided.
    """
    from nssh.cli import click as _click

    rows: list[UsageRow] = []

    for name in sorted(app.commands.keys()):
        cmd = app.commands[name]

        # Build label: "nssh log [bold]play[/bold]"
        label = f"{cli_prefix} [bold]{name}[/bold]"

        # Add argument placeholders (e.g., HOST, FILE)
        for param in cmd.params:
            if isinstance(param, _click.Argument):
                metavar = param.metavar or param.name.upper()
                if param.required:
                    label += f" {metavar}"
                else:
                    label += f" [{metavar}]"

        # Use short_help if available, otherwise truncate help
        description = cmd.short_help or cmd.help or ""
        if len(description) > 60:
            description = description[:57] + "..."

        rows.append(UsageRow(label, description))

    return [UsageSection("Commands", rows=rows)]
