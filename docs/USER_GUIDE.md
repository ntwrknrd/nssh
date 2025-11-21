# nssh User Guide

This guide covers the full CLI usage, credential workflows, configuration details, and development notes. Pair it with the main [README.md](../README.md) for a high-level overview.

## Table of Contents
- [Quick Start Reference](#quick-start-reference)
- [Core Concepts](#core-concepts)
  - [Credential Resolution](#credential-resolution)
- [Configuration](#configuration)
  - [Environment Variables](#environment-variables)
  - [Configuration File](#configuration-file)
- [CLI Usage](#cli-usage)
- [nssh (Interactive Connector)](#nssh-interactive-connector)
  - [nssh cred (Credential Management)](#nssh-cred-credential-management)
  - [nssh host (SSH Config Management)](#nssh-host-ssh-config-management)
  - [nssh log (Session Recording)](#nssh-log-session-recording)
  - [nssh benchmark (Performance Analysis)](#nssh-benchmark-performance-analysis)
  - [nssh self (CLI & Shell Management)](#nssh-self-cli--shell-management)
- [Security Best Practices](#security-best-practices)
  - [Credential Protection](#credential-protection)
  - [Password Handling](#password-handling)
  - [Recording Privacy](#recording-privacy)
  - [File Permissions](#file-permissions)
  - [Key Management](#key-management)
  - [Safe Debugging](#safe-debugging)
  - [Manual Credential Editing (Advanced)](#manual-credential-editing)
  - [Key Rotation](#key-rotation)
  - [Multi-User Security](#multi-user-security)
- [Troubleshooting](#troubleshooting)

## Quick Start Reference

```bash
nssh core-switch                           # Connect (fuzzy search)
nssh admin@firewall                        # Connect as different user
nssh host list                             # List hosts
nssh host add switch.example.com --user admin
```

## Core Concepts

### Credential Resolution

nssh resolves credentials in this priority order:

1. **Host-specific credential** matching username → password auth
2. **Context fallback credential** (from SSH config Include file) → password auth
3. **No matching credential** → SSH keys or interactive prompt

**Key concepts:**

- **Host Overrides** - Host-specific credentials take priority over context fallbacks
- **Context Fallbacks** - Each context (SSH config Include file) can have a single fallback credential
- **User Prefixing** - `user@host` syntax preserves your intent; falls back to SSH keys if no credential exists

For the complete five-step resolution algorithm, context definitions, and detailed scenarios, see [ARCHITECTURE.md - Credential Resolution Algorithm](ARCHITECTURE.md#credential-resolution-algorithm).

## Configuration

### Environment Variables

| Variable | Purpose | Default |
|----------|---------|---------|
| `NSSH_DEFAULT_USER` | Default username when adding hosts | `$USER` |
| `NSSH_USERNAME` | Set internally when using `user@host` (don't override) | - |
| `NSSH_AGE_KEY` | Override age encryption key file path | `~/.config/age/keys.txt` |
| `NSSH_CRED_FILE` | Override encrypted credentials file path | `~/.ssh/nssh_credentials.age` |
| `NSSH_BACKUP_DIR` | Override SSH config backup directory | `~/.ssh/backups` |
| `NSSH_DEBUG` | Enable detailed timing logs | (disabled) |
| `NSSH_RECORD` | Override recording setting (`0`, `1`, `force`) | (from config) |
| `NSSH_RECORD_DIR` | Override recording storage directory | `~/.local/state/nssh/casts` |
| `NSSH_CONFIG` | Override config file path | `~/.config/nssh/config.toml` |
| `ASCIINEMA_SERVER_URL` | Server URL for `nssh log upload` | (none) |
| `NSSH_BIN_DIR` | Install directory for executables | `~/.local/bin` |
| `NSSH_SHARE_DIR` | Install directory for shared assets | `~/.local/share/nssh` |
| `XDG_CONFIG_HOME` | Base directory for configuration files | `~/.config` |
| `XDG_STATE_HOME` | Base directory for state data | `~/.local/state` |

For recording configuration details, see [examples/config.toml](examples/config.toml).

### Configuration File

Optional config via `~/.config/nssh/config.toml` for encryption paths, recording settings, host filters. Environment variables override config. See [examples/config.toml](examples/config.toml).

### Data Files

| File | Purpose |
|------|---------|
| `~/.ssh/config` | Main SSH config with Include directives |
| `~/.ssh/.nssh_host_index` | Pre-compiled host index for fast lookups |
| `~/.ssh/nssh_credentials.age` | Age-encrypted credential store |
| `~/.config/age/keys.txt` | Age encryption key |
| `~/.config/nssh/config.toml` | Recording & encryption config (optional) |
| `~/.local/state/nssh/casts/` | Session recordings (if enabled) |

## CLI Usage

### nssh (Interactive Connector)

```bash
nssh core-switch-01      # Exact match connects immediately
nssh core                # Partial match opens fzf when multiple hits
nssh admin@firewall      # Force connection as admin user
nssh router -l netadmin  # Override username via SSH -l flag
nssh -- host             # Escape when hostname matches subcommand (e.g., 'host', 'cred')
nssh -V hostname         # Enable verbose SSH output for debugging
nssh hostname -vvv       # Pass raw ssh options through
```

`nssh` preserves any `user@` prefix throughout fuzzy selection. When multiple candidates match, it launches `fzf` so you can choose interactively; single matches connect automatically. For all command options, run `nssh --help` or see [docs/examples/help/nssh.txt](examples/help/nssh.txt).

#### How nssh Works

nssh uses an in-process PTY connector for password injection during SSH connections. For technical details, see [ARCHITECTURE.md - PTY Connector Architecture](ARCHITECTURE.md#pty-connector-architecture).

### nssh cred (Credential Management)

Contexts organize credentials around SSH config files (e.g., `work_hosts`).

```bash
nssh cred ctx add work --file work_hosts
nssh cred ctx update work --username alice
nssh cred ctx list
nssh cred ctx rm work
```

Host-specific overrides allow fine-grained control:

```bash
nssh cred add special-switch --username netadmin
nssh cred add firewall --username readonly
nssh cred list firewall
nssh cred rm firewall --username readonly
```

See [docs/examples/help/nssh-cred.txt](examples/help/nssh-cred.txt) for the exhaustive command list and option descriptions.

### nssh host (SSH Config Management)

Add hosts interactively or via flags:

```bash
nssh host add                           # Guided flow with previews (includes connection test)
nssh host add switch.example.com --user admin            # password auth (default)
nssh host add device.local --file homelab_hosts --auth key
nssh host add legacy-server --auth password --no-test  # Skip connection test
nssh host rm old-server
nssh host list 192.168
nssh host sort --file homelab_hosts
nssh host update hostname                            # Auto-detect auth + legacy compat
```

For detailed information about SSH compatibility detection and troubleshooting for legacy devices, see [ARCHITECTURE.md - SSH Compatibility Detection and Remediation](ARCHITECTURE.md#ssh-compatibility-detection-and-remediation). Use `nssh host add --help` or see [docs/examples/help/nssh-host.txt](examples/help/nssh-host.txt) for command options.

### nssh log (Session Recording)

nssh integrates with [asciinema v3](https://asciinema.org) for automatic SSH session recording. Recordings are organized by hostname and date in `~/.local/state/nssh/casts/`.

For configuration details, see [Configuration File](#configuration-file).

Manage recorded sessions:

```bash
# List recordings (filter by keyword, repeatable for AND logic)
nssh log list --search lab-switch-01
nssh log list --search 2025-11-14
nssh log list --search lab --search 2025-11-15  # AND logic

# Play sessions (interactive picker)
nssh log play                    # Pick from most recent recordings
nssh log play --date 2025-11-14  # Pick from specific date
nssh log play --file session-001.cast  # Play specific file

# Upload to asciinema server (requires server configuration)
# Configure asciinema server (add to ~/.bashrc, ~/.zshrc, ~/.config/fish/config.fish, etc.)
export ASCIINEMA_SERVER_URL=https://asciinema.example.com

nssh log upload                  # Pick recording to upload
nssh log upload --date 2025-11-14

# Or specify server for one-off upload
nssh log upload --server https://asciinema.example.com

# Export recordings (saves to current directory by default)
nssh log export                      # Pick and export to ./hostname_YYYY-MM-DD_session-NNN.txt
nssh log export --txt                # Explicit text format
nssh log export --gif                # Pick and export to ./hostname_YYYY-MM-DD_session-NNN.gif
nssh log export --output demo.txt    # Custom output path
nssh log export --file session.cast --output demo.gif --gif

# Clean up old recordings
nssh log cleanup --dry-run  # Preview what would be deleted
nssh log cleanup            # Actually delete old recordings
```

**Interactive Mode**: Commands without `--file` launch an fzf picker showing available recordings sorted by modification time. Use arrow keys and fuzzy search to select a recording. The `--date` option filters picker results (defaults to recent sessions).

See [docs/examples/help/nssh-log.txt](examples/help/nssh-log.txt) for complete command reference. For privacy considerations when recording is enabled by default, see [Security Best Practices](#recording-privacy).

### nssh benchmark (Performance Analysis)

The `nssh benchmark run` command provides structured performance analysis with warmup runs, multiple samples, and statistical summaries. Use `NSSH_DEBUG=1 nssh hostname` to see timing breakdown for connection stages during normal usage. For detailed performance analysis, timing stages explanation, and benchmarking tools, see [ARCHITECTURE.md - Debugging and Profiling](ARCHITECTURE.md#debugging-and-profiling) and [docs/examples/help/nssh-benchmark.txt](examples/help/nssh-benchmark.txt).

`nssh benchmark run` has two useful modes:

- **Structured (default):** Enables instrumentation and reports each stage (pty-start, host-selection, credential-vault, ssh-connection, recording-session, pty-teardown, etc.). Measures timing from within the Python CLI process. Use this to identify stage-level regressions.
- **Simple-only (`--simple-only`):** Disables instrumentation and times the entire binary invocation (shell → interpreter startup → connect workflow). This measures what you experience: "how long from pressing Enter to getting a shell prompt."

By default, the benchmark command respects your recording configuration (from `NSSH_RECORD` env var or `config.toml`), so you can measure the real user experience with recording enabled or disabled. Use `--no-record` to force disable recording and measure pure SSH connection overhead without recording influence.

For a complete list of timing stages and their meanings (including nested stages like ssh-connection within recording-session), see the Timing Stages table in [ARCHITECTURE.md](ARCHITECTURE.md#debugging-and-profiling).

### nssh self (CLI & Shell Management)

Optional shell integration for Fish, Bash, Zsh with history tracking and tab completion.

```bash
# Install shell helpers + completions
nssh self install --install-shell-helpers --append-shell-snippet ~/.bashrc

# Preview changes
nssh self install --dry-run

# Remove shell helpers (keeps CLI binary)
nssh self cleanup
```

**Shell history:** Tracks both search term (`nssh core`) and resolved hostname (`nssh core-switch-01`). Atuin compatible.

For complete command reference and options, see [docs/examples/help/nssh-self.txt](examples/help/nssh-self.txt).

## Security Best Practices

nssh implements multiple security layers to protect credentials and minimize attack surface.

### Credential Protection

Credentials in `~/.ssh/nssh_credentials.age` are encrypted using [age](https://github.com/FiloSottile/age) (ChaCha20-Poly1305 + X25519). Passwords are never written to plaintext, never appear in shell history or process listings, and are streamed directly into the PTY. Interactive prompts only (no CLI args); double-prompted to prevent typos.

### Recording Privacy

When session recording is enabled by default, be conscious about when connections are being recorded. Sessions may capture sensitive information including passwords, command output, and private data displayed in the terminal.

Use host filtering in your config to exclude sensitive systems:

```toml
[recording]
enabled = true
exclude_hosts = ["prod-*"]  # Skip all production hosts
# Or use include_hosts = ["lab-*"] to only record specific hosts
```

For individual compliance-sensitive sessions, disable recording with the `NSSH_RECORD` environment variable:

```bash
NSSH_RECORD=0 nssh prod-db-01  # Disable recording for this session
```

### File Permissions

nssh enforces secure file permissions automatically. Sensitive files (credentials, age keys, backups) are `600`; configs/indexes are `644`. Verify: `ls -la ~/.ssh/nssh_credentials.age ~/.config/age/keys.txt`

### Key Management

Generate age key during initial setup: `mkdir -p ~/.config/age && age-keygen -o ~/.config/age/keys.txt`. Each user needs their own key - no shared credentials.

### Safe Debugging

`NSSH_DEBUG=1` logs timing only, never credentials. View passwords: `nssh cred get`. List without passwords: `nssh cred list`.

### Key Rotation

If compromised: decrypt with old key, generate new key, re-encrypt, verify, securely delete temp files. See [examples/nssh_credentials.json](examples/nssh_credentials.json) for manual editing format.

## Troubleshooting

### Quick Reference

| Symptom | Quick Check | Solution Reference |
|---------|-------------|-------------------|
| "No hosts found" | `nssh host list` | [Host not found](#host-not-found-errors) |
| "Failed to decrypt credentials" | `ls ~/.config/age/keys.txt` | [No credential found](#no-credential-found-but-one-should-exist) |
| "Command not found: nssh cred" | `which nssh` | Install nssh |
| Connection hangs | `nssh hostname -vvv` | [Connection hangs](#connection-hangs-or-times-out) |
| Wrong auth method | `nssh host list hostname` | [Password auth not working](#password-authentication-not-working) |
| Stale SSH session | See error message | [Authorization denied](#authorization-denied-cannot-authorize-shell-for-unknown-sessionid) |

### "No credential found" but one should exist

Verify credential configuration:
```bash
nssh cred ctx list            # Check context mappings
nssh cred list hostname       # Check host-specific credentials
```

If credentials exist but aren't matching, check that your SSH config file is mapped to the correct context with `nssh cred ctx list`.

### "Host not found" errors

Check SSH config parsing:
```bash
nssh host list hostname         # See if host is recognized
cat ~/.ssh/.nssh_host_index | grep hostname  # Check index directly
```

If the host exists in your SSH config but isn't showing up, the index may be stale. It auto-rebuilds when using `nssh host` commands, but you can rebuild manually with [CONTRIBUTING.md](../CONTRIBUTING.md) instructions.

### Connection hangs or times out

Enable verbose SSH output to diagnose:
```bash
nssh hostname -vvv             # Pass -vvv to underlying ssh command
```

This shows authentication attempts, key exchanges, and where the connection stalls.

### Password authentication not working

Verify the SSH config has password auth enabled:
```bash
nssh host list hostname         # Check Auth column shows "passwd"
```

If it shows "key", run `nssh host update hostname` to realign authentication automatically.

### Performance debugging

Enable timing instrumentation:
```bash
NSSH_DEBUG=1 nssh hostname     # Shows timing breakdown
```

For detailed timing information and troubleshooting, see [nssh benchmark (Performance Analysis)](#nssh-benchmark-performance-analysis) and [ARCHITECTURE.md - Timing Instrumentation](ARCHITECTURE.md#timing-instrumentation).

### "Authorization denied: Cannot authorize shell for unknown sessionId"

If you see an error like:
```
Authorization denied: Cannot authorize shell for unknown sessionId 4110
```

This indicates a stale SSH connection multiplexing session. Close the existing multiplexed connection:
```bash
ssh -O exit <hostname>
```

Then retry your nssh connection. The `-O exit` command cleanly terminates the SSH master connection, allowing a fresh session to be established.
