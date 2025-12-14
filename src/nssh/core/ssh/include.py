"""Pure helpers to plan/apply inclusion of ~/.ssh/conf.d in SSH config."""

from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
from typing import Callable, Optional

from nssh.core.env.system import set_secure_permissions


def _has_include(lines: list[str], include_dir: Path) -> bool:
    include_pattern = str(include_dir / "*")
    for line in lines:
        stripped = line.strip()
        if not stripped or stripped.startswith("#"):
            continue
        if stripped.lower().startswith("include ") and (
            str(include_dir) in stripped or include_pattern in stripped
        ):
            return True
    return False


def _insertion_index(lines: list[str]) -> int:
    idx = 0
    while idx < len(lines) and lines[idx].strip().startswith("#"):
        idx += 1
    while idx < len(lines) and lines[idx].strip() == "":
        idx += 1
    return idx


@dataclass(frozen=True)
class IncludePlan:
    """Planned mutations for adding conf.d include."""

    changed: bool
    created: bool
    insert_index: int
    original_lines: list[str]
    new_lines: list[str]


def plan_conf_d_include(
    config_path: Path, include_dir: Path, *, create_if_missing: bool = False
) -> Optional[IncludePlan]:
    """
    Compute changes needed to ensure `Include include_dir/*` exists.

    Returns None if config is missing and creation is not allowed.
    """
    include_pattern = str(include_dir / "*")

    if not config_path.exists():
        if not create_if_missing:
            return None
        new_lines = [
            "# SSH configuration",
            "# Managed include directory",
            f"Include {include_pattern}",
            "",
            "Host *",
            "  ServerAliveInterval 60",
            "",
        ]
        return IncludePlan(
            changed=True,
            created=True,
            insert_index=2,
            original_lines=[],
            new_lines=new_lines,
        )

    lines = config_path.read_text().splitlines()
    if _has_include(lines, include_dir):
        return IncludePlan(
            changed=False,
            created=False,
            insert_index=-1,
            original_lines=lines,
            new_lines=lines,
        )

    idx = _insertion_index(lines)
    new_lines = lines[:idx] + [f"Include {include_pattern}"] + lines[idx:]
    if not new_lines or new_lines[-1] != "":
        new_lines.append("")

    return IncludePlan(
        changed=True,
        created=False,
        insert_index=idx,
        original_lines=lines,
        new_lines=new_lines,
    )


def apply_conf_d_include_plan(
    plan: IncludePlan,
    config_path: Path,
    *,
    backup_func: Optional[Callable[[Path], Path]] = None,
) -> Optional[Path]:
    """
    Apply a computed plan to disk, returning backup path if one was created.
    """
    backup_path: Optional[Path] = None
    if backup_func and not plan.created and config_path.exists():
        backup_path = backup_func(config_path)

    text = "\n".join(plan.new_lines)
    if not text.endswith("\n"):
        text += "\n"

    config_path.parent.mkdir(parents=True, exist_ok=True)
    config_path.write_text(text)
    set_secure_permissions(config_path)
    return backup_path
