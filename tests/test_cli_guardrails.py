"""Ensure CLI modules route prompts/panels through shared helpers."""

from __future__ import annotations

import ast
from pathlib import Path

PROJECT_ROOT = Path(__file__).resolve().parents[1]
CLI_ROOT = PROJECT_ROOT / "src" / "nssh" / "cli"
ALLOWED_SUBTREES = {CLI_ROOT / "common"}
ALLOWLIST = {CLI_ROOT / "__init__.py"}

FORBIDDEN_IMPORTS = {
    "nssh.cli": {"Prompt", "Confirm", "Panel"},
    "rich.prompt": {"Prompt", "Confirm"},
    "rich.panel": {"Panel"},
}
FORBIDDEN_FZF_SYMBOLS = {"fzf_select", "check_fzf"}
REQUIRED_HELPER_CALLS = {
    CLI_ROOT / "host" / "__init__.py": ("render_usage(", "run_cli("),
    CLI_ROOT / "ctx" / "__init__.py": ("render_usage(", "run_cli("),
    CLI_ROOT / "log" / "__init__.py": ("render_usage(", "run_cli("),
    CLI_ROOT / "benchmark" / "__init__.py": ("render_usage(", "run_cli("),
    CLI_ROOT / "self" / "__init__.py": ("render_usage(", "run_cli("),
}


def _is_allowed(path: Path) -> bool:
    if path in ALLOWLIST:
        return True
    return any(path.is_relative_to(subtree) for subtree in ALLOWED_SUBTREES)


def _iter_cli_files() -> list[Path]:
    return [path for path in CLI_ROOT.rglob("*.py") if not _is_allowed(path)]


def _collect_violations(path: Path) -> list[str]:
    violations: list[str] = []
    tree = ast.parse(path.read_text())
    rel = path.relative_to(PROJECT_ROOT)

    for node in ast.walk(tree):
        if isinstance(node, ast.ImportFrom):
            module = node.module or ""
            names = {alias.name for alias in node.names if alias.name != "*"}

            if module in FORBIDDEN_IMPORTS:
                blocked = names & FORBIDDEN_IMPORTS[module]
                for name in sorted(blocked):
                    violations.append(
                        f"{rel}:{node.lineno} imports {name} from {module} (use cli.common)"
                    )

            if module == "nssh.core.fzf":
                blocked_utils = names & FORBIDDEN_FZF_SYMBOLS
                for name in sorted(blocked_utils):
                    violations.append(
                        f"{rel}:{node.lineno} imports {name} from core.fzf (use cli.common.selectors)"
                    )

        if isinstance(node, ast.Import):
            for alias in node.names:
                if alias.name in {"rich.prompt", "rich.panel"}:
                    violations.append(
                        f"{rel}:{node.lineno} imports {alias.name} directly (use cli.common)"
                    )

    return violations


def _check_usage_helpers() -> list[str]:
    missing: list[str] = []
    for path, markers in REQUIRED_HELPER_CALLS.items():
        text = path.read_text()
        rel = path.relative_to(PROJECT_ROOT)
        for marker in markers:
            if marker not in text:
                missing.append(f"{rel} missing helper call: {marker[:-1]}")
    return missing


def test_cli_modules_use_shared_prompt_helpers() -> None:
    violations: list[str] = []
    for file_path in _iter_cli_files():
        violations.extend(_collect_violations(file_path))
    violations.extend(_check_usage_helpers())

    if violations:
        joined = "\n".join(violations)
        raise AssertionError(f"CLI guardrail violations:\n{joined}")
