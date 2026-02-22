# nssh User Guide

This guide covers installation, setup, credential management, and day-to-day CLI usage.
For a project overview, see the repository `README.md`.

## Table of Contents

- [Getting Started](#getting-started)
  - [Prerequisites](#prerequisites)
  - [Install](#install)
  - [Initialize nssh](#initialize-nssh)
  - [SSH Include Layout (conf.d)](#ssh-include-layout-confd)
  - [Create Your First Context](#create-your-first-context)
  - [Add Your First Host](#add-your-first-host)
  - [First Connection](#first-connection)
  - [Lock and Unlock (Agent Session)](#lock-and-unlock-agent-session)
- [Quick Reference](#quick-reference)
- [Core Concepts](#core-concepts)
  - [Host vs HostName](#host-vs-hostname)
  - [Contexts](#contexts)
  - [Vault, Keys, and the Agent](#vault-keys-and-the-agent)
  - [Credential Lookup Order](#credential-lookup-order)
  - [Smart Connect Routing](#smart-connect-routing)
  - [Host Key Prompts and Pinning](#host-key-prompts-and-pinning)
  - [Session Recording Model](#session-recording-model)
- [Configuration](#configuration)
  - [Config File](#config-file)
  - [Environment Variables](#environment-variables)
  - [Files and Directories](#files-and-directories)
- [CLI Usage](#cli-usage)
  - [nssh (Smart Connect)](#nssh-smart-connect)
  - [nssh connect (Direct Connect)](#nssh-connect-direct-connect)
  - [nssh unlock / nssh lock](#nssh-unlock--nssh-lock)
  - [nssh ctx (Contexts)](#nssh-ctx-contexts)
  - [nssh host (Host Files)](#nssh-host-host-files)
    - [Batch Add / Batch Remove](#batch-add--batch-remove)
    - [Compatibility Fixes for Legacy SSH](#compatibility-fixes-for-legacy-ssh)
  - [nssh cp (SCP Wrapper)](#nssh-cp-scp-wrapper)
  - [nssh log (Recordings)](#nssh-log-recordings)
  - [nssh self (Install / Maintenance)](#nssh-self-install--maintenance)
    - [self init](#self-init)
    - [self status](#self-status)
    - [self rekey](#self-rekey)
    - [self reinstall](#self-reinstall)
    - [self bench](#self-bench)
    - [self uninstall / self reset](#self-uninstall--self-reset)
- [Security Best Practices](#security-best-practices)
  - [Minimize Secret Exposure](#minimize-secret-exposure)
  - [File Permissions](#file-permissions)
  - [Host Key Safety](#host-key-safety)
  - [Recording Privacy](#recording-privacy)
  - [Key Rotation and Rollback](#key-rotation-and-rollback)
- [Troubleshooting](#troubleshooting)

## Getting Started

### Prerequisites

`nssh` is a thin wrapper around the system SSH tools plus a local credential vault.
**Required**

1. **OpenSSH client** (`ssh`)
   ```bash
   ssh -V
   ```
2. **SCP** (`scp`) for `nssh cp`
   ```bash
   scp -V 2>/dev/null || true
   ```

**Recommended (optional, but improves UX)**

3. **fzf** for fuzzy selection (nssh falls back to an in-process selector if missing)
   ```bash
   fzf --version
   ```
4. **asciinema** for session recording and playback
   ```bash
   asciinema --version
   ```
5. **agg** (or `asciicast2gif`) for GIF export via `nssh log export`
   ```bash
   agg --version 2>/dev/null || true
   asciicast2gif --version 2>/dev/null || true
   ```

**Building from source (optional)**

- Build toolchain for source builds (see repository `CONTRIBUTOR.md`)
- Hardware (YubiKey/PIV) builds require CGO and PC/SC libraries (see [self reinstall](#self-reinstall)).

### Install

Choose one path:
**Option A: Install script**
If you’re installing from the upstream repo, use the installer script referenced in `README.md`.
**Option B: Build from source**
From the repository root:
```bash
make build
./bin/nssh --help
```
To put it on your PATH (example):
```bash
install -m 0755 ./bin/nssh ~/.local/bin/nssh
```

### Initialize nssh

Run the interactive initializer:
```bash
nssh self init
```
Useful flags:
```bash
nssh self init --dry-run     # preview without writing files
nssh self init --skip-shell  # don’t modify shell rc files
nssh self init -y            # accept defaults
```
What `self init` sets up (high level):

- Ensures `~/.config/nssh`, `~/.local/share/nssh`, and `~/.local/state/nssh` exist
- Initializes credential protection (passphrase-based by default; hardware mode if selected and available)
- Ensures `~/.ssh/config` includes `~/.ssh/conf.d/*`
- Offers shell integration and completions
- Checks for external dependencies (`ssh`, `scp`, and optional tools)

Afterwards, run:
```bash
nssh self status
```

### SSH Include Layout (conf.d)

`nssh` is designed around an SSH include directory:

- `~/.ssh/config` contains an `Include ~/.ssh/conf.d/*` line
- `~/.ssh/conf.d/` holds one or more host files (created/managed by `nssh`)

`nssh` writes host blocks into the include files. This keeps the main SSH config small and lets you group hosts by “context” (see below).
> If you already have your own Include layout, `nssh` will reuse it. If you don’t, `nssh self init` will add one.

### Create Your First Context

Before adding hosts, create at least one context:
```bash
nssh ctx add work
```
A context ties together:

- An include file (e.g., `work_hosts`) under `~/.ssh/conf.d/`
- (Optional) a domain suffix for auto-selection (e.g., `example.com`)
- (Optional) a fallback username/password used when a host doesn’t have its own credential

Non-interactive (scripted) context creation:
```bash
nssh ctx add work --ssh-config work_hosts --domain example.com --username admin
```

### Add Your First Host

Add a host interactively:
```bash
nssh host add switch.example.com
```
Notes:

- `nssh` will ask which context/include file to put the host in.
- By default, host addition runs a connection test; you can skip it with `--force`.
- If you choose password auth, `nssh` can store the password in the vault.

Common add flows:
```bash
nssh host add switch.example.com           # guided prompts
nssh host add switch.example.com -y        # accept defaults
nssh host add switch --hostname 10.0.0.10  # 'switch' is the Host alias, IP is the target
nssh host add switch.example.com --auth key
nssh host add switch.example.com --auth password --force
```

### First Connection

Once the host exists in SSH config, connect with:
```bash
nssh switch
```
How smart connect behaves:

- Exact match: connects immediately
- Partial match: if multiple hosts match, you’ll be prompted to choose
- No match: `nssh` will offer to run `nssh host add <name>`

### Lock and Unlock (Agent Session)

The vault is encrypted at rest. To use stored credentials, `nssh` runs a background **agent** that holds the decryption capability for a limited time.
Unlock explicitly:
```bash
nssh unlock
```
Lock explicitly (end the session):
```bash
nssh lock
```
Automation/headless unlock:
```bash
printf '%s\n' "$PASSPHRASE" | nssh unlock --stdin
```
Agent lifetime is controlled by `[agent]` settings in `config.toml` (idle timeout, max lifetime). See [Config File](#config-file).

## Quick Reference

```bash
# Status / setup
nssh self init
nssh self status

# Contexts
nssh ctx add work
nssh ctx list
nssh ctx get work
nssh ctx edit work --domain example.com
nssh ctx remove work

# Hosts
nssh host add switch.example.com
nssh host list
nssh host get switch
nssh host edit switch
nssh host rm switch
nssh host sort

# Connect
nssh switch
nssh admin@switch
nssh connect host -p 2222       # direct connect with extra ssh flags

# Vault session
nssh unlock
nssh lock

# SCP wrapper
nssh cp switch:/etc/hostname ./
nssh cp ./config.txt switch:~/

# Recordings (opt-in)
nssh log list
nssh log play
nssh log export
nssh log upload

# Benchmarks
nssh self bench ssh switch
nssh self bench scp switch --size 1M
```

## Core Concepts

### Host vs HostName

SSH config has two different ideas that matter a lot in `nssh`:

- **Host**: the alias you type to connect (`nssh core-switch`) - must be unique
- **HostName**: the target address (FQDN or IP) that SSH actually connects to

`nssh` uses the **Host alias** as the primary key for:

- Finding the SSH block that applies (so your SSH settings match)
- Looking up host-specific credentials in the vault

When you run `nssh host add server.example.com`, `nssh` will suggest a Host alias derived from the first label:

- `server.example.com` -> `server`
- `lab-router-01.example.com` -> `lab-router-01`
- `router` -> `router`

Alternatively, specify Host and HostName separately:
```bash
nssh host add switch --hostname 10.0.0.10   # 'switch' = Host alias, IP = target
```

You can always override Host and HostName during `host add` (or later via `host edit`).

### Contexts

A **context** is a small record stored in the encrypted vault that typically represents one environment (work, homelab, customer-A, etc).
Each context can include:

- `ssh-config`: which include file it corresponds to (a file under `~/.ssh/conf.d/`)
- `domain`: a suffix like `example.com` that lets `nssh` auto-select the context for matching FQDNs
- `credential`: an optional fallback username/password

Context selection shows up in two places:

1. **When adding hosts**: `nssh host add` asks which context/include file to write into.
2. **When connecting or copying**: `nssh` uses the include file a host came from to find the context fallback credential.

### Vault, Keys, and the Agent

At rest, credentials live in an age-encrypted file:

- The vault file is `credentials.age`
- The public key is `age.pub`
- The private key material is protected in one of two ways:
  - **Software mode**: `age.key.enc` (passphrase-protected)
  - **PIV mode**: `piv.json` (encrypted to one or more enrolled YubiKeys)

During use, a background agent process holds the active “unlock session”. Most commands will:

- Use the agent automatically if it’s already running
- Prompt to unlock (when a TTY is available)
- Degrade gracefully if running non-interactively (no prompts)

You can see session status with:
```bash
nssh self status
```

### Credential Lookup Order

Credential resolution is intentionally predictable:

1. If you connect as `user@host`, `nssh` looks for a credential matching that **exact username**:
   - host-specific credentials first
   - then the context fallback credential
2. If you connect as just `host` (no username specified), `nssh` will:
   - use a host credential marked as the default (if one exists)
   - otherwise use the context fallback credential (if one exists)
3. If no credential is found, `nssh` won’t inject a password. SSH will proceed with keys or prompts.

This is why the include file matters: it ties a host back to a context, which may provide a fallback credential.

### Smart Connect Routing

The UX goal is “connect by default”:

- `nssh something` behaves like “smart connect”
- subcommands still exist (`nssh host ...`, `nssh ctx ...`, etc.)

Flags **before** the target are nssh flags; flags **after** the target are passed through to ssh:
```bash
nssh -v switch          # nssh debug logging (flag before target)
nssh switch -v          # SSH verbose mode (flag after target, passed to ssh)
nssh switch -vvv        # SSH extra verbose (passed to ssh)
nssh -p 2222 switch     # rewritten to: nssh switch -p 2222 (passed to ssh)
```
Use `nssh connect` when you want to bypass smart matching and treat the next arg as the SSH destination verbatim:
```bash
nssh connect user@1.2.3.4 -p 2222
```
For canonical help output, see:

- `docs/examples/help/nssh.txt`
- `docs/examples/help/connect.txt`

### Host Key Prompts and Pinning

When connecting to a host for the first time, SSH may prompt to confirm the host key.
`nssh` detects these prompts and offers actions such as:

- Reject (abort)
- Accept once (pin for this session using a temporary known_hosts)
- Accept always (delegate to OpenSSH to add to your real known_hosts)

You can configure the default behavior via:

- `ssh.security.host_key_policy` (`pin` or `tofu`)
- `ssh.security.accept_once_mode` (`pin` or `accept-new`)

See [Configuration](#configuration) for details.

### Session Recording Model

Recording is **opt-in** by default.
When enabled, `nssh` wraps the SSH session with `asciinema` and stores `.cast` files in a state directory (default: `~/.local/state/nssh/casts`).
You can:

- list recordings (`nssh log list`)
- play (`nssh log play`)
- export to `.txt` or `.gif` (`nssh log export`)
- upload (`nssh log upload`) after authenticating (`nssh log auth`)

Recording can be constrained via include/exclude host patterns in `config.toml` or via environment overrides (see [Environment Variables](#environment-variables)).

## Configuration

### Config File

`nssh` reads an optional TOML config file:

- Default: `~/.config/nssh/config.toml`
- Example: `docs/examples/config/config.example.toml`

Key sections (as implemented):
```toml
[agent]
idle_timeout = "1h"
activity_increment = "15m"
max_lifetime = "24h"

[host.defaults]
default_context = ""
default_user = ""

[logging.audit]
enabled = true
max_backup_files = 10
max_size = "10MB"

[logging.session]
enabled = false
append_mode = true
dir = "~/.local/state/nssh/casts"
asciinema_server_url = "https://asciinema.org"
exclude_hosts = ["prod-*"]
include_hosts = ["lab-*"]
idle_time_limit = 0
idle_time_limit_mode = "play"
title_format = "nssh:{host}"
window_size = "100x30"

[logging.session.archive]
enabled = false
dir = "~/.local/state/nssh/archives"
min_age = "30d"
max_bundles = 12
jitter = "30m"
max_run_bytes = 0

[ssh.connection]
timeout = "30s"
password_timeout = "10s"
idle_timeout = "0"

[ssh.security]
host_key_policy = "pin"      # or "tofu"
accept_once_mode = "pin"     # or "accept-new"
compat_persist_probes = false
```
Notes:

- Security mode (software vs PIV) is detected from files in `~/.config/nssh/`; it is not selected via `config.toml`.
- `host_key_policy="tofu"` is a preset that sets `accept_once_mode="accept-new"` and enables `compat_persist_probes`.

### Environment Variables

Environment variables are intentionally limited. The list below reflects what is currently supported.
| Variable | What it affects |
|----------|------------------|
| `NSSH_AGENT_IDLE_TIMEOUT` | Overrides `[agent].idle_timeout` (e.g., `30m`, `2h`) |
| `NSSH_AGENT_ACTIVITY_INCREMENT` | Overrides `[agent].activity_increment` |
| `NSSH_AGENT_MAX_LIFETIME` | Overrides `[agent].max_lifetime` |
| `NSSH_ACCEPT_ONCE_MODE` | Overrides `ssh.security.accept_once_mode` (`pin` / `accept-new`) |
| `NSSH_HOST_KEY_POLICY` | Overrides `ssh.security.host_key_policy` (`pin` / `tofu`) |
| `NSSH_AGE_KEY` | Overrides legacy unprotected key path detection (used for migration messaging) |
| `NSSH_PASSPHRASE` | Non-interactive passphrase for `nssh self init` only (CI/testing) |
| `NSSH_RECORD` | Recording on/off (`1`/`0`) |
| `NSSH_RECORD_DIR` | Recording directory override |
| `NSSH_RECORD_IDLE_TIME_LIMIT` | Recording idle limit override (seconds) |
| `NSSH_RECORD_IDLE_TIME_LIMIT_MODE` | Idle limit mode (`play` / `record` / `both`) |
| `NSSH_RECORD_TITLE_FORMAT` | Title template override |
| `NSSH_RECORD_HEADLESS` | Headless recording mode (`1` / `true`) |
| `NSSH_RECORDING_INNER` | Internal recursion guard for recording wrapper |
| `NSSH_ASCIINEMA_SERVER_URL` | asciinema server URL override (for `nssh log auth/upload`) |
| `NSSH_DEBUG` | Enables timing output (`NSSH_TIMING:*`) for benchmarks/debugging |
| `XDG_CONFIG_HOME` | Base dir for config (`~/.config` default) |
| `XDG_DATA_HOME` | Base dir for data (`~/.local/share` default) |
| `XDG_STATE_HOME` | Base dir for state (`~/.local/state` default) |

### Files and Directories

Default locations follow the XDG base directory spec (unless overridden by XDG env vars).
| Path | Purpose |
|------|---------|
| `~/.config/nssh/config.toml` | Optional configuration |
| `~/.config/nssh/age.pub` | Vault public key (encryption) |
| `~/.config/nssh/age.key.enc` | Software-mode encrypted private key |
| `~/.config/nssh/piv.json` | PIV-mode keystore metadata (hardware builds) |
| `~/.local/share/nssh/credentials.age` | Encrypted vault contents |
| `~/.local/share/nssh/backups/` | Backups for credentials/SSH config writes |
| `~/.local/state/nssh/audit.log` | Security/audit log (rotates) |
| `~/.local/state/nssh/casts/` | Session recordings (`.cast`) |
| `~/.local/state/nssh/archives/` | Optional recording archives (`.tar.gz`) |
| `~/.ssh/config` | Main SSH config (contains Include for conf.d) |
| `~/.ssh/conf.d/` | Host include files (written by `nssh`) |

## CLI Usage

This section focuses on *operator-level* usage. For the exact flag list, see:

- `docs/examples/help/nssh.txt`
- `docs/examples/help/host.txt`
- `docs/examples/help/ctx.txt`
- `docs/examples/help/log.txt`
- `docs/examples/help/self.txt`

Also note the global `--explain` flag on many commands, which prints a detailed description of what the command does.

### nssh (Smart Connect)

Smart connect is the default behavior when you run `nssh <something>`.
Examples:
```bash
nssh core-switch-01          # connect if exact match
nssh core                    # if ambiguous, select from matches
nssh admin@core-switch-01    # connect as a specific user
nssh -p 2222 core-switch-01  # pass SSH flags (forwarded)
```

> **Tip:** `nssh connect` with no arguments opens the fuzzy finder across all known hosts with no pre-filtering.
> Handy to bind to a hotkey in your terminal multiplexer for a quick host picker.

If you want to see what `nssh` thinks you meant:
```bash
nssh --help
nssh host add --explain      # detailed purpose of a subcommand
```

### nssh connect (Direct Connect)

Use `connect` when you want a raw SSH wrapper experience:

- No "host add" fallback
- When a hostname is given, it is always treated as the destination verbatim (no smart matching)
- Useful when a Host alias conflicts with a subcommand name (e.g., `nssh connect host`)
- With **no arguments**, opens the fuzzy finder across all known hosts (see tip above)
```bash
nssh connect router.example.com
nssh connect router.example.com -p 2222 -o StrictHostKeyChecking=accept-new
```

### nssh unlock / nssh lock

`unlock` starts the agent session.
```bash
nssh unlock
```
Automation:
```bash
printf '%s\n' "$PASSPHRASE" | nssh unlock --stdin
```
`lock` terminates the agent session and clears the in-memory unlock state:
```bash
nssh lock
```

### nssh ctx (Contexts)

Common tasks:
```bash
nssh ctx add work
nssh ctx add work --dry-run           # preview context creation
nssh ctx list
nssh ctx get work
nssh ctx edit work --ssh-config work_hosts
nssh ctx edit work --domain example.com
nssh ctx edit work --username admin
nssh ctx remove work
```
Context fields at a glance:

- `--ssh-config`: file under `~/.ssh/conf.d/` where hosts for this context live
- `--domain`: suffix used to auto-select the context when adding hosts
- `--username`: fallback username (password is prompted securely)

### nssh host (Host Files)

Common tasks:
```bash
nssh host add switch.example.com
nssh host list
nssh host list --select 'core|edge'
nssh host get switch
nssh host edit switch
nssh host rm switch
nssh host sort
```
Flags worth knowing:

- `nssh host add --dry-run` shows what would be added, without writing files
- `nssh host add --force` skips connection testing
- `nssh host add --auth key|password` sets auth mode up front
- `nssh host edit --auth key|password` switches auth mode later
- `nssh host get --show-secret` reveals decrypted passwords (use carefully)

#### Batch Add / Batch Remove

Batch operations support **CSV** and **JSON**:
```bash
nssh host add ./hosts.example.csv
nssh host add ./hosts.example.json

nssh host rm ./hosts.example.csv
nssh host rm ./hosts.example.json
```

| Field    | Required | Description                                              |
|----------|----------|----------------------------------------------------------|
| host     | Yes      | SSH Host alias (the identifier you use to connect)       |
| hostname | No       | SSH HostName (connection target: FQDN or IP address)     |
| user     | No       | SSH username (defaults to config.toml setting)           |
| port     | No       | SSH port (defaults to 22)                                |
| context  | No       | Credential context for password authentication           |
| password | No       | Host-specific password (stored in vault)                 |

When `hostname` is omitted, the `host` value is used as both the alias and connection target.

Example files: `docs/examples/batch/hosts.example.csv` and `hosts.example.json`.

Notes:

- `context` will be auto-created if it doesn't exist (batch add).
- Fields not specified use defaults from `~/.config/nssh/config.toml`:
  ```toml
  [host.defaults]
  user = "admin"
  context = "work"
  ```

#### Compatibility Fixes for Legacy SSH

Some older devices require deprecated SSH algorithms.
`nssh` can detect common negotiation errors and apply SSH config directives (per-host) to make the connection work.
Where this shows up:

- `nssh host add` runs a connection test and may apply fixes before writing the final host block.
- `nssh host edit --auth ...` can optionally run compatibility fixes after changing auth mode.

If you don’t want any network checks during add:
```bash
nssh host add legacy-box.example.com --force
```

### nssh cp (SCP Wrapper)

`nssh cp` is a wrapper around `scp` that can inject a password from the vault when needed.
Direction is inferred from which argument includes `host:path`.
```bash
# pull remote → local
nssh cp switch:/etc/hostname ./

# push local → remote
nssh cp ./config.txt switch:~/

# recurse
nssh cp -r switch:~/configs ./configs/
```
If both paths look remote, `nssh cp` will refuse (to match common safety expectations).

### nssh log (Recordings)

Recording is disabled by default. Enable it in config:
```toml
[logging.session]
enabled = true
```
Then use:
```bash
nssh log list
nssh log play
nssh log export
nssh log delete
```
Upload flow:
```bash
nssh log auth
nssh log upload
```
Notes:

- `nssh log export` exports to `.txt` via `asciinema convert`, or to `.gif` via `agg`/`asciicast2gif`.
- You can set a self-hosted asciinema server URL via config or `NSSH_ASCIINEMA_SERVER_URL`.

### nssh self (Install / Maintenance)

#### self init

Covered in [Initialize nssh](#initialize-nssh).

#### self status

`nssh self status` is the fastest way to see what’s configured:

- config and vault files present/missing
- include dir and host counts
- whether the agent session is currently unlocked
- which optional dependencies are available
```bash
nssh self status
```

#### self rekey

Use `rekey` to rotate or switch credential protection mode:
```bash
nssh self rekey --software   # switch to passphrase-protected software mode
nssh self rekey --hardware   # switch to YubiKey PIV mode (hardware builds)
nssh self rekey --rollback   # recover from a failed mode switch
```
If your `age.pub` is missing or corrupted, you can regenerate it:
```bash
nssh self rekey --repair-pubkey
```

#### self reinstall

`reinstall` downloads and installs the latest release from GitHub:
```bash
nssh self reinstall
```
Hardware builds (YubiKey/PIV support):
```bash
nssh self reinstall --hardware
```
For development workflows (build from source):
```bash
nssh self reinstall --dev
nssh self reinstall --dev --hardware
```
If the hardware dev build fails, you likely need CGO and PC/SC libraries:

- macOS: PCSC.framework is built in
- Linux: install `pcscd` and development headers (distro-specific)

#### self bench

`bench` runs repeatable timing measurements for SSH or SCP:
```bash
nssh self bench ssh switch
nssh self bench scp switch --size 1M
```
An example benchmark output is in `docs/examples/output/benchmark-run.txt`.

#### self uninstall / self reset

Uninstall (optionally keep config or recordings):
```bash
nssh self uninstall
nssh self uninstall --dry-run
nssh self uninstall --keep-config --keep-recordings
```
Reset deletes nssh data and starts fresh:
```bash
nssh self reset
nssh self reset --dry-run
nssh self reset --force
```

## Security Best Practices

### Minimize Secret Exposure

- Prefer interactive prompts over automation.
- Avoid `nssh unlock --stdin` unless you’re in a controlled environment (CI, ephemeral runner, etc.).
- Treat `nssh host get --show-secret` and `nssh ctx get --show-secret` as “break glass”: they print plaintext secrets.

### File Permissions

`nssh` creates sensitive files with restrictive permissions where possible (typically `0600` for secrets and logs, `0700` for directories).
If you suspect permissions drifted:
```bash
nssh self status
ls -la ~/.config/nssh ~/.local/share/nssh ~/.local/state/nssh
```

### Host Key Safety

- Prefer “Accept once” (pinning) when you can’t independently verify the host key yet.
- Treat “host key changed” warnings as high severity: verify out-of-band before proceeding.
- Use `ssh.security.host_key_policy = "pin"` unless you explicitly want TOFU behavior.

### Recording Privacy

If you enable recording, assume recordings may contain sensitive information (commands, output, on-screen secrets).
Mitigations:

- Use `include_hosts` to record only low-risk environments
- Use `exclude_hosts` to never record sensitive systems
- Disable per-session with:
  ```bash
  NSSH_RECORD=0 nssh prod-db
  ```

### Key Rotation and Rollback

- Use `nssh self rekey` for planned rotations.
- If a mode switch is interrupted and you end up with an “ambiguous” keystore state, use:
  ```bash
  nssh self rekey --rollback
  ```
- Keep backups until you’ve verified you can decrypt and connect successfully.

## Troubleshooting

### Quick Checks

```bash
nssh self status
nssh --help
nssh unlock
```

### “nssh: command not found”

- Confirm where the binary is installed:
  ```bash
  which nssh
  ```
- If you built from source, install to a directory on your PATH (example: `~/.local/bin`).

### “no vault initialized” / “run 'nssh self init'”

Initialize the vault and SSH layout:
```bash
nssh self init
```

### “vault locked … run 'nssh unlock'”

Unlock the session:
```bash
nssh unlock
```
If you’re running without a TTY (automation), use stdin:
```bash
printf '%s\n' "$PASSPHRASE" | nssh unlock --stdin
```
If unlock attempts are repeatedly failing, you may be in a lockout window (software mode). Wait for the lockout to expire or adjust lockout settings in config.

### “No contexts configured”

Create a context first:
```bash
nssh ctx add work
```
Then add hosts:
```bash
nssh host add switch.example.com
```

### Host not found / unexpected host list

If smart connect says it can’t find a host:

1. Confirm the host exists:
   ```bash
   nssh host list --select switch
   ```
2. Confirm your SSH Include is wired:
   ```bash
   rg -n \"^\\s*Include\\s+\" ~/.ssh/config
   ```
3. Re-run init if you’re missing `~/.ssh/conf.d`:
   ```bash
   nssh self init
   ```

### Host key prompts, failures, or “REMOTE HOST IDENTIFICATION HAS CHANGED!”

- If the host is legitimately rebuilt/rekeyed, remove the old entry from your known_hosts:
  ```bash
  ssh-keygen -R <host>
  ```
- If you’re unsure, do not proceed until you can verify the new host key out-of-band.

### Recording not happening

- Recording is disabled by default. Enable in config or via env:
  ```bash
  NSSH_RECORD=1 nssh switch
  ```
- Ensure `asciinema` is installed and available on PATH.
- Check host include/exclude patterns in `[logging.session]`.

### "Hardware support not compiled into this binary"

You're running a non-hardware build. Install the hardware build:
```bash
nssh self reinstall --hardware
```
Or build from source:
```bash
nssh self reinstall --dev --hardware
# or manually:
go build -tags hardware ./cmd/nssh
```

### Performance questions

Use the built-in benchmarks:
```bash
nssh self bench ssh switch
```
Or enable timing output for a single run:
```bash
NSSH_DEBUG=1 nssh switch
```
