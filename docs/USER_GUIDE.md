# nssh User Guide

This guide covers the full CLI usage, credential workflows, configuration details, and development notes. Pair it with the main [README.md](../README.md) for a high-level overview.

## Table of Contents
- [Getting Started](#getting-started)
  - [Prerequisites](#prerequisites)
  - [Initial Setup](#initial-setup)
  - [SSH Include Files and Context Credentials](#ssh-include-files-and-context-credentials)
  - [First Connection](#first-connection)
- [Quick Reference](#quick-reference)
- [Core Concepts](#core-concepts)
  - [Credential Resolution](#credential-resolution)
- [Configuration](#configuration)
  - [Environment Variables](#environment-variables)
  - [Configuration File](#configuration-file)
- [CLI Usage](#cli-usage)
- [nssh (Interactive Connector)](#nssh-interactive-connector)
  - [nssh cp (File Transfer)](#nssh-cp-file-transfer)
  - [nssh ctx (Context Management)](#nssh-ctx-context-management)
  - [nssh host (SSH Config Management)](#nssh-host-ssh-config-management)
  - [nssh log (Session Recording)](#nssh-log-session-recording)
  - [nssh benchmark (Performance Analysis)](#nssh-benchmark-performance-analysis)
  - [nssh self (CLI & Shell Management)](#nssh-self-cli--shell-management)
- [Security Best Practices](#security-best-practices)
  - [Credential Protection](#credential-protection)
  - [Recording Privacy](#recording-privacy)
  - [File Permissions](#file-permissions)
  - [Key Management](#key-management)
  - [Safe Debugging](#safe-debugging)
  - [Key Rotation](#key-rotation)
- [Troubleshooting](#troubleshooting)

## Getting Started

This section walks you through setting up nssh from scratch to your first SSH connection.

### Prerequisites

Before installing nssh, ensure you have these tools installed:

1. **OpenSSH** - SSH client (ships with most Unix systems)
   ```bash
   ssh -V  # Should show OpenSSH version
   ```

2. **age** - Modern file encryption tool for credential storage
   ```bash
   # macOS
   brew install age

   # Linux (most distros)
   # Download from https://github.com/FiloSottile/age/releases
   ```

3. **fzf** - Fuzzy finder for interactive host selection
   ```bash
   # macOS
   brew install fzf

   # Linux
   # apt: sudo apt install fzf
   # dnf: sudo dnf install fzf
   ```

4. **uv** - Python package installer
   ```bash
   curl -LsSf https://astral.sh/uv/install.sh | sh
   ```

5. **Python 3.14** - Install via uv
   ```bash
   uv python install 3.14
   ```

6. **(Optional) asciinema v3+** - For session recording features
   ```bash
   # macOS
   brew install asciinema

   # Linux: https://docs.asciinema.org/getting-started/
   ```

### Initial Setup

**Step 1: Install nssh**

```bash
# Clone the repository
git clone https://github.com/ntwrknrd/nssh.git
cd nssh

# Install nssh binary to ~/.local/bin
uv tool install .
```

**Step 2: Initialize nssh**

Run the interactive setup wizard:

```bash
nssh self init
```

This command will guide you through:
- Creating age encryption key (if missing)
- Setting up SSH config structure
- Installing shell integration (auto-detects your shell)
- Creating first include file (optional)
- Setting up context credential (optional)
- Creating config.toml (optional)

**Manual setup options:**

If you prefer to skip interactive prompts:
```bash
# Preview without making changes
nssh self init --dry-run

# Skip shell integration setup
nssh self init --skip-shell
```

After running `init`, reload your shell:
```bash
source ~/.bashrc  # or ~/.zshrc, or restart your terminal
```

**Step 3: Verify installation**

```bash
nssh --help        # Should show nssh help
nssh self status   # Shows what's configured and next steps
```

After initialization, you'll have:
- `~/.local/bin/nssh` - The nssh binary (installed by uv)
- `~/.config/nssh/age.key` - Your age encryption key
- `~/.ssh/config` - SSH config with Include structure
- `~/.ssh/conf.d/` - Directory for include files (if created)

The following files are created automatically as you use nssh:
- `~/.local/share/nssh/credentials.age` - Encrypted password storage (when you add credentials)
- `~/.local/state/nssh/host_index` - Fast host lookup index (when you add hosts)
- `~/.local/share/nssh/backups/` - SSH config backups (timestamped, when you modify config)

### What `nssh self init` Does

The `nssh self init` command is an interactive setup wizard that automates the entire configuration process. Here's what happens when you run it:

**1. System Validation**
- Checks for required dependencies (age, fzf, Python 3.14+)
- Verifies nssh is installed and on PATH
- Shows clear error messages with installation links if anything is missing

**2. Age Encryption Key**
- Checks if `~/.config/nssh/age.key` exists
- If missing, offers to create it with `age-keygen`
- Sets secure permissions (0600) automatically

**3. SSH Config Structure**
- Checks for `~/.ssh/config`
- If missing, offers to create with `Include` directive structure
- Creates `~/.ssh/conf.d/` directory for include files
- Uses best practices (ServerAliveInterval 60, Include pattern)

**4. Shell Integration** (Auto-detected)
- Detects your shell from `$SHELL` environment variable
- Suggests appropriate rc file (`.bashrc`, `.zshrc`, `.config/fish/config.fish`)
- Offers to install shell helpers and append sourcing snippet
- Fish users get completions automatically

**5. First Include File** (Optional)
- Offers to create your first include file (e.g., `default`, `work`, `homelab`)
- Creates file in `~/.ssh/conf.d/` with proper permissions
- Skipped if include files already exist

**6. Context Credential** (Optional)
- If include file was created, offers to set up context credential
- Prompts for context name, username, and password
- Creates encrypted credential as fallback for all hosts in that include file
- Password entered securely (not echoed, confirmed twice)

**7. Config Template** (Optional)
- Offers to create `~/.config/nssh/config.toml` from example
- Allows customization of recording, encryption paths, etc.
- Completely optional (defaults work fine)

**Interactive vs. Non-Interactive:**
- **Default mode**: Prompts for each step with sensible defaults
- **`--dry-run` flag**: Shows what would be done without making changes
- **`--skip-shell` flag**: Skips shell integration setup

**After init completes:**
- Run `nssh self status` to see what was configured
- Get actionable next steps (e.g., "Add first host: nssh host add")
- All changes tracked in manifest for clean uninstall via `nssh self uninstall`

### SSH Include Files and Context Credentials

Before adding hosts, it's recommended to organize your SSH config using Include files and set up context credentials. This provides fallback authentication for entire groups of hosts.

> **NOTE:** `nssh self init` can guide you through this setup interactively. The instructions below are for manual setup or for understanding how it works.

**What are Include files?**

SSH config Include files let you split your configuration into multiple files (e.g., `work_hosts`, `homelab_hosts`, `prod_hosts`). This keeps your config organized and enables nssh's context-based credential fallbacks.

**Step 1: Create an Include file**

```bash
# Create a file for your hosts (e.g., work equipment)
touch ~/.ssh/work_hosts

# Or for homelab devices
touch ~/.ssh/homelab_hosts
```

**Step 2: Reference it in your main SSH config**

Add an Include directive at the top of `~/.ssh/config`:

```bash
# Add to ~/.ssh/config
Include work_hosts
Include homelab_hosts
```

**Step 3: Create a context credential (fallback)**

A context credential provides default authentication for all hosts in an Include file:

```bash
# Create a context named "work" (interactive - prompts for SSH config file and credentials)
nssh ctx add work

# Or use flags to set specific properties after creation
nssh ctx edit work --ssh-config work_hosts --username alice --password
```

**Why does this matter?**

When you connect to a host, nssh uses this five-step credential resolution:

1. **Host-specific credential** (if you added one with `nssh host edit hostname`)
2. **Context fallback credential** (the credential you just created above)
3. **SSH keys** (if no password credential found)

This means:
- Add 20 switches to `work_hosts` → only set the password once in the context credential
- Override specific devices → use `nssh host edit special-switch` to add credentials
- No credential needed → nssh falls back to SSH key authentication

For detailed credential resolution behavior and examples, see [Core Concepts - Credential Resolution](#credential-resolution).

### First Connection

**Option 1: Add host to an Include file with context credential**

If you set up a context credential above, add hosts to that Include file. The interactive flow will prompt you to select which context/Include file to use:

```bash
# Interactive flow - select the work context when prompted
nssh host add switch.example.com

# The context provides username and password automatically
```

The host will automatically use the context credential for authentication.

**Option 2: Add host with its own password**

For hosts that need different credentials, create a host-specific credential:

```bash
# Interactive guided setup (recommended for first time)
nssh host add

# Or specify details directly
nssh host add admin@switch.example.com

# You'll be prompted for the password (entered twice for safety)
```

This does three things:
1. Adds the host to your SSH config (`~/.ssh/config`)
2. Stores the encrypted password in `~/.local/share/nssh/credentials.age`
3. Tests the connection automatically (use `--force` to skip)

**Option 3: Add host with SSH key authentication**

If you prefer SSH keys:

```bash
nssh host add myuser@server.example.com --auth key
```

**Connect to your host**

```bash
# Exact hostname - connects immediately
nssh switch.example.com

# Partial match - opens fuzzy finder if multiple matches
nssh switch

# Connect as different user
nssh admin@switch.example.com
```

**What just happened?**

1. nssh looked up "switch.example.com" in your SSH config
2. Found the matching credential (username + encrypted password)
3. Decrypted the password using your age key
4. Injected the password through an in-process PTY connector
5. Established the SSH connection

**Next Steps**

- View your hosts: `nssh host list`
- Manage contexts: `nssh ctx list`
- View recordings (if enabled): `nssh log list`
- Benchmark performance: `nssh benchmark ssh switch.example.com`

**Common First-Time Issues**

| Issue | Solution |
|-------|----------|
| `nssh: command not found` | Ensure `~/.local/bin` is in your PATH: `export PATH="$HOME/.local/bin:$PATH"` |
| `Failed to decrypt credentials` | Verify age key exists: `ls ~/.config/nssh/age.key` |
| `age-keygen: command not found` | Install age: `brew install age` (macOS) or download from [age releases](https://github.com/FiloSottile/age/releases) |
| Connection test fails | Check credentials are correct, or skip test with `--force` flag |

See the sections below for detailed CLI usage and advanced features.

## Quick Reference

```bash
nssh core-switch                           # Connect (fuzzy search)
nssh admin@firewall                        # Connect as different user
nssh cp hostname:~/file.txt ./             # Pull file from remote
nssh cp ./file.txt hostname:~/             # Push file to remote
nssh host list                             # List hosts
nssh host add admin@switch.example.com
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
| `NSSH_AGE_KEY` | Override age encryption key file path | `~/.config/nssh/age.key` |
| `NSSH_CRED_FILE` | Override encrypted credentials file path | `~/.local/share/nssh/credentials.age` |
| `NSSH_BACKUP_DIR` | Override SSH config backup directory | `~/.local/share/nssh/backups` |
| `NSSH_DEBUG` | Enable detailed timing logs | (disabled) |
| `NSSH_RECORD` | Override recording setting (`0`, `1`, `force`) | (from config) |
| `NSSH_RECORD_HEADLESS` | Enable headless recording mode (`1`) | (disabled) |
| `NSSH_RECORD_DIR` | Override recording storage directory | `~/.local/state/nssh/casts` |
| `NSSH_CONFIG` | Override config file path | `~/.config/nssh/config.toml` |
| `ASCIINEMA_SERVER_URL` | Server URL for `nssh log upload` | (none) |
| `NSSH_BIN_DIR` | Install directory for executables | `~/.local/bin` |
| `NSSH_SHARE_DIR` | Install directory for shared assets | `~/.local/share/nssh` |
| `XDG_CONFIG_HOME` | Base directory for configuration files | `~/.config` |
| `XDG_STATE_HOME` | Base directory for state data | `~/.local/state` |

For recording configuration details, see [examples/config/config.toml](examples/config/config.toml).

### Configuration File

Optional config via `~/.config/nssh/config.toml` for encryption paths, recording settings, host filters. Environment variables override config. See [examples/config/config.toml](examples/config/config.toml).

### Data Files

| File | Purpose |
|------|---------|
| `~/.ssh/config` | Main SSH config with Include directives |
| `~/.local/state/nssh/host_index` | Pre-compiled host index for fast lookups |
| `~/.local/share/nssh/credentials.age` | Age-encrypted credential store |
| `~/.config/nssh/age.key` | Age encryption key |
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

`nssh` preserves any `user@` prefix throughout fuzzy selection. When multiple candidates match, it launches `fzf` so you can choose interactively; single matches connect automatically. For all command options, run `nssh --help` or see [examples/help/nssh.txt](examples/help/nssh.txt).

#### How nssh Works

nssh uses an in-process PTY connector for password injection during SSH connections. For technical details, see [ARCHITECTURE.md - PTY Connector Architecture](ARCHITECTURE.md#pty-connector-architecture).

### nssh cp (File Transfer)

Standard SCP file transfer with automatic password injection from nssh's credential vault.

```bash
# Pull file from remote host
nssh cp hostname:~/file.txt ./

# Push file to remote host
nssh cp ./file.txt hostname:~/

# Transfer directory recursively
nssh cp -r hostname:~/dir ./local/

# Connect as specific user
nssh cp admin@hostname:/var/log/app.log ./
```

Maintains standard SCP CLI syntax while using nssh's credential resolution. Requires exact hostname match for safety.

**Common options:**
- `-r` - Copy directories recursively
- `-p` - Preserve modification times and modes
- `-q` - Quiet mode (disable progress meter)
- `-v` - Verbose mode

For complete options, see [examples/help/nssh.txt](examples/help/nssh.txt).

### nssh ctx (Context Management)

Contexts organize credentials around SSH config files (e.g., `work_hosts`).

```bash
nssh ctx add work                                    # Interactive setup
nssh ctx edit work --ssh-config work_hosts --username alice
nssh ctx list
nssh ctx rm work
```

Host-specific credentials are managed through `nssh host edit`:

```bash
nssh host edit special-switch    # Add/modify credentials for existing host
nssh host get firewall           # View host details including credentials
```

See [examples/help/ctx.txt](examples/help/ctx.txt) for the context command list and [examples/help/host.txt](examples/help/host.txt) for host commands.

### nssh host (SSH Config Management)

Add hosts interactively or via flags:

```bash
nssh host add                           # Guided flow with previews (includes connection test)
nssh host add admin@switch.example.com  # password auth (default)
nssh host add device.local --auth key
nssh host add legacy-server --auth password --force  # Skip connection test
nssh host rm old-server
nssh host list --select 192.168
nssh host sort --select homelab
```

For detailed information about SSH compatibility detection and troubleshooting for legacy devices, see [ARCHITECTURE.md - SSH Compatibility Detection and Remediation](ARCHITECTURE.md#ssh-compatibility-detection-and-remediation). Use `nssh host add --help` or see [examples/help/host.txt](examples/help/host.txt) for command options.

#### Batch Operations

Both `nssh host add` and `nssh host rm` support batch operations from `.txt`, `.csv`, or `.json` files:

```bash
# Add multiple hosts from file
nssh host add ./hosts.csv --dry-run    # Preview first
nssh host add ./hosts.csv

# Remove multiple hosts from file
nssh host rm ./hosts.csv --dry-run
nssh host rm ./hosts.csv
```

**CSV Format:**

```csv
hostname,user,port,context,password,host
switch-01.example.com,admin,22,work,,
switch-02.example.com,admin,22,work,,
rpi-a.home.arpa,root,22,homelab,,rpi-a
```

| Field | Required | Description |
|-------|----------|-------------|
| `hostname` | Yes | FQDN (e.g., `switch.example.com`) |
| `host` | No | SSH alias. If omitted, derived from hostname by splitting on first dot (e.g., `switch.example.com` -> `switch`) |
| `user` | No | SSH username (defaults to context or `$USER`) |
| `port` | No | SSH port (defaults to 22) |
| `context` | No | Target context/include file |
| `password` | No | Host-specific password |

**Alias Derivation:**

When the `host` field is omitted, the SSH alias is derived from the `hostname` by taking the first segment before the first dot:

- `switch-01.example.com` -> `switch-01`
- `rpi-b.home.arpa` -> `rpi-b`
- `firewall.prod.internal` -> `firewall`

This allows you to use the same CSV file for both add and remove operations. If you need a different alias (e.g., the hostname doesn't contain dots), specify it explicitly in the `host` column.

**Text Format (`.txt`):**

One hostname per line, comments with `#`:

```
switch-01.example.com
switch-02.example.com
# This is a comment
rpi-a.home.arpa
```

**JSON Format:**

```json
[
  {"hostname": "switch-01.example.com", "user": "admin", "context": "work"},
  {"hostname": "rpi-a.home.arpa", "host": "rpi-a", "context": "homelab"}
]
```

### nssh log (Session Recording)

nssh integrates with [asciinema v3](https://asciinema.org) for automatic SSH session recording. Recordings are organized by hostname and date in `~/.local/state/nssh/casts/`.

For configuration details, see [Configuration File](#configuration-file).

Manage recorded sessions:

```bash
# List recordings (filter by regex pattern)
nssh log list
nssh log list --select lab-switch-01
nssh log list --select "2025-11-14"
nssh log list --select "lab.*2025-11-15"  # Regex pattern matching

# Play sessions (interactive picker)
nssh log play                    # Pick from recordings via fzf

# Upload to asciinema server (requires server configuration)
# Configure asciinema server (add to ~/.bashrc, ~/.zshrc, ~/.config/fish/config.fish, etc.)
export ASCIINEMA_SERVER_URL=https://asciinema.example.com

nssh log upload                  # Pick recording to upload

# Export recordings (saves to current directory by default)
nssh log export                  # Pick and export via fzf

# Delete old recordings
nssh log delete                          # Interactive picker
nssh log delete --older-than 30          # Delete recordings older than N days
nssh log delete --select hostname        # Delete by regex pattern
nssh log delete --older-than 30 --dry-run  # Preview deletion
```

**Interactive Mode**: Commands launch an fzf picker showing available recordings sorted by modification time. Use arrow keys and fuzzy search to select a recording.

See [examples/help/log.txt](examples/help/log.txt) for complete command reference. For privacy considerations when recording is enabled by default, see [Security Best Practices](#recording-privacy).

### nssh benchmark (Performance Analysis)

The `nssh benchmark` command provides structured performance analysis with warmup runs, multiple samples, and statistical summaries. Use `NSSH_DEBUG=1 nssh hostname` to see timing breakdown for connection stages during normal usage. For detailed performance analysis, timing stages explanation, and benchmarking tools, see [ARCHITECTURE.md - Debugging and Profiling](ARCHITECTURE.md#debugging-and-profiling).

**Available subcommands:**

- `nssh benchmark ssh HOST` - Benchmark SSH connection overhead
- `nssh benchmark scp HOST` - Benchmark SCP file transfer performance

`nssh benchmark ssh` has two useful modes:

- **Structured (default):** Enables instrumentation and reports each stage (pty-start, host-selection, credential-vault, ssh-connection, recording-session, pty-teardown, etc.). Measures timing from within the Python CLI process. Use this to identify stage-level regressions.
- **Simple-only (`--simple-only`):** Disables instrumentation and times the entire binary invocation (shell → interpreter startup → connect workflow). This measures what you experience: "how long from pressing Enter to getting a shell prompt."

By default, `nssh benchmark ssh` respects your recording configuration (from `NSSH_RECORD` env var or `config.toml`), so you can measure the real user experience with recording enabled or disabled. Use `--no-record` to force disable recording and measure pure SSH connection overhead without recording influence.

`nssh benchmark scp` accepts a `--size` option to control the test file size in KB (default: 100KB).

For a complete list of timing stages and their meanings (including nested stages like ssh-connection within recording-session), see the Timing Stages table in [ARCHITECTURE.md](ARCHITECTURE.md#debugging-and-profiling). For command options, see [examples/help/benchmark.txt](examples/help/benchmark.txt).

### nssh self (CLI & Shell Management)

Optional shell integration for Fish, Bash, Zsh with history tracking and tab completion.

```bash
# Install shell helpers + completions (interactive)
nssh self init

# Preview changes
nssh self init --dry-run

# Skip shell integration
nssh self init --skip-shell

# Uninstall (removes shell helpers and CLI)
nssh self uninstall
```

**Shell history:** Tracks both search term (`nssh core`) and resolved hostname (`nssh core-switch-01`). Atuin compatible.

For complete command reference and options, see [examples/help/self.txt](examples/help/self.txt).

## Security Best Practices

nssh implements multiple security layers to protect credentials and minimize attack surface.

### Credential Protection

Credentials in `~/.local/share/nssh/credentials.age` are encrypted using [age](https://github.com/FiloSottile/age) (ChaCha20-Poly1305 + X25519). Passwords are never written to plaintext, never appear in shell history or process listings, and are streamed directly into the PTY. Interactive prompts only (no CLI args); double-prompted to prevent typos.

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

nssh enforces secure file permissions automatically. Sensitive files (credentials, age keys, backups) are `600`; configs/indexes are `644`. Verify: `ls -la ~/.local/share/nssh/credentials.age ~/.config/nssh/age.key`

### Key Management

Generate age key during initial setup: `mkdir -p ~/.config/nssh && age-keygen -o ~/.config/nssh/age.key`. Each user needs their own key - no shared credentials.

### Safe Debugging

`NSSH_DEBUG=1` logs timing only, never credentials. View host details: `nssh host get hostname`. List contexts: `nssh ctx list`.

### Key Rotation

If compromised: decrypt with old key, generate new key, re-encrypt, verify, securely delete temp files. See [examples/config/nssh_credentials.json](examples/config/nssh_credentials.json) for manual editing format.

## Troubleshooting

### Quick Reference

| Symptom | Quick Check | Solution Reference |
|---------|-------------|-------------------|
| "No hosts found" | `nssh host list` | [Host not found](#host-not-found-errors) |
| "Failed to decrypt credentials" | `ls ~/.config/nssh/age.key` | [No credential found](#no-credential-found-but-one-should-exist) |
| "Command not found: nssh" | `which nssh` | Install nssh |
| Connection hangs | `nssh hostname -vvv` | [Connection hangs](#connection-hangs-or-times-out) |
| Wrong auth method | `nssh host list --select hostname` | [Password auth not working](#password-authentication-not-working) |
| Stale SSH session | See error message | [Authorization denied](#authorization-denied-cannot-authorize-shell-for-unknown-sessionid) |

### "No credential found" but one should exist

Verify credential configuration:
```bash
nssh ctx list                 # Check context mappings
nssh host get hostname        # Check host-specific credentials
```

If credentials exist but aren't matching, check that your SSH config file is mapped to the correct context with `nssh ctx list`.

### "Host not found" errors

Check SSH config parsing:
```bash
nssh host list --select hostname    # See if host is recognized
cat ~/.local/state/nssh/host_index | grep hostname  # Check index directly
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
nssh host list --select hostname    # Check Auth column shows "passwd"
```

If it shows "key", remove and re-add the host with `--auth password` to fix the configuration.

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
