# AGENTS.md

This file provides guidance to AI coding agents when working with code in this repository.

## Build Commands

```bash
# Standard build
make build

# Cross-compile for Linux
make linux
```

## Testing

```bash
# Run all tests
make test

# Run a single test
go test ./internal/agent -run TestDaemon

# Update help snapshots after CLI changes
go test ./cmd/nssh -args -update-snapshots
```

## Linting

```bash
golangci-lint run ./...
```

## Releases

Use `gh release create` to tag and create releases with notes (avoids opening an editor):

```bash
gh release create v0.2.4 --title "v0.2.4" --notes "## What's New
- Feature 1
- Feature 2"
```

## Architecture Overview

nssh is an SSH wrapper providing inventory management, provider-backed credential resolution, automatic password injection, and session recording.

### Package Structure

- `cmd/nssh/main.go` - Small entry point that delegates to `internal/app`
- `internal/app` - Cobra root command, argument preprocessing for smart routing, and process exit handling
- `internal/config` - TOML config loading, include merging, defaults, validation, and XDG paths
- `internal/credential` - Provider registry and Pass, 1Password, and Bitwarden credential resolvers
- `internal/agent` - Background runtime for provider-session brokering, Unix socket IPC, recording archival, and idle/lifetime timeouts
- `internal/connect` - Shared SSH/SCP host lookup, inventory group resolution, and credential selection
- `internal/ssh/connector` - PTY-based SSH execution, prompt detection (password, host-key), timing instrumentation
- `internal/ssh/sshconfig` - SSH config parsing, include file discovery, host matching, mutations
- `internal/recording` - Asciinema planning, session locking, metadata, and archive policy
- `internal/ssh/compat` - Legacy SSH algorithm detection and remediation
- `internal/secret` - Memguard-protected request-scoped password handling
- `internal/inventory` - Local and external inventory provider state, route reconciliation, and generated SSH config
- `internal/cli/*` - Subcommand implementations (`inv`, `agent`, `log`, `cp`, `self`)

### Key Design Patterns

**Argument Preprocessing**: `preprocessArgs()` in main.go transforms `nssh hostname` to `nssh smart-connect hostname`, separating global flags from SSH passthrough flags.

**Provider-Backed Credentials**: Password managers own storage and authentication. nssh stores only host/group auth mappings, resolves a provider record at connect time, and keeps the resulting secret request-scoped.

**Agent Runtime**: The agent is not a password cache. It brokers agent-owned provider sessions, serves status/stop requests over a Unix socket, and hosts background recording archive work.

**PTY Connector**: Ring buffer with tiered pattern matching (suffix -> contains -> regex) for prompt detection. Passwords held as `*secret.Secret`, accessed via `secret.Use()`.

**Recording Wrapper**: Outer nssh spawns asciinema around inner nssh (guarded by `NSSH_RECORDING_INNER=1` env var).

### Exit Codes

Centralized in `internal/exit/exit.go`: 0=success, 1=general, 2=connection, 3=auth, 4=host not found, 126=not executable, 127=not found.

### Debugging

- `-v` / `--verbose` for debug logs
- `NSSH_DEBUG=1` emits `NSSH_TIMING:*` markers to stderr
- `nssh self bench ssh myhost` for connection benchmarks
