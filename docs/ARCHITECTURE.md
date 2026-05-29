# nssh Architecture & Technical Design

This document is for developers working on `nssh`. It focuses on the internal
package layout, connection orchestration, vault + agent behavior, and the
implementation details behind features like password injection, host-key
workflows, and session recording.

For end-user usage and configuration examples, see [USER_GUIDE.md](USER_GUIDE.md).

## Overview

`nssh` wraps OpenSSH tooling with smart host resolution, encrypted credentials, PTY-based prompt handling, session recording, and SSH config management.

This document covers internal architecture, subsystem boundaries, connection flows, and implementation details for developers.

## Table of Contents

- [Connection Architecture](#connection-architecture) - CLI routing, config, vault, agent, connector
- [PTY Connector Architecture](#pty-connector-architecture) - SSH execution, prompt detection, host-key UX
- [SCP Connector Architecture](#scp-connector-architecture) - `nssh cp`, remote spec parsing, password injection
- [Connection Flow](#connection-flow) - Smart connect routing, recording wrapper, retry loops
- [SSH Compatibility Detection and Remediation](#ssh-compatibility-detection-and-remediation)
- [Credential Vault and Resolution](#credential-vault-and-resolution) - data model, unlock policy, resolution rules
- [Recording System Architecture](#recording-system-architecture) - planning, locks, index metadata, CLI integration
- [API Reference](#api-reference) - internal Go packages, key types, IPC protocol
- [SSH Config Implementation](#ssh-config-implementation) - include discovery, parsing, mutation, sorting
- [Debugging and Profiling](#debugging-and-profiling) - timing, audit logs, benchmarks, troubleshooting

## Connection Architecture

### CLI Entry Point and Routing

The binary entry point is `cmd/nssh/main.go`. The root command is built with
Cobra and supports two connection paths:

- `nssh connect <host> [ssh-args...]` (explicit, bypasses smart matching)
- `nssh <host> [ssh-args...]` (default, routed to smart-connect)

Routing is implemented by preprocessing argv before Cobra runs:

- `preprocessArgs()` classifies arguments into:
  - **Global flags** (e.g., `-v`, `-V`, `-h`) that must stay before the subcommand
  - **SSH passthrough flags** (e.g., `-p 2222`, `-J jump`, `-o ...`) that must
    appear after the hostname
- The default UX (`nssh <host>`) is routed to a hidden subcommand:
  `smart-connect <host> [ssh-args...]`
- `connect` and `smart-connect` disable interspersed flag parsing so SSH flags
  after the hostname are preserved (`cmd.Flags().SetInterspersed(false)`).
- Hostnames that collide with subcommand names (e.g., `host`, `log`) can still be
  connected to via `nssh connect <host> ...`.

Examples (preprocessing behavior):

```text
nssh host             -> nssh smart-connect host
nssh -v host          -> nssh -v smart-connect host
nssh -p 2222 host     -> nssh smart-connect host -p 2222
nssh host -p 2222     -> nssh smart-connect host -p 2222
```

Key files:

- `cmd/nssh/main.go` (root command, argument preprocessing, connect orchestration)
- `internal/cli/*` (subcommands: `inv`, `cred`, `log`, `cp`, `self`, `lock`, `unlock`)

### Subsystems

Layered runtime flow:

1. **Config & Paths**: `internal/config`
2. **Host Resolution**: `internal/ssh/sshconfig`
3. **Credential Access**: `internal/vault` + `internal/session`
4. **SSH Execution**: `internal/ssh/connector`
5. **Recording**: `internal/ssh/recording` + `internal/cli/log`
6. **UI**: `internal/ui`
7. **Logging & Exits**: `internal/logging`, `internal/exit`

### Config and Paths

XDG-style paths (`internal/config/paths.go`):

- `~/.config/nssh/config.toml`
- `~/.local/share/nssh/credentials.age`
- `~/.local/state/nssh/casts/`
- `~/.local/share/nssh/backups/`
- `~/.ssh/config` and `~/.ssh/nssh.d/*`

Config schema in `internal/config/settings.go` supports TOML and environment overrides.

### Vault + Agent Model

The credential system has two layers:

- **Vault** (`internal/vault`): owns on-disk encrypted data, backups, and
  resolution logic
- **Agent** (`internal/agent`): holds a decrypt session in memory and serves
  decrypt operations over a Unix domain socket

Important properties:

- Most commands can run with the vault **locked**; they will either:
  - prompt to unlock (interactive TTY), or
  - proceed without passwords (non-interactive / automation)
- Secrets in process memory are wrapped in `internal/secret.Secret` (memguard),
  and panic on formatting attempts to reduce accidental leakage.

Composition root:

- `internal/session/vault.go` builds a `vault.Manager` with agent-backed
  `SessionDeps` so `vault.Manager` can decrypt without embedding agent imports.

### SSH Connector and Prompt Handling

`internal/ssh/connector` handles PTY-based SSH: starts `ssh` under a PTY, streams I/O, detects/responds to prompts (passwords, host keys), emits timing markers when `NSSH_DEBUG=1`. See [PTY Connector Architecture](#pty-connector-architecture).

### Recording Wrapper

`internal/ssh/connector/recording_wrapper.go` spawns asciinema around re-executed `nssh` with recursion guard `NSSH_RECORDING_INNER=1`. See [Recording System Architecture](#recording-system-architecture).

## PTY Connector Architecture

The PTY connector lives in `internal/ssh/connector` and is responsible for the
interactive part of `nssh`: creating a pseudo-terminal, running `ssh`, handling
signals/window sizing, and safely responding to prompts.

### Key Responsibilities

The connector:

1. Builds a robust `ssh` argv that preserves normal SSH config behavior
2. Starts `ssh` under a PTY and relays I/O bidirectionally
3. Detects password prompts with minimal overhead
4. Handles host key verification prompts in a consistent, safe UX
5. Enforces connection, idle, and prompt timeouts when configured
6. Ensures terminal state and secrets are cleaned up on every exit path

### Process Model

```text
user terminal  <->  nssh connector  <->  PTY master  <->  ssh (child)  -> network
                   (prompt detection)
```

SSH receives a real TTY (`-tt`) for normal interactive authentication.

### Prompt Detection Pipeline

Ring buffer (2048 bytes default) with tiered checks (`internal/ssh/connector/patterns.go`):

1. Suffix checks (`bytes.HasSuffix`)
2. Contains checks (`bytes.Contains`)
3. Regex (only when needed)

`sync.Pool` for read buffers reduces GC pressure.

### Password Injection

Passwords held as `*secret.Secret` (accessed via `secret.Use(...)`). Filter window (100ms) prevents duplicate injection.

### Host Key Verification UX

Host key prompts are treated as a first-class flow (see
`internal/ssh/connector/hostkey.go`):

- **New host key**: show fingerprint (when parseable) and prompt:
  - Reject (disconnect)
  - Accept once (this session only)
  - Accept always (add to `known_hosts`)
- **Changed host key**: show a prominent warning and default to rejection

"Accept once" implementation: abort connection, create temp `known_hosts` via `ssh-keyscan`, restart with `-o UserKnownHostsFile=<temp> -o StrictHostKeyChecking=yes`. Pinning prevents key swap.

Non-interactive: rejects unless permissive flags provided.

### Signals and Terminal Mode

Raw mode for stdin, SIGWINCH forwarding, cleanup on exit (terminal state + temp files).

### Timing Instrumentation

`NSSH_DEBUG=1` emits `NSSH_TIMING:<event>:<duration_ms>` to stderr. Events: `config_load`, `credential_lookup`, `pty_start`, `first_read`, `password_prompt`, `password_sent`, `session_end`, `total`. Parsed by `nssh self bench`.

## SCP Connector Architecture

`internal/cli/cp/cp.go` wraps `scp` with SSH config resolution and vault integration.

### Remote Spec Parsing

Edge cases: `:file` (local), `./file:name` (local), `[::1]:/path` (IPv6). `findColonSeparator()` and `parseRemoteSpec()` match OpenSSH behavior.

### Execution Model

Build remote spec with SSH `Host` identifier, resolve credentials, spawn `scp` under PTY, inject password via simplified prompt detector.

## Connection Flow

This section outlines what happens during a typical `nssh <host>` invocation.

### Smart Connect Flow (Default)

```text
1. User runs: nssh [user@]<query> [ssh-args...]

2. Arg preprocessing (cmd/nssh/main.go):
   - Preserve global flags for Cobra (-v, -V, -h)
   - Move SSH flags after hostname for subcommand parsing
   - Route to: smart-connect <query> [ssh-args...]

3. Host resolution (internal/ssh/sshconfig):
   - Exact match on Host
   - Match on derived short ID (e.g., "router" from "router.example.com")
   - Prefix/contains matches across Host and HostName
   - Multiple matches -> interactive fuzzy select
   - No matches -> refresh stale inventory providers once, then raise HostNotFoundError

4. Recording decision (internal/ssh/connector/recording_wrapper.go):
   - Load recording settings (config + env)
   - If enabled for this host:
     - Acquire a session lock directory
     - Spawn: asciinema rec --command "<nssh inner>" <castPath>
     - Set NSSH_RECORDING_INNER=1 and exit with inner’s status
   - If disabled, continue normally

5. Config load + optional audit logger:
   - Parse config.toml (internal/config)
   - If enabled, start audit log writer (internal/logging)

6. Username + credential resolution:
   - Determine username from (in order):
     - ssh args (-l), user@host prefix, SSH config User, config default
   - Build vault manager via session composition root (internal/session)
   - If vault is locked and stdin is a TTY, prompt to unlock
   - Resolve credential by include-file mapping, then domain mapping

7. PTY connector runs SSH (internal/ssh/connector):
   - Start ssh under PTY, relay I/O, handle host keys and passwords
   - Enforce timeouts and emit timing markers when enabled
```

### Host Not Found Behavior

`HostNotFoundError` triggers local inventory creation with `nssh inv set <hostname>`.

### Multiple Matches Behavior

`sshconfig.MatchHost()` returns suggestions; `ui.FuzzySelectString()` prompts (prefers `fzf`, falls back to in-process).

### Retry Loop for Compatibility Fixes

After failed connection: probe with verbose SSH, detect missing algorithms, prompt to apply fixes, retry. Implementation: `internal/ssh/compat`, `connector/tester.go`, `sshconfig/mutations.go`, `cmd/nssh/main.go`.

## SSH Compatibility Detection and Remediation

Interactive "detect then apply" workflow for legacy SSH servers.

### Detection Sources

1. **Proactive**: during inventory changes via `ssh -vv ... -- exit`
2. **Reactive**: after failed connection

### Probe Design

`internal/ssh/connector/tester.go`: uses `ConnectTimeout`, `ControlPath=none`, `StrictHostKeyChecking=accept-new`, temp `known_hosts`. Modes: BatchMode (negotiation only) or PTY + password (full auth).

### Fix Application

`compat.CompatConfigs` + `sshconfig.ApplyCompatFixes()`: removes conflicts, inserts compat lines after `HostName`/`Port`, updates `HostEntry.Properties`. Bounded loops prevent directive duplication.

## Credential Vault and Resolution

Age-encrypted JSON vault with deterministic resolution, in-memory decrypt sessions, TTY/non-interactive support.

### Data Model

The decrypted JSON structure (see `internal/vault/manager.go`) is:

```json
{
  "groups": {
    "work": {
      "git_include_file": "work.conf",
      "domain": "corp.example.com",
      "credential": { "username": "alice", "password": "..." }
    }
  },
  "hosts": {
    "router1": {
      "credentials": [
        { "username": "admin", "password": "...", "default": true }
      ]
    }
  }
}
```

Notes:

- Host entries store a list of credentials; one may be marked `default:true`
- Groups can be referenced by SSH include file (basename) and/or by domain
- Passwords are plaintext only in decrypted JSON (the `.age` file protects them
  at rest)

### Unlock and Session Caching

Agent daemon holds age identity in memory; vault delegates decrypt over Unix socket. `nssh unlock` starts session, `nssh lock` terminates. Auto-prompt when locked + TTY.

### Resolution Rules (Connect Time)

Resolution order: username detection → include-file resolution → domain resolution → nil (SSH proceeds with keys).

**With username**: exact match in host credentials → group credential → nil.
**Without username**: host default → group credential → nil.

### Secret Handling in Memory

`*secret.Secret` destroyed on exit. PTY uses `Use([]byte)`, avoiding string copies.

## Recording System Architecture

Asciinema integration with per-host session directories, locks, and index metadata.

### Settings and Host Selection

Settings: defaults (`recording.DefaultRecordingSettings()`) → config (`[logging.session]`) → env overrides. Host selection: exclude/include patterns (globs or `regex:` prefix).

### Recording Plan

`recording.BuildRecordingPlan()`: cast path, lock directory, append/new behavior, title template, window size, idle limit.

### Wrapper Strategy (Outer/Inner nssh)

```text
outer nssh -> acquire lock -> asciinema rec -> inner nssh (NSSH_RECORDING_INNER=1) -> PTY connector
```

Keeps PTY connector focused on SSH, not recording management.

### Locking and Session Number Allocation

Per-session lock directory (atomic, `.lockinfo` with PID/timestamp) + sequence allocation lock (`flock()` on `.session-counter.lock`). Append mode prefers unlocked existing session, else new sequence.

### Index Metadata

`<cast>.index.json`: host, path, session entries (timestamps, argv, exit code, auth). Atomic writes (tmp → rename). Produced by `recording.WriteIndex()`, read by `recording.ReadCastMetadata()`.

### CLI Integration

`internal/cli/log`: list, play, export, upload, delete. Fuzzy selection reused from host matching.

## API Reference

Internal packages with stable boundaries for contributors. Not for third-party import (`internal/` enforced).

### Internal Package Entry Points

**SSH Connector** (`internal/ssh/connector`): `*connector.Connector`, `NewConnector()`, `SetTimeouts()`, `Run()`.

**SSH Config Parser** (`internal/ssh/sshconfig`): `*Parser`, `*HostEntry`, `*ParsedConfig`. Methods: `FindIncludeFiles()`, `GetAllHosts()`, `FindHost()`, `MatchHost()`, `ParseFile()`, `WriteFile()`, `ApplyCompatFixes()`, `ApplyAuthType()`.

**Vault Manager** (`internal/vault`): `*vault.Manager` (via `internal/session`). Methods: `NeedsUnlock()`, `ResolveCredential()`, `ResolveCredentialWithDomain()`, `GetHostCredentials()`, `GetGroupByIncludeFile()`.

### Agent IPC Protocol

JSON-over-UDS (`internal/agent/protocol.go`). Request: `{"v":1,"op":"decrypt","data":"..."}`. Response: `{"ok":true,"data":"..."}`. Operations: `hello`, `status`, `decrypt`, `recipient`, `lock`. Socket: `$XDG_RUNTIME_DIR/nssh.sock` (Linux) or `$TMPDIR/nssh.sock` (macOS).

### Exit Codes

Exit codes are centralized in `internal/exit/exit.go`:

Centralized in `internal/exit/exit.go`: 0 (success), 1 (general), 2 (connection), 3 (auth), 4 (host not found), 5 (vault), 126 (not executable), 127 (not found). Returned as `*exit.ExitError`.

## SSH Config Implementation

Parsing + include discovery (`parser.go`, `include.go`), mutations (`mutations.go`), CLI orchestration (`internal/cli/host/*`).

### Include Discovery

`Parser.FindIncludeFiles()`: BFS walk from `~/.ssh/config`, finds `Include` directives, expands paths/globs, filters to existing files.

### Parsing Model

`Parser.ParseFile()`: line-by-line scan, builds `HeaderLines` + `HostEntry` list (Host, Patterns, Lines, Properties, SourceFile). Wildcard hosts (`Host *`) treated as defaults.

### Matching Heuristics

`Parser.MatchHost()`: exact → short ID → prefix → contains. Returns suggestions for interactive selection if multiple matches.

### Writing and Backups

Atomic writes: temp file → `chmod 0600` → `rename()`. Timestamped backups in `~/.local/share/nssh/backups/`, pruned per config.

### Sorting and Insertion

Alphabetized by `Host` identifier for predictable diffs.

## Debugging and Profiling

**Verbose logs**: `-v` / `--verbose` for debug logs (slog). Audit log: `~/.local/state/nssh/audit.log` (rotated).

**SSH verbose**: `nssh myhost -vvv` (flags passed through to SSH).

**Timing**: `NSSH_DEBUG=1 nssh myhost` emits `NSSH_TIMING:*` to stderr.

**Benchmarks**: `nssh self bench ssh myhost [--samples N --warmups N]`.

**Recording troubleshooting**: check `asciinema` on PATH, `NSSH_RECORDING_INNER`, session dirs (`~/.local/state/nssh/casts/`), stale locks.

**Vault troubleshooting**: `nssh unlock`, `nssh lock`, `nssh self status`. Automation: use SSH keys or `nssh unlock --stdin`.
