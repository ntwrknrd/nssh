# Contributing to nssh

Thanks for helping improve nssh. This guide captures the contributor-focused details you'll need when building or testing changes.

## Table of Contents
- [Getting Started](#getting-started)
- [Developer Notes](#developer-notes)
- [Development Workflow](#development-workflow)
  - [Making Changes](#making-changes)
- [Testing Expectations](#testing-expectations)
  - [Manual Smoke Tests](#manual-smoke-tests)
  - [Automated Unit Tests](#automated-unit-tests)
  - [Performance Benchmarking](#performance-benchmarking)
- [Troubleshooting and Debugging](#troubleshooting-and-debugging)
- [Coding Standards](#coding-standards)
- [Adding CLI Commands](#adding-cli-commands)
- [Code Quality Tools](#code-quality-tools)
- [Commit and PR Guidelines](#commit-and-pr-guidelines)
- [Security and Configuration](#security-and-configuration)

## Getting Started

Prerequisites: See [README.md Installation](../README.md#installation).

**Developer workflow:**
```bash
uv sync                                    # Sync dependencies
uv run nssh host list                      # Run without installing
uv tool install --force --reinstall .      # Test installed entry points
```

## Developer Notes
- **Shared CLI toolkit:** Import from `src/nssh/cli/common/` (`ui.py`, `prompt.py`, `selectors.py`, etc.) for consistent look-and-feel across commands.
- **Host CLI tests:** Use `tests/test_cli_host.py` with temp `$HOME` fixture to test SSH config flows without touching real configs.

## Development Workflow

- Repo layout: Shell helpers live under `src/nssh/assets/`; Python modules live under `src/nssh/` (`core/` for shared logic, `cli/` for Click commands).
- Work in feature branches, keep changes scoped, and stash experimental helpers inside `src/nssh/assets/completions/` or `build/` so the root stays tidy.
- Iterate with `uv run ...` for quick tests; reinstall via `uv tool install --force --reinstall .` only when verifying installed entry points.

### Making Changes

1. Edit code:
   - Python CLI: `src/nssh/cli/`
   - Core modules: `src/nssh/core/`
   - Shell helpers: `src/nssh/assets/scripts/`
2. Test immediately—installed tools require a reinstall to pick up edits.
3. Clean caches when needed:
   ```bash
   find . -type d -name __pycache__ -exec rm -rf {} + 2>/dev/null
   ```
4. Reinstall to refresh binaries:
   ```bash
   uv tool install --force --reinstall .
   ```
5. Commit when ready:
   ```bash
   git add -A
   git commit -m "Description of changes"
   ```

## Testing Expectations

### Manual Smoke Tests

```bash
uv run nssh ctx list
uv run nssh host list
uv run nssh test-host
```

### Automated Unit Tests

18 test files (~2900 lines) covering CLI, credentials, SSH config, PTY connector, recording, benchmarking.

```bash
uv run pytest                              # All tests
uv run pytest -v                           # Verbose
uv run pytest --cov=nssh                   # Coverage
uv run pytest tests/test_benchmark_core.py # Specific file
uv run pytest -k "credential"              # Pattern match
```

### Performance Benchmarking

Include benchmark results if you modified credential resolution, config parsing, host indexing, or connection flow:

```bash
uv run nssh benchmark ssh hostname --warmups 1 --samples 3
```


## Troubleshooting and Debugging

For common user-facing issues (host not found, credential failures, connection problems), see [USER_GUIDE.md - Troubleshooting](docs/USER_GUIDE.md#troubleshooting).

For developer-specific debugging:
- **Testing Python modules directly**: `uv run python -m nssh.cli.host list`
- **Inspecting host index**: See [ARCHITECTURE.md - Debugging and Profiling](docs/ARCHITECTURE.md#debugging-and-profiling)
- **Testing changes before reinstalling**: Use `uv run nssh host`, `uv run nssh ctx` commands
- **Performance profiling**: See [Performance Benchmarking](#performance-benchmarking)

## Coding Standards
- Target Python 3.14+ with full type hints on new public functions. Keep to PEP 8 spacing (4-space indents, ~100 character lines).
- CLI command names use simple verbs (e.g., `add`, `get`, `list`, `rm`); internal helpers use descriptive snake_case like `prompt_required`.
- Prefer Rich panels/tables for human-facing output, and keep docstrings short and action-oriented.
- Use the shared CLI toolkit in `src/nssh/cli/common/`: `ui.show_panel`/`print_table` for Rich output, `prompt.ask_text`/`confirm` for questions, `selectors.fzf_select` for `fzf` prompts (supports `multi=True` for multi-select), `help.render_usage` for `--help`, `app.run_cli` for startup/KeyboardInterrupt handling, and `workflows.*` for multi-step confirmations. Adding new helpers there keeps styles consistent across commands.
- Maintain context-aware credential defaults: route new logic through the shared analyzers in `src/nssh/core/` instead of bespoke scripts.

## Adding CLI Commands
- Mirror the existing layout: each CLI lives in `src/nssh/cli/<command>/` with an `__main__.py` (or Click `group`) plus per-command modules (e.g., `add.py`, `edit.py`).
- Import Click primitives from `nssh.cli`, but route all Rich prompts/panels/selectors through `cli/common/` (`prompt.py`, `ui.py`, `selectors.py`, `help.py`, `app.py`, `workflows.py`). Never import `rich.prompt.Prompt`, `rich.prompt.Confirm`, `rich.panel.Panel`, or `nssh.core.fzf.fzf_select` directly from a command module.
- If a command needs a new UI primitive or workflow (credential confirmation flows, password sourcing, etc.), add it to `cli/common/` and reuse it everywhere—avoid inline prompt logic.
- Guardrails enforce this convention: `tests/test_cli_guardrails.py` fails if a CLI module bypasses the shared helpers. Update/extend that test if you add new shared primitives or new modules that need exemptions.

## Code Quality Tools

The project uses automated formatting and type checking:

```bash
# Format code with Black (4-space indents, ~100 char lines)
uv run black src/ tests/

# Lint with Ruff (fast Python linter)
uv run ruff check src/ tests/

# Type check with mypy (Python 3.14+)
uv run mypy

# Run all checks before committing
uv run black src/ tests/ && uv run ruff check src/ tests/ && uv run mypy
```

**Pre-commit checklist:**
- [ ] All tests pass: `uv run pytest`
- [ ] Code formatted: `uv run black src/ tests/`
- [ ] Linting passes: `uv run ruff check src/ tests/`
- [ ] Type checking passes: `uv run mypy`
- [ ] Manual smoke tests completed (see [Testing Expectations](#testing-expectations))

## Commit and PR Guidelines
- Follow concise, imperative commit subjects under 60 characters (e.g., `completion fixes`).
- PR descriptions must explain user-impacting changes, list manual test commands, include timing artifacts if performance is touched, and link issues (`Closes #123`) when relevant.
- Provide before/after screenshots for shell integration tweaks (fzf previews, Fish/Bash helpers, etc.).

## Security and Configuration
- Never commit decrypted credentials, `.nssh_host_index`, or age key material. Stub sensitive paths in tests via temp directories.
- Document new environment variables in [README.md](README.md) and/or `docs/` as part of your change.
- Default to prompting via `prompt_required` rather than accepting raw passwords on the command line.
