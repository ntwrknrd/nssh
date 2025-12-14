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

Prerequisites: See [README.md Installation](README.md#installation).

**Developer workflow:**
```bash
make build                                 # Build to ~/Downloads by default
make build-hardware                        # CGO build with YubiKey PIV support
go run ./cmd/nssh host list                # Run without installing
```

## Developer Notes
- **Shared UI toolkit:** Import from `internal/ui/` (`styles.go`, `prompts.go`, `panels.go`, etc.) for consistent look-and-feel across commands.
- **Repository layout:**
  - `cmd/nssh/`: CLI entrypoint + top-level wiring
  - `internal/cli/`: Cobra subcommands and CLI orchestration
  - `internal/ui/`: terminal UX primitives (styles, prompts, panels); should stay policy-light
  - `internal/vault/`: encrypted credential storage + resolution logic (core domain)
  - `internal/agent/`: background daemon that holds decrypted credentials and serves IPC
  - `internal/ssh/`: SSH subsystem (connector runtime, ssh config parsing/mutation, recording planning)
  - `internal/config/`, `internal/secret/`, `internal/exit/`: shared "leaf" packages
  - `internal/session/`: composition root for wiring vault↔agent dependencies (non-CLI)
- **Build tags:** Use `-tags hardware` for PIV/YubiKey code paths. Many runtime pieces are Unix-only (PTY/recording/agent socket).

## Development Workflow

- Work in feature branches, keep changes scoped, and maintain healthy dependency layering inside `internal/`.
- Iterate with `go run ./cmd/nssh ...` for quick tests; reinstall via `make build` only when verifying installed binaries.

### Layering Rules

Think in terms of "leaf → core → subsystems → CLI".

Leaf packages (keep small, broadly reusable):
- `internal/config` (settings + XDG paths)
- `internal/secret` (memguard-backed secret handling)
- `internal/exit` (typed exit codes/errors)
- `internal/session/mode` (canonical mode identifiers)
- `internal/ssh/compat` (pure helpers for legacy SSH compatibility)

Core domain:
- `internal/vault` should *not* depend on UX or agent/daemon packages.

Subsystems:
- `internal/agent` should not depend on CLI/UI or SSH subsystem packages.
- `internal/ssh` should not depend on vault/agent or CLI packages.

Top-level wiring:
- `cmd/nssh` and `internal/cli` are allowed to import the subsystems and wire them together.
- `internal/session` is the non-CLI wiring point: it may import `internal/vault` and `internal/agent`, but should not import `internal/ui`, Cobra, or `golang.org/x/term`.

Guardrails enforce these rules via tests:
- `internal/vault/imports_test.go`
- `internal/session/imports_test.go`
- `internal/agent/imports_test.go`
- `internal/ssh/compat/imports_test.go`

If you introduce a new cross-package dependency, prefer:
1. moving the shared type/helper into an existing leaf package, or
2. adding a new small leaf package with explicit import-boundary tests.

### Making Changes

1. Edit code:
   - CLI commands: `internal/cli/`
   - Vault/credentials: `internal/vault/`
   - Agent daemon: `internal/agent/`
   - SSH subsystem: `internal/ssh/`
   - Shared utilities: `internal/{config,secret,exit,ui}`
2. Test immediately—installed binaries require a rebuild to pick up edits.
3. Format and vet when needed:
   ```bash
   gofmt -w .
   go vet ./...
   ```
4. Rebuild to refresh binaries:
   ```bash
   make build
   ```
5. Commit when ready:
   ```bash
   git add -A
   git commit -m "Description of changes"
   ```

## Testing Expectations

### Manual Smoke Tests

```bash
go run ./cmd/nssh ctx list
go run ./cmd/nssh host list
go run ./cmd/nssh self status
```

### Automated Unit Tests

Tests covering CLI, vault, agent, SSH connector, recording, config parsing, and import boundaries.

```bash
go test ./...                              # All tests
go test -v ./...                           # Verbose
go test ./internal/vault/...               # Specific package
go test -run TestFoo ./...                 # Pattern match
go test -tags hardware ./...               # Tests requiring PIV hardware
```

### Performance Benchmarking

Include benchmark results if you modified credential resolution, config parsing, host indexing, or connection flow:

```bash
go test -bench=. -benchmem ./internal/vault/...
go test -bench=. -benchmem ./internal/ssh/...
```

## Troubleshooting and Debugging

For common user-facing issues (host not found, credential failures, connection problems), see [USER_GUIDE.md - Troubleshooting](docs/USER_GUIDE.md#troubleshooting).

For developer-specific debugging:
- **Testing Go modules directly**: `go run ./cmd/nssh host list`
- **Inspecting agent state**: See [ARCHITECTURE.md - Agent Daemon](docs/ARCHITECTURE.md#agent-daemon-architecture)
- **Testing changes before rebuilding**: Use `go run ./cmd/nssh ...` commands
- **Performance profiling**: See [Performance Benchmarking](#performance-benchmarking)

## Coding Standards
- Target Go 1.25+ with clean, idiomatic Go. Follow standard gofmt spacing and keep to ~100 character lines.
- CLI command names use simple verbs (e.g., `add`, `get`, `list`, `rm`); internal helpers use descriptive names following Go conventions.
- Prefer `internal/ui` panels/tables for human-facing output, and keep comments short and action-oriented.
- Use the shared UI toolkit in `internal/ui/`: `ShowPanel`/`PrintTable` for formatted output, `PromptText`/`Confirm` for questions, `FzfSelect` for `fzf` prompts (supports multi-select), and shared styles from `styles.go`. Adding new helpers there keeps styles consistent across commands.
- Maintain context-aware credential defaults: route new logic through the shared resolvers in `internal/vault/` instead of bespoke scripts.
- Handle secrets securely: Use `internal/secret.Secret` for password storage (memguard-protected). Always call `.Destroy()` when done. Access via `.UseBytes()` or `.UseString()` callbacks.

## Adding CLI Commands
- Mirror the existing layout: each CLI lives in `internal/cli/<command>/` with a `cmd.go` (Cobra command definition) plus per-command modules.
- Import Cobra primitives directly, but route all UI output/prompts through `internal/ui/` (`prompts.go`, `panels.go`, `tables.go`, `styles.go`, `fzf.go`). Never import `fmt.Printf`, `os.Stdin`, or build panels/prompts inline in command modules.
- If a command needs a new UI primitive or workflow (credential confirmation flows, password sourcing, etc.), add it to `internal/ui/` and reuse it everywhere—avoid inline prompt logic.
- Guardrails enforce this convention: import tests in `internal/{vault,agent,session}/imports_test.go` fail if you bypass the layering rules. Update/extend those tests if you add new shared primitives or modules that need exemptions.

## Code Quality Tools

The project uses automated formatting and linting:

```bash
# Format code with gofmt
gofmt -w .

# Vet code
go vet ./...

# Lint with golangci-lint (see .golangci.yml)
golangci-lint run

# Run all checks before committing
gofmt -w . && go vet ./... && golangci-lint run
```

**Pre-commit checklist:**
- [ ] All tests pass: `go test ./...`
- [ ] Code formatted: `gofmt -w .`
- [ ] Vetting passes: `go vet ./...`
- [ ] Linting passes: `golangci-lint run`
- [ ] Manual smoke tests completed (see [Testing Expectations](#testing-expectations))

## Commit and PR Guidelines
- Follow concise, imperative commit subjects under 60 characters (e.g., `fix credential resolution panic`).
- PR descriptions must explain user-impacting changes, list manual test commands, include timing artifacts if performance is touched, and link issues (`Closes #123`) when relevant.
- Provide before/after screenshots for shell integration tweaks (fzf previews, Fish/Bash/Zsh helpers, etc.).

## Security and Configuration
- Never commit decrypted credentials, vault data, or age key material. Stub sensitive paths in tests via temp directories.
- Document new environment variables in [README.md](README.md) and/or `docs/` as part of your change.
- Default to prompting via `internal/ui` helpers rather than accepting raw passwords on the command line.
- Use `internal/secret.Secret` for all password/key material handling. Never store sensitive data in plain strings.
