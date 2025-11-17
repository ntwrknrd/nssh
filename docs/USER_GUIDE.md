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
  - [nssh (Connection Wrapper)](#nssh-connection-wrapper)
  - [nssh cred (Credential Management)](#nssh-cred-credential-management)
  - [nssh host (SSH Config Management)](#nssh-host-ssh-config-management)
  - [nssh log (Session Recording)](#nssh-log-session-recording)
  - [nssh benchmark (Performance Analysis)](#nssh-benchmark-performance-analysis)
  - [nssh install-shell (Shell Integration)](#nssh-install-shell-shell-integration)
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
# Connect to a host (fuzzy search)
nssh core-switch

# Connect as different user
nssh admin@firewall

# List all configured hosts
nssh host list

# Add a new host with credential
nssh host add switch.example.com --user admin --password

# Manage credentials
nssh cred list-contexts
nssh cred add-context-cred work --username alice
nssh cred add hostname --username admin
```

## Core Concepts

### Credential Resolution

nssh resolves credentials using this priority order:

1. **Host-specific credential** matching username → password auth
2. **Context fallback credential** matching username (or default) → password auth
3. **No matching credential** → SSH keys or interactive prompt

**Key concepts:**

- **Context Fallbacks** - Each context stores a single fallback credential that only applies when a host inside that Include file has no host-specific password
- **Host Overrides** - Host-specific credentials take priority over context fallbacks
- **User Prefixing** - `user@host` syntax preserves your intent; falls back to SSH keys if no credential exists

**Context Definition:**

Contexts map SSH config Include files to a single fallback credential. Example: All hosts in `~/.ssh/work_hosts` can automatically use the `alice` fallback username/password unless a host-specific credential exists or a different username is specified.

For the detailed five-step resolution algorithm, see [ARCHITECTURE.md - Credential Resolution Algorithm](ARCHITECTURE.md#credential-resolution-algorithm).

### Extended Authentication Scenarios

#### Scenario 1: Host with Specific Credential

```bash
# Configuration:
# - Host "firewall" has credential: username="admin"

nssh firewall                  # → Uses admin credential (password auth)
nssh root@firewall             # → No root credential, uses SSH key
nssh admin@firewall            # → Uses admin credential (password auth)
```

**Resolution path for `nssh firewall`:**
1. Check host credentials → found "admin" → use it
2. (context check skipped because host credential found)
3. Execute: `sshpass -d <FD> ssh -o User="admin" firewall` (credential streamed via pipe)

**Resolution path for `nssh root@firewall`:**
1. Check host credentials for "root" → not found
2. Check context fallback credential for "root" → not found
3. Return None → execute: `ssh root@firewall` (SSH key auth)

For more detailed credential resolution scenarios and the full algorithm, see [ARCHITECTURE.md - Credential Resolution Algorithm](ARCHITECTURE.md#credential-resolution-algorithm).

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

nssh supports optional configuration via `~/.config/nssh/config.toml`. Configure encryption paths, recording settings, host filters, and cleanup policies. All settings have sensible defaults. See [examples/config.toml](examples/config.toml) for a complete annotated example. Environment variables take precedence over config file settings.

For SSH config structure examples, see [examples/homelab_hosts](examples/homelab_hosts), [examples/work_hosts](examples/work_hosts), and [examples/ssh_config](examples/ssh_config).

### Data Files

| File | Purpose | Created By |
|------|---------|------------|
| `~/.ssh/config` | Main SSH config with Include directives | User/nssh host |
| `~/.ssh/.nssh_host_index` | Pre-compiled host index for fast lookups | nssh host |
| `~/.ssh/nssh_credentials.age` | Age-encrypted credential store | nssh cred |
| `~/.config/age/keys.txt` | Age encryption key | age-keygen |
| `~/.ssh/backups/*.bak` | Timestamped config backups | nssh host |
| `~/.config/nssh/config.toml` | Recording & encryption config (optional) | User |
| `~/.local/state/nssh/casts/` | Session recordings (if enabled) | nssh |

See [examples/](examples/) for file format references.

## CLI Usage

### nssh (Connection Wrapper)

```bash
nssh core-switch-01      # Exact match connects immediately
nssh core                # Partial match opens fzf when multiple hits
nssh admin@firewall      # Force connection as admin user
nssh -u netadmin router  # Override username via flag
nssh -V hostname         # Enable verbose SSH output for debugging
nssh hostname -vvv       # Pass raw ssh options through
```

`nssh` preserves any `user@` prefix throughout fuzzy selection. When multiple candidates match, it launches `fzf` so you can choose interactively; single matches connect automatically. For all command options, run `nssh --help` or see [docs/examples/help/nssh.txt](examples/help/nssh.txt).

### nssh cred (Credential Management)

Contexts organize credentials around SSH config files (e.g., `work_hosts`).

```bash
nssh cred create-context work --file work_hosts
nssh cred add-context-cred work --username alice
nssh cred list-contexts
nssh cred delete-context work
```

Host-specific overrides allow fine-grained control:

```bash
nssh cred add special-switch --username netadmin
nssh cred add firewall --username readonly
nssh cred list-host firewall
nssh cred delete firewall --username readonly
```

See [docs/examples/help/nssh-cred.txt](examples/help/nssh-cred.txt) for the exhaustive command list and option descriptions.

### nssh host (SSH Config Management)

Add hosts interactively or via flags:

```bash
nssh host add                           # Guided flow with previews (includes connection test)
nssh host add switch.example.com --user admin --password
nssh host add device.local --file homelab_hosts --key
nssh host add legacy-server --password --no-test  # Skip connection test
nssh host rm old-server
nssh host list 192.168
nssh host sort --file homelab_hosts
nssh host update hostname --auth password           # password, keyboard-interactive, publickey
nssh host update hostname --compat kex              # kex, macs, ciphers, hostkey
nssh host update hostname --compat kex --compat macs  # multiple compat options
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
nssh log play                    # Pick from today's recordings
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

**Interactive Mode**: Commands without `--file` launch an fzf picker showing available recordings sorted by modification time. Use arrow keys and fuzzy search to select a recording. The `--date` option filters picker results (defaults to today).

See [docs/examples/help/nssh-log.txt](examples/help/nssh-log.txt) for complete command reference. For privacy considerations when recording is enabled by default, see [Security Best Practices](#recording-privacy).

### nssh benchmark (Performance Analysis)

Use `NSSH_DEBUG=1 nssh hostname` to see timing breakdown for connection stages. For detailed performance analysis, benchmarking tools, and metrics, see [ARCHITECTURE.md - Performance Metrics](ARCHITECTURE.md#performance-metrics) and [docs/examples/help/nssh-benchmark.txt](examples/help/nssh-benchmark.txt).

### nssh install-shell (Shell Integration)

nssh provides optional shell integration for Fish, Bash, and Zsh with shell history tracking and tab completion. Installation deploys wrapper scripts and completion files to your local environment.

**Basic installation:**

```bash
# Install shell wrapper + completions
nssh install-shell --shell-profile ~/.bashrc
nssh install-shell --shell-profile ~/.zshrc
nssh install-shell --shell-profile ~/.config/fish/config.fish

# Preview without changes
nssh install-shell --shell-profile ~/.bashrc --dry-run
```

**What gets installed:**

| Component | Location | Purpose |
|-----------|----------|---------|
| nssh wrapper script | `~/.local/share/nssh/nssh-wrapper.sh` | Shell history integration |
| Python CLI shim | `~/.local/share/nssh/nssh-python-cli` | Direct Typer entrypoint used by the wrapper |
| Fish function | `~/.config/fish/functions/nssh.fish` | Fish-specific wrapper |
| Fish completions | `~/.config/fish/completions/nssh.fish` | Tab completion for nssh subcommands + hosts |

**Shell history:** The wrapper tracks both your search term (`nssh core`) and the selected hostname (`nssh core-switch-01`), preserving workflow context. Compatible with Atuin for synced history across devices.

**Tab completions (Fish only):** A single completion file powers subcommand suggestions plus hostname completion for the bare `nssh <host>` path. The completion script queries `nssh __list-subcommands` so the top-level list stays in sync without manual edits.

For manual installation, advanced options, and complete command reference, see [docs/examples/help/nssh-install-shell.txt](examples/help/nssh-install-shell.txt).

## Security Best Practices

nssh implements multiple security layers to protect credentials and minimize attack surface.

### Credential Protection

Credentials in `~/.ssh/nssh_credentials.age` are encrypted using [age](https://github.com/FiloSottile/age) with modern encryption (ChaCha20-Poly1305 + X25519). Credentials are never written to disk in plaintext, never appear in shell history or process listings, and are passed securely to sshpass via file descriptor.

### Password Handling

nssh requires interactive password prompts and never accepts passwords via CLI arguments. When adding credentials, nssh prompts twice to prevent typos.

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

nssh enforces secure file permissions automatically:

| File | Permissions | Purpose |
|------|-------------|---------|
| `~/.ssh/nssh_credentials.age` | `600` | Encrypted credential store |
| `~/.config/age/keys.txt` | `600` | Age private key |
| `~/.ssh/.nssh_host_index` | `644` | Host index (no secrets) |
| `~/.ssh/config` | `644` | SSH config (no secrets) |
| `~/.ssh/backups/*.bak` | `600` | Config backups |

Verify with: `ls -la ~/.ssh/nssh_credentials.age ~/.config/age/keys.txt`

### Key Management

Generate your age key during initial setup:

```bash
mkdir -p ~/.config/age
age-keygen -o ~/.config/age/keys.txt
```

Each user must have their own age key and credential file. nssh does not support shared credentials across users. See [Key Rotation](#key-rotation) and [Multi-User Security](#multi-user-security) below for more details.

### Safe Debugging

When `NSSH_DEBUG=1` is enabled, only timing data is logged. Credentials are never logged:

```bash
NSSH_DEBUG=1 nssh hostname  # Shows timing, no credentials
```

To view credentials explicitly: `nssh cred show` (displays plaintext passwords to stdout)
To list without passwords: `nssh cred list`, `nssh cred list-contexts`, `nssh cred list-host hostname`

### Manual Credential Editing (Advanced)

For emergency manual editing of the encrypted credential store, decrypt with `age -d`, edit the JSON, and re-encrypt with `age -r`. Use `nssh cred` commands instead when possible. See [docs/examples/nssh_credentials.json](examples/nssh_credentials.json) for format reference.

### Key Rotation

If your age key is compromised: backup credentials, decrypt with old key, generate new age key, re-encrypt with new public key, verify decryption works, and securely delete temporary files.

```bash
# Decrypt, rotate, and re-encrypt
age -d -i ~/.config/age/keys.txt ~/.ssh/nssh_credentials.age > /tmp/creds.json
mv ~/.config/age/keys.txt ~/.config/age/keys.txt.old
age-keygen -o ~/.config/age/keys.txt
age -r "$(age-keygen -y ~/.config/age/keys.txt)" -o ~/.ssh/nssh_credentials.age /tmp/creds.json
age -d -i ~/.config/age/keys.txt ~/.ssh/nssh_credentials.age | jq .  # Verify
rm -P /tmp/creds.json  # macOS; use shred -u on Linux
```

Rotate keys after suspected compromise, security audits, or system compromise. Test decryption before deleting temporary files.

### Multi-User Security

Each user must have their own age key (`~/.config/age/keys.txt`) and credential file (`~/.ssh/nssh_credentials.age`). nssh does not support shared credentials - age keys are user-specific and sharing compromises the security model. For shared infrastructure, use separate nssh setups per user with individual credential stores.

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
nssh cred list-contexts        # Check context mappings
nssh cred list-host hostname   # Check host-specific credentials
```

If credentials exist but aren't matching, check that your SSH config file is mapped to the correct context with `nssh cred list-contexts`.

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

If it shows "key", update the host: `nssh host update hostname --auth password`

### Performance debugging

Enable timing instrumentation:
```bash
NSSH_DEBUG=1 nssh hostname     # Shows timing breakdown
```

For detailed performance metrics and troubleshooting, see [nssh benchmark (Performance Analysis)](#nssh-benchmark) and [ARCHITECTURE.md](ARCHITECTURE.md#performance-metrics).

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

---

For architecture internals, see [ARCHITECTURE.md](ARCHITECTURE.md). For contributor workflow, see [CONTRIBUTING.md](../CONTRIBUTING.md).
