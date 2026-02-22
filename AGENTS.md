# AGENTS.md

This file provides guidance to AI coding agents when working with code in this repository.

## Build Commands

```bash
# Standard build (software-only, no CGO)
make build

# Hardware-enabled build (requires PC/SC: libpcsclite-dev on Linux)
make build-hardware

# Cross-compile for Linux
make linux

# Verify both builds succeed
make verify-builds
```

## Testing

```bash
# Run all tests
make test

# Run hardware-enabled tests
make test-hardware

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

nssh is an SSH wrapper providing host management, encrypted credential storage, automatic password injection, and session recording.

### Package Structure

- `cmd/nssh/main.go` - Entry point, Cobra CLI setup, argument preprocessing for smart routing
- `internal/config` - TOML config loading, XDG paths (`~/.config/nssh/`, `~/.local/share/nssh/`, `~/.local/state/nssh/`)
- `internal/vault` - Age-encrypted credential vault, resolution logic (by include-file, domain, host)
- `internal/agent` - Background daemon holding decrypted credentials, Unix socket IPC, idle/lifetime timeouts
- `internal/ssh/connector` - PTY-based SSH execution, prompt detection (password, host-key), timing instrumentation
- `internal/ssh/sshconfig` - SSH config parsing, include file discovery, host matching, mutations
- `internal/ssh/recording` - Asciinema integration, session locking, index metadata
- `internal/ssh/compat` - Legacy SSH algorithm detection and remediation
- `internal/secret` - Memguard-protected secrets that panic on string formatting
- `internal/cli/*` - Subcommand implementations (host, ctx, log, cp, self, lock, unlock)

### Key Design Patterns

**Argument Preprocessing**: `preprocessArgs()` in main.go transforms `nssh hostname` to `nssh smart-connect hostname`, separating global flags from SSH passthrough flags.

**Two-Layer Credential System**: Vault owns encrypted on-disk data; Agent holds decrypted session in memory via Unix socket. Commands can run with vault locked (will prompt or proceed without passwords).

**Build Tags**: Hardware features (YubiKey PIV) use `hardware` build tag. Stub files (`*_stub.go`) provide no-op implementations for standard builds.

**PTY Connector**: Ring buffer with tiered pattern matching (suffix -> contains -> regex) for prompt detection. Passwords held as `*secret.Secret`, accessed via `secret.Use()`.

**Recording Wrapper**: Outer nssh spawns asciinema around inner nssh (guarded by `NSSH_RECORDING_INNER=1` env var).

### Exit Codes

Centralized in `internal/exit/exit.go`: 0=success, 1=general, 2=connection, 3=auth, 4=host not found, 5=vault.

### Debugging

- `-v` / `--verbose` for debug logs
- `NSSH_DEBUG=1` emits `NSSH_TIMING:*` markers to stderr
- `nssh self bench ssh myhost` for connection benchmarks
