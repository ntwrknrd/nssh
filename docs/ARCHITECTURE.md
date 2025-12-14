# nssh Architecture & Technical Design

This document describes the internal implementation, algorithms, performance optimizations, and programmatic APIs for developers and integrators working with `nssh`. For end-user documentation, see [USER_GUIDE.md](USER_GUIDE.md).

## Overview

`nssh` is a pure Python CLI that launches OpenSSH through an in-process PTY connector:

- **Python CLI** handles argument parsing, host/credential resolution, and PTY management
- **PTY connector** spawns SSH directly, injects passwords, and handles interactive prompts
- **Core modules** manage encrypted credentials, SSH config parsing, and data operations

This document covers:

- Internal architecture and connection flow
- Credential resolution and host selection algorithms
- Host index implementation and performance optimizations
- Python APIs for programmatic integration
- Advanced debugging and profiling techniques

## Table of Contents

- [Connection Architecture](#connection-architecture) - Python CLI, core modules, PTY connector
- [SCP Connector Architecture](#scp-connector-architecture) - File transfer integration
- [Connection Flow](#connection-flow) - Host resolution, credential lookup, SSH execution
- [SSH Compatibility Detection](#ssh-compatibility-detection-and-remediation)
- [Credential Resolution Algorithm](#credential-resolution-algorithm) - Five-step flow, priority order
- [Recording System](#recording-system-architecture) - Workflow, locks, metadata, security
- [API Reference](#api-reference) - Python APIs, data formats, exit codes
- [Host Index](#host-index-implementation) - Format, generation, performance
- [Debugging](#debugging-and-profiling) - SSH debugging, timing, credential testing

## Connection Architecture

### Python CLI

The `nssh` command is a pure Python entry point (`src/nssh/cli/main.py`) that:

- Parses `[user@]<search-term>` arguments (fast path avoids importing Click when possible)
- Resolves hosts + credentials inside `nssh.core.connect`
- Handles fuzzy selection internally (`fzf` is called from Python when multiple hosts match)
- Integrates with shell history via the optional shell functions

For host selection and credential resolution details, see [Connection Flow](#connection-flow) and [Credential Resolution Algorithm](#credential-resolution-algorithm). For PTY connector implementation, see [PTY Connector Architecture](#pty-connector-architecture).

### Core Modules

**`src/nssh/core/connect.py`** (Primary - Optimized Flow)

- Unified host selection + credential resolution in single invocation
- Eliminates duplicate Python bootstrapping overhead
- Index-based fast path for exact matches
- Launches PTY connector for direct SSH execution

**`src/nssh/core/auth/credentials.py`**

- `CredentialManager` class - age-encrypted credential storage
- Manages contexts (environment-specific defaults) and host-specific credentials
- Methods: `decrypt_credentials()`, `encrypt_credentials()`, `resolve_credential()`

**`src/nssh/core/ssh/config.py`**

- `SSHConfigParser` class - SSH config file manipulation
- Handles Include directives, alphabetical sorting, backups, index rebuilding
- Methods: `parse_ssh_config()`, `write_ssh_config()`, `create_backup()`, `rebuild_index()`

**`src/nssh/core/ssh/fixer.py`**

- Authentication presets and legacy compatibility fixers
- Functions: `generate_ssh_config()`, `detect_auth_type()`, `parse_ssh_compatibility_error()`, `iterative_compatibility_fix()`

**`src/nssh/core/connector/pty.py`**

- PTY-based connector for direct OpenSSH execution
- Handles password injection, host key prompts, keyboard-interactive auth
- Integrates with recording system via asciinema wrapper
- Real-time prompt detection and credential auto-answer

**`src/nssh/core/env/paths.py`**

- Lazy helpers for resolving credential/age key/backup/recording paths
- Honors env variables + `config.toml` overrides at call time
- Centralized path resolution for all nssh components

**`src/nssh/core/env/settings.py`**

- Configuration file loading and environment variable resolution
- Handles `~/.config/nssh/config.toml` parsing
- Provides default values for all configurable paths and settings

**`src/nssh/core/env/system.py`**

- Thin subprocess + permission helpers (`run_command`, `check_command`, `set_secure_permissions`)
- System-level utilities for command execution and file operations

**`src/nssh/core/ui/fzf.py`**

- Minimal integration layer around the external `fzf` binary
- Used by CLI selectors and interactive host selection
- Keeps subprocess code isolated from core logic

**`src/nssh/core/ui/console.py`**

- Rich console output utilities
- Shared console instance for consistent output formatting

**`src/nssh/core/recording/manager.py`**

- Recording plan computation and session management
- Lock mechanism for concurrent session safety
- Metadata generation and session indexing

**`src/nssh/core/recording/proxy.py`**

- Recording proxy layer for PTY connector integration
- Handles asciinema subprocess wrapper when recording is enabled

## PTY Connector Architecture

The PTY connector (`src/nssh/core/connector/pty.py`) is the core innovation in nssh's architecture. It eliminates the need for external tools like `sshpass` by providing native password injection through PTY manipulation.

### Key Features

1. **Direct SSH Execution**: Uses `pty.fork()` + `os.execvpe()` to spawn OpenSSH directly
2. **In-Process Password Injection**: Monitors PTY output for password prompts via regex patterns
3. **Host Key Handling**: Detects and passes through host key confirmation prompts to user
4. **Keyboard-Interactive Auth**: Supports multi-stage authentication challenges
5. **Recording Integration**: Wraps SSH in asciinema subprocess when recording is enabled

### Password Injection Flow

```text
1. PTY connector spawns SSH via pty.fork()
2. Parent process enters select() loop monitoring PTY master FD
3. Buffer accumulates output; regex patterns detect "password:" or "passcode:" prompts
4. Credential written directly to PTY (never echoes to user terminal)
5. Control returns to user for remainder of interactive session
```

### Implementation Details

**Key Components:**
- `run_with_pty()` - Main entry point accepting hostname, credentials, SSH args
- `_relay_loop()` - I/O multiplexing between user terminal and SSH PTY
- `PASSWORD_PATTERNS` - Compiled regex for prompt detection (case-insensitive)
- `HOSTKEY_PROMPT_RE` - Host key verification pattern
- `_inject_password()` - Writes credential to PTY with terminal echo disabled

**Recording Integration:**
When recording is enabled, the connector spawns:
```
asciinema rec --command "ssh ..." --append → pty.fork() → ssh
```

**Exit Code Handling:**
- SSH exit code is propagated to shell ($?)
- Recording lock released automatically via context manager
- PTY cleanup happens in finally block

**Signal Handling:**
- SIGWINCH (terminal resize) forwarded to SSH subprocess
- SIGINT/SIGTERM propagated for clean shutdown
- Child process waited via os.waitpid()

## SCP Connector Architecture

The SCP connector (`src/nssh/core/connector/scp.py`) wraps OpenSSH's `scp` command with PTY-based password injection, maintaining standard SCP CLI syntax while integrating with nssh's credential vault.

### Implementation

**Entry point:** `run_scp(source, dest, password, scp_args)`

Spawns `scp` via PTY fork and injects password when prompted, identical to the interactive connector flow. The CLI layer (`src/nssh/cli/cp/`) handles hostname resolution and credential lookup; the connector layer only manages the PTY wrapper.

**Safety:** Requires exact hostname match (no fuzzy matching) to prevent accidental file transfers to wrong hosts.

**Credential flow:**
1. CLI resolves hostname via `find_host_match()`
2. CLI resolves credentials via `resolve_credential_for_host()`
3. Connector spawns `scp` and injects password via PTY

Uses same PTY password injection mechanism as interactive SSH connector.

## Connection Flow

### Optimized Connection Flow (Primary)

```text
1. User runs: nssh [user@]<search>
   └─> CLI fast-path extracts optional user@ prefix

2. `connect.py` checks `~/.local/state/nssh/host_index` for exact match
   ├─> Index hit: proceeds immediately
   └─> Index miss: parses SSH configs and Include directives

3. Host matching:
   ├─> Exact match: continues to credential resolution
   └─> Multiple matches: launches in-process `fzf` for interactive selection

4. Credential resolution:
   ├─> Loads `~/.local/share/nssh/credentials.age`
   ├─> Prefers host-specific credentials (respecting requested username)
   ├─> Falls back to the context credential referenced by the Include file
   └─> Returns `(username, password)` tuple or `None`

5. Connection execution:
   └─> PTY connector spawns `ssh -tt`, injects passwords when prompted, mirrors host-key prompts, and streams output directly to the user

6. Shell history integration (optional):
   └─> Shell helpers record both the initial search term and the resolved hostname
```

### Fallback Behavior

When the pre-compiled index (`~/.local/state/nssh/host_index`) is missing or doesn't contain a match:

1. `connect.py` falls back to full SSH config parsing
2. Reads `~/.ssh/config` and follows all `Include` directives
3. Extracts all `Host` entries with their source files
4. Performs fuzzy matching against the search term
5. Results are identical to index-based lookup, just slower 

The index is automatically rebuilt by `nssh host` commands (`add`, `rm`, `sort`), so this fallback is rare in normal usage.

## SSH Compatibility Detection and Remediation

nssh automatically detects and fixes SSH compatibility issues with legacy network equipment by monitoring SSH stderr for error patterns (kex, macs, ciphers, hostkey mismatches). When `nssh host add` tests a connection and detects an issue, it prompts to apply the appropriate fix and retests iteratively (up to 5 iterations).

Legacy algorithms are appended to modern defaults using the `+` prefix (e.g., `KexAlgorithms +diffie-hellman-group1-sha1`) to maintain security for modern hosts while enabling compatibility for legacy devices.

Implementation details in `src/nssh/core/ssh/fixer.py`. For user-facing documentation, see [USER_GUIDE.md - SSH Config Management](USER_GUIDE.md#nssh-host).

## Credential Resolution Algorithm

### Five-Step Resolution Flow

```text
1. Parse search term:
   ├─> Extract username from user@hostname syntax
   └─> Store hostname for matching

2. Check host-specific credentials:
   ├─> If username specified: search for matching username in host credentials
   ├─> If no username: use first credential in host credentials list
   └─> If match found: return credential, EXIT

3. Determine context from SSH config file:
   ├─> Read SSH config, follow Include directives
   ├─> Identify which Include file contains the host
   ├─> Look up context mapping (Include filename → context name)
   └─> If no context mapping: return None, EXIT (fallback to SSH keys)

4. Check context credential:
   ├─> If username specified: match against the single fallback credential
   ├─> If no username: use the fallback credential (if defined)
   └─> If match found: return credential, EXIT

5. No credential found:
   └─> Return None (SSH will use key-based auth or interactive prompt)
```

### Priority Order Summary

**With username specified (`user@hostname`):**

1. Host-specific credential matching username
2. Context credential matching username
3. Return None → SSH keys

**Without username (`hostname`):**

1. First host-specific credential
2. Context fallback credential (if present)
3. Return None → SSH keys

### Context Definition

Contexts map SSH config Include files to a single fallback credential:

```json
{
  "contexts": {
    "work": {
      "git_include_file": "work_hosts",
      "credential": {"username": "alice", "password": "encrypted"}
    }
  }
}
```

For examples, see [USER_GUIDE.md - Core Concepts](USER_GUIDE.md#core-concepts).

## Recording System Architecture

nssh integrates with asciinema v3 to provide automatic SSH session recording with host-based filtering, metadata indexing, and automatic cleanup. This section covers the implementation details and technical design.

### Recording Workflow

```text
1. User runs: nssh hostname
   └─> CLI resolves host and credentials

2. PTY connector initialization:
   ├─> Calls recording._compute_plan(hostname)
   ├─> Checks NSSH_RECORD environment variable (fast path if =0)
   ├─> Loads ~/.config/nssh/config.toml [recording] section
   ├─> Evaluates include/exclude patterns (glob or regex)
   └─> Returns RecordingPlan(enabled=True/False, cast_path, lock_directory, ...)

3. Recording decision:
   ├─> If enabled=false: spawn SSH directly via PTY
   ├─> If enabled=true: acquire lock, spawn asciinema wrapper
   └─> If NSSH_RECORD=force: fail if asciinema missing

4. SSH execution:
   ├─> Without recording: pty.fork() -> execvpe("ssh", ...)
   └─> With recording: pty.fork() -> execvpe("asciinema", ["rec", "--command", "ssh", ...])

5. Session I/O:
   └─> PTY connector relays stdin/stdout (recording happens in asciinema subprocess)

6. Post-connection:
   └─> Lock released automatically (context manager)
```

### Lock Mechanism Implementation

To prevent corrupted recordings from concurrent sessions, nssh uses per-session lock directories at `~/.local/state/nssh/casts/<hostname>/<date>/.session-NNN.lock/` containing `.lockinfo` files with process PIDs. Lock validity is checked via `os.kill(pid, 0)`. When append mode is enabled, nssh searches for unlocked sessions (most recent first) or allocates a new sequence number. Stale locks from crashed processes are automatically detected and ignored. Implementation in `src/nssh/cli/log.py`.

### Session Metadata Format

Each recording directory includes `metadata.json` with hostname, username, ISO 8601 timestamp, session number, cast file path, terminal dimensions, and recording configuration.

### Configuration Loading and Pattern Matching

Recording configuration is loaded from `~/.config/nssh/config.toml` with pattern matching for include/exclude hosts (glob or regex with `regex:` prefix). Pattern precedence: NSSH_RECORD environment variable (highest), include_hosts (allowlist), exclude_hosts (denylist), default (record all). Include and exclude patterns are mutually exclusive.

### Cleanup Policy Implementation

Old recordings can be cleaned up using the `nssh log delete` command, which can remove recording sessions older than a specified number of days (using `--older-than N`). The command supports dry-run mode (`--dry-run`) to preview deletions before execution. See [USER_GUIDE.md - nssh log (Session Recording)](USER_GUIDE.md#nssh-log-session-recording) for usage details.

### Security Considerations

**What gets recorded:**

- All terminal I/O (input and output) during SSH session
- Terminal dimensions and timing data
- Session metadata (hostname, username, timestamp)

**Security implications:**

1. **Passwords in recordings:** Interactive password prompts are recorded in plaintext
2. **Sensitive output:** API keys, secrets, PII displayed in terminal are recorded
3. **Compliance requirements:** Recordings may violate PCI-DSS, HIPAA, SOC2 policies

**Mitigation strategies:**

```toml
[recording]
# Strategy 1: Exclude production systems entirely
exclude_hosts = ["prod-*", "*.production.com"]

# Strategy 2: Only record specific lab/dev environments
include_hosts = ["lab-*", "dev-*", "test-*"]
```

**File permissions:**

Recording directory should have restrictive permissions:

```bash
chmod 700 ~/.local/state/nssh/casts  # Owner only
```

**Storage encryption:**

For environments requiring encryption at rest:

```bash
# Option 1: Encrypted filesystem (LUKS, FileVault, BitLocker)
# Option 2: Encrypted home directory
# Option 3: Manual encryption of recording directory
```

### Integration with nssh log CLI

The `nssh log` CLI provides a management interface to the recording system:

**Key operations:**

| Command | Implementation |
|---------|---------------|
| `nssh log list` | Scans recording directory, parses metadata, filters by `--select` regex |
| `nssh log play` | Interactive fzf picker, invokes `asciinema play` |
| `nssh log upload` | Interactive fzf picker, invokes `asciinema upload` with `ASCIINEMA_SERVER_URL` |
| `nssh log export` | Interactive fzf picker, invokes `asciinema convert`, saves to current directory |
| `nssh log delete` | Interactive fzf picker or `--older-than N` days, `--select` regex filtering |

**File selection:**

All log commands (except `list`) use an interactive fzf picker:
- Scans `recording_dir/*/session-*.cast` for all recordings
- Sorts results by modification time (newest first)
- Launches fzf with formatted entries: `hostname | date | session | full_path`
- User selects via fuzzy search and arrow keys

Implementation in `src/nssh/cli/log.py::pick_recording_interactive()`.

## API Reference

This section documents the programmatic APIs and data formats for developers integrating `nssh` into their tools or scripts. The CLI commands (`nssh`, `nssh ctx`, `nssh host`) are built on these Python APIs. For end-user CLI usage, see the [User Guide](USER_GUIDE.md).

### Python API

For programmatic access to credential and SSH config management:

#### CredentialManager Class

Located in `nssh.core.auth.credentials`:

```python
from nssh.core.auth.credentials import CredentialManager

cm = CredentialManager()
```

**Key Methods:**

| Method | Parameters | Returns | Description |
|--------|------------|---------|-------------|
| `decrypt_credentials()` | - | `Dict` | Load and decrypt credential file |
| `encrypt_credentials(data)` | `data: Dict` | `None` | Encrypt and save credentials |
| `create_context(name, file)` | `name: str`, `file: str` | `None` | Create new context |
| `add_context_credential(context, user, pwd)` | `context: str`, `user: str`, `pwd: str` | `None` | Add credential to context |
| `add_host_credential(host, user, pwd)` | `host: str`, `user: str`, `pwd: str` | `None` | Add host-specific credential |
| `list_contexts()` | - | `List[Dict]` | List all contexts |
| `resolve_credential(hostname, git_include_file, username)` | `hostname: str`, `git_include_file: Optional[str]`, `username: Optional[str]` | `Tuple[str, str]` or `None` | Resolve credential for host |
| `get_context_by_file(filename)` | `filename: str` | `Optional[str]` | Get context name from SSH config filename |

#### SSHConfigParser Class

Located in `nssh.core.ssh.config`:

```python
from nssh.core.ssh.config import SSHConfigParser

parser = SSHConfigParser()
```

**Key Methods:**

| Method | Parameters | Returns | Description |
|--------|------------|---------|-------------|
| `find_include_files()` | - | `List[Path]` | Get all Include files from main config |
| `parse_ssh_config(file_path)` | `file_path: Path` | `Tuple[List[str], List[Tuple]]` | Parse config into header and hosts |
| `write_ssh_config(file_path, header, hosts)` | `file_path: Path`, `header: List[str]`, `hosts: List[Tuple]` | `None` | Write config file |
| `create_backup(file_path)` | `file_path: Path` | `None` | Create timestamped backup |
| `rebuild_index()` | - | `None` | Rebuild `~/.local/state/nssh/host_index` |
| `host_exists(file_path, hostname, hosts)` | `file_path: Path`, `hostname: str`, `hosts: Optional[List]` | `bool` | Check if host exists in file |
| `find_host_in_files(hostname, files)` | `hostname: str`, `files: List[Path]` | `Tuple[Path, List[str]]` or `None` | Locate host across multiple config files |

### Data Formats

#### Credential File Format

File: `~/.local/share/nssh/credentials.age` (age-encrypted JSON containing contexts and host-specific credentials). Passwords are plaintext in decrypted JSON (encrypted in `.age` file). Edit via `nssh ctx` and `nssh host edit` commands, not manually.

See [examples/config/nssh_credentials.json](examples/config/nssh_credentials.json) for complete format and examples.

#### Host Index Format

File: `~/.local/state/nssh/host_index` (plaintext, auto-generated). Format: `hostname|filepath` pairs (one per line). Automatically rebuilt by `nssh host` commands. Used for fast exact-match lookups (fast vs slower full parse).

See [examples/state/.nssh_host_index](examples/state/.nssh_host_index) for format example.

#### Timing Log Format

Output format when `NSSH_DEBUG=1` or via `nssh benchmark`: `[timestamp] TIMING: stage-name - XXXms`

See [benchmark-run.txt](examples/output/benchmark-run.txt) for example benchmark output.

### Exit Codes

| Code | Meaning | Context |
|------|---------|---------|
| `0` | Success | Command completed successfully |
| `1` | Error | User error, validation failure, or runtime error |

**Examples:**

- Credential not found → Exit 0 (fallback to SSH keys is not an error)
- Invalid hostname → Exit 1
- Missing required option → Exit 1

## Host Index Implementation

### Index File Format

Location: `~/.local/state/nssh/host_index`

Format: Plain text, one line per host

```text
hostname|filepath
core-switch-01|/Users/user/.ssh/work_hosts
firewall|/Users/user/.ssh/homelab_hosts
k3s-master01|/Users/user/.ssh/cloud_hosts
```

### Automatic Rebuilding

The index is automatically rebuilt after these operations:

- `nssh host add` - New host entry added
- `nssh host rm` - Host entry removed
- `nssh host sort` - Config file alphabetically sorted

**Implementation:** Each CLI command calls `SSHConfigParser.rebuild_index()` after successful modification.

### Index Generation Process

```python
# Simplified algorithm from ssh_config.py
def rebuild_index():
    hosts = []

    # Parse main config and all Include files
    for file in [config] + include_files:
        for host_entry in parse_file(file):
            hosts.append(f"{host_entry.alias}|{file.absolute_path}")

    # Write to index file
    write_file("~/.local/state/nssh/host_index", "\n".join(hosts))
```

### Lookup Performance

**Index-based lookup (exact match):**

```bash
grep "^hostname|" ~/.local/state/nssh/host_index
# Result: fast (single grep operation)
```

**Full config parsing (index miss or partial match):**

```python
# Parse ~/.ssh/config
# Follow all Include directives
# Extract all Host entries
# Result: slower (Python initialization + file I/O + parsing)
```

**Why the index is fast:**

- Simple text file, no Python parsing needed for exact matches
- Single grep operation on small file (<100KB for 1000+ hosts)
- No Include directive resolution required
- No SSH config syntax parsing needed

## Debugging and Profiling

### SSH Debugging

```bash
# Verbose SSH output (levels: -v, -vv, -vvv)
nssh hostname -vvv
```

### Timing Instrumentation

Enable timing output and identify bottlenecks:

```bash
NSSH_DEBUG=1 nssh hostname
```

**Timing Stages (PTY Connector Architecture):**

| Stage | Description | When |
|-------|-------------|------|
| `pty-start` | PTY process initialization | Always |
| `config-parse` | SSH config file parsing | On index miss |
| `host-selection` | Index lookup + SSH config resolution | Always |
| `credential-vault` | Age decryption + credential resolution | Always |
| `connection-orchestration` | PTY setup + recording plan computation | Always |
| `recording-setup` | Lock acquisition + metadata generation | If recording enabled |
| `recording-session` | SSH + asciinema wrapper (contains ssh-connection) | If recording enabled |
| `  ssh-connection` | Actual SSH execution time (nested stage) | Always |
| `pty-teardown` | PTY cleanup and exit | Always |

**Nested Stages:**

When session recording is enabled (`NSSH_RECORD=1`), `ssh-connection` is nested *inside* `recording-session`. The relationship is:

```
recording-session = ssh-connection + asciinema-overhead (~75ms)
```

The benchmark renderer calculates and displays `asciinema-overhead` as a derived metric by subtracting `ssh-connection` from `recording-session`.

**Key Differences from Legacy Wrapper Architecture:**
- No `wrapper-start` or `wrapper-teardown` stages
- No `python-bootstrap` stage (CLI is already in-process)
- `ssh-connection` exists in both recording and non-recording modes (nested when recording enabled)

**Example Output:**

See [benchmark-run.txt](examples/output/benchmark-run.txt) for example benchmark output. Note: the example shows recording disabled mode (`NSSH_RECORD=0`), so only `config-parse` and `host-selection` stages appear. The `config-parse` stage only appears when the host index cache misses.

**Recording Mode Behavior:**

- When recording is enabled (`NSSH_RECORD=1`): Both `recording-session` and nested `ssh-connection` stages appear
- When recording is disabled (`NSSH_RECORD=0`): Only `ssh-connection` stage appears (as shown in the example)

**Benchmark Options:**

- Use `nssh benchmark ssh HOST` for detailed stage-by-stage timing breakdown
- Use `nssh benchmark ssh --simple-only HOST` for end-to-end "hit Enter" latency only
- Use `nssh benchmark ssh --no-record HOST` to measure SSH overhead without recording influence
- Use `nssh benchmark scp HOST` to benchmark SCP file transfer performance (accepts `--size KB` option)


### Credential Decryption Testing

Test age decryption:

```bash
# Manually decrypt credentials

age -d -i ~/.config/nssh/age.key ~/.local/share/nssh/credentials.age | python3 -m json.tool

# If decryption fails:
# - Check file permissions (should be 600)
# - Verify age key hasn't been rotated
# - Ensure credentials.age was encrypted with your current public key
```
