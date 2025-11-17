# Contributing to nssh

Thanks for helping improve nssh. This guide captures the contributor-focused details you'll need when building or testing changes.

## Table of Contents
- [Getting Started](#getting-started)
- [Developer Notes](#developer-notes)
- [Development Workflow](#development-workflow)
  - [Setup](#setup)
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
- [Additional References](#additional-references)

## Getting Started
1. Install prerequisites (OpenSSH, `sshpass`, `age`, `jq`, `fzf`, Python 3.14+, and `uv`).
2. Sync dependencies inside the repo:
   ```bash
   uv sync
   ```
3. Run the CLIs locally without installing:
   ```bash
   uv run nssh host list
   uv run nssh cred list-contexts
   ```
4. When you need the binaries on your PATH, reinstall with:
   ```bash
   uv tool install --force --reinstall .
   ```

## Developer Notes
- **Shared CLI toolkit:** All Rich panels, prompts, `fzf` pickers, usage renderers, and prompt workflows live under `src/nssh/cli/common/` (`ui.py`, `prompt.py`, `selectors.py`, `help.py`, `app.py`, `workflows.py`). Import from these modules (instead of `rich.panel.Panel` or `nssh.core.fzf.fzf_select`) so every command inherits the same look-and-feel.
- **Host CLI tests:** Use Typer's `CliRunner` with the ready-made fixture in `tests/test_cli_host.py`. This test suite stubs `CredentialManager`, rewrites `HOME` into a temp directory, and lets you extend flows (add/list/rm/update) without touching real SSH configs. Run it with `uv run pytest tests/test_cli_host.py` or `uv run pytest` for the full matrix.

## Development Workflow
- Repo layout: Bash wrapper + shell helpers remain at the root; Python modules live under `src/nssh/` (`core/` for shared logic, `cli/` for Typer entry points).
- Work in feature branches, keep changes scoped, and stash experimental helpers inside `src/nssh/assets/completions/` or `build/` so the root stays tidy.
- Iterate with `uv run ...` for quick tests; reinstall via `uv tool install --force --reinstall .` only when verifying installed entry points.

### Setup

```bash
cd nssh  # adjust to your cloned path

# Install dependencies
uv sync

# Run CLI tools directly from source (uses local code, no installation needed)
uv run nssh cred list
uv run nssh host add --help
uv run nssh connect hostname
uv run nssh benchmark capture hostname
uv run nssh install-shell --help

# Only install when you need the binaries on PATH or want to test installed entry points
uv tool install --force --reinstall .
```

### Making Changes

1. Edit code:
   - Bash script: `nssh`
   - Python package: `src/nssh/`
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

Before submitting a PR, run these manual smoke tests to verify basic functionality:

```bash
# Credential flows
uv run nssh cred list-contexts
uv run nssh cred list

# Host management
uv run nssh host list
uv run nssh host list work

# Full connection flow (requires configured host)
uv run nssh test-host
```

### Automated Unit Tests

The project has comprehensive test coverage with 40+ tests across 7 test files:

```bash
# Run all tests
uv run pytest

# Run with verbose output
uv run pytest -v

# Run with coverage report
uv run pytest --cov=nssh --cov-report=term-missing

# Run specific test file
uv run pytest tests/test_benchmark_core.py

# Run tests matching a pattern
uv run pytest -k "credential"
```

**Test coverage:**
The test suite includes 10+ test files covering CLI help, credentials, SSH config parsing, connection flow, benchmarking, recording, and shell integration. See the `tests/` directory for the current test suite.

**CLI integration tests:** `tests/test_cli_host.py` uses Typer's `CliRunner` plus a temp `$HOME` fixture so you can exercise `nssh host` subcommands without touching a real SSH setup. Extend this file for new flows (password prompts, compat fixes, etc.) and run it directly via:

```bash
uv run pytest tests/test_cli_host.py
```

### Performance Benchmarking

For performance-sensitive changes, capture timing artifacts:

```bash
# Basic benchmark
uv run nssh benchmark capture hostname --warmups 1 --samples 3

# With budget enforcement
uv run nssh benchmark capture hostname \
  --stage-budget host-selection=150 \
  --total-budget 500 \
  --budget-metric max

# Generate JSON artifact for PR
uv run nssh benchmark capture hostname --samples 5 --json-output benchmark/hostname.json
```

**Include benchmark results in your PR if you modified:**
- Credential resolution logic
- SSH config parsing
- Host indexing
- Any performance-critical code path

For code architecture and module organization, see [ARCHITECTURE.md - Two-Layer Architecture](docs/ARCHITECTURE.md#two-layer-architecture).

## Troubleshooting and Debugging

For common user-facing issues (host not found, credential failures, connection problems), see [USER_GUIDE.md - Troubleshooting](docs/USER_GUIDE.md#troubleshooting).

For developer-specific debugging:
- **Testing Python modules directly**: `uv run python -m nssh.cli.host list`
- **Inspecting host index**: See [ARCHITECTURE.md - Debugging and Profiling](docs/ARCHITECTURE.md#debugging-and-profiling)
- **Testing changes before reinstalling**: Use `uv run nssh host`, `uv run nssh cred` commands
- **Performance profiling**: See [Performance Benchmarking](#performance-benchmarking)

## Coding Standards
- Target Python 3.14+ with full type hints on new public functions. Keep to PEP 8 spacing (4-space indents, ~100 character lines).
- CLI command names stay kebab-case (e.g., `add-context-cred`); internal helpers use descriptive snake_case like `prompt_required`.
- Prefer Rich panels/tables for human-facing output, and keep docstrings short and action-oriented.
- Use the shared CLI toolkit in `src/nssh/cli/common/`: `ui.show_panel`/`print_table` for Rich output, `prompt.ask_text`/`confirm` for questions, `selectors.select_via_fzf` for `fzf` prompts, `help.render_usage` for `--help`, `app.run_cli` for startup/KeyboardInterrupt handling, and `workflows.*` for multi-step confirmations. Adding new helpers there keeps styles consistent across commands.
- Maintain context-aware credential defaults: route new logic through the shared analyzers in `src/nssh/core/` instead of bespoke scripts.

## Adding CLI Commands
- Mirror the existing layout: each CLI lives in `src/nssh/cli/<command>/` with an `__main__.py` (or Typer `app`) plus per-command modules (e.g., `add.py`, `update.py`).
- Import Typer primitives from `nssh.cli`, but route all Rich prompts/panels/selectors through `cli/common/` (`prompt.py`, `ui.py`, `selectors.py`, `help.py`, `app.py`, `workflows.py`). Never import `rich.prompt.Prompt`, `rich.prompt.Confirm`, `rich.panel.Panel`, or `nssh.core.fzf.fzf_select` directly from a command module.
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

## Additional References
- Jump straight to [Testing Expectations](#testing-expectations) for the required test plan, or to [Security and Configuration](#security-and-configuration) for guardrails.
