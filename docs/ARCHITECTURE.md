# nssh Architecture & Technical Design

This document describes the internal implementation, algorithms, performance optimizations, and programmatic APIs for developers and integrators working with `nssh`. For end-user documentation, see [USER_GUIDE.md](USER_GUIDE.md).

## Overview

`nssh` is built on a two-layer architecture:

- **Bash layer** handles user interaction, orchestration, and SSH command execution
- **Python layer** manages encrypted credentials, SSH config parsing, and data operations

This document covers:

- Internal architecture and connection flow
- Credential resolution and host selection algorithms
- Host index implementation and performance optimizations
- Python APIs for programmatic integration
- Advanced debugging and profiling techniques

## Table of Contents

- [Overview](#overview)
- [Two-Layer Architecture](#two-layer-architecture)
  - [Bash Layer (Orchestration)](#bash-layer)
  - [Python Layer (Data Management)](#python-layer)
  - [Core Modules](#core-modules)
- [Connection Flow](#connection-flow)
  - [Optimized Connection Flow (Primary)](#optimized-connection-flow)
  - [Fallback Behavior](#fallback-behavior)
- [SSH Compatibility Detection and Remediation](#ssh-compatibility-detection-and-remediation)
- [Credential Resolution Algorithm](#credential-resolution-algorithm)
  - [Five-Step Resolution Flow](#five-step-resolution-flow)
  - [Priority Order Summary](#priority-order-summary)
  - [Context Definition](#context-definition)
- [Recording System Architecture](#recording-system-architecture)
  - [Recording Workflow](#recording-workflow)
  - [Lock Mechanism Implementation](#lock-mechanism-implementation)
  - [Session Metadata Format](#session-metadata-format)
  - [Configuration Loading and Pattern Matching](#configuration-loading-and-pattern-matching)
  - [Cleanup Policy Implementation](#cleanup-policy-implementation)
  - [Security Considerations](#security-considerations)
  - [Integration with nssh log CLI](#integration-with-nssh-log-cli)
- [API Reference](#api-reference)
  - [Python API](#python-api)
    - [CredentialManager Class](#credentialmanager-class)
    - [SSHConfigParser Class](#sshconfigparser-class)
  - [Data Formats](#data-formats)
    - [Credential File Format](#credential-file-format)
    - [Host Index Format](#host-index-format)
    - [Timing Log Format](#timing-log-format)
  - [Exit Codes](#exit-codes)
- [Host Index Implementation](#host-index-implementation)
  - [Index File Format](#index-file-format)
  - [Index Generation Process](#index-generation-process)
  - [Automatic Rebuilding](#automatic-rebuilding)
  - [Lookup Performance](#lookup-performance)
- [Performance Metrics](#performance-metrics)
  - [Current Measurements](#current-measurements)
  - [Optimization History](#optimization-history)
  - [Key Optimizations](#key-optimizations)
- [Debugging and Profiling](#debugging-and-profiling)
  - [Testing Python Modules](#testing-python-modules)
  - [SSH Debugging](#ssh-debugging)
  - [Timing Instrumentation](#timing-instrumentation)
  - [Performance Profiling](#performance-profiling)
  - [Credential Decryption Testing](#credential-decryption-testing)

## Two-Layer Architecture

### Bash Layer (Orchestration)

The bash wrapper (`src/nssh/assets/scripts/nssh-wrapper.sh`) serves as the main entry point and orchestration layer:

- Parses user input (`[user@]<search-term>`)
- Invokes the unified Python module for host selection and credential resolution
- Executes SSH connections using `sshpass` for password authentication or native SSH for key-based auth
- Integrates with shell history to record actual hostnames (not search terms)
- Handles fuzzy selection via `fzf` when multiple hosts match

### Python Layer (Data Management)

The Python package (`nssh`) handles all data operations:

- **Credential management:** Age-encrypted storage, context-based resolution
- **SSH config parsing:** Include directive resolution, host extraction, alphabetical sorting
- **Host indexing:** Pre-compiled index for fast lookups
- **Unified connection logic:** Combined host selection and credential resolution

### Core Modules

**`src/nssh/core/connect.py`** (Primary - Optimized Flow)

- Unified host selection + credential resolution in single invocation
- Eliminates duplicate Python bootstrapping overhead
- Index-based fast path for exact matches
- Invoked via: `uv run python -m nssh.core.connect <search> [username]`
- Outputs: `hostname|filepath|username|@fd:<number>`

**`src/nssh/core/credentials.py`**

- `CredentialManager` class - age-encrypted credential storage
- Manages contexts (environment-specific defaults) and host-specific credentials
- Methods: `decrypt_credentials()`, `encrypt_credentials()`, `resolve_credential()`

**`src/nssh/core/ssh_config.py`**

- `SSHConfigParser` class - SSH config file manipulation
- Handles Include directives, alphabetical sorting, backups, index rebuilding
- Methods: `parse_ssh_config()`, `write_ssh_config()`, `create_backup()`, `rebuild_index()`

**`src/nssh/core/paths.py`**

- Lazy helpers for resolving credential/age key/backup paths
- Honors env variables + `config.toml` overrides at call time

**`src/nssh/core/system.py`**

- Thin subprocess + permission helpers (`run_command`, `check_command`, `set_secure_permissions`)

**`src/nssh/core/fzf.py`**

- Minimal integration layer around the external `fzf` binary
- Used by CLI selectors; keeps subprocess code isolated from core logic

**`src/nssh/core/ssh_compat.py`**

- Authentication presets and legacy compatibility fixers
- Functions: `generate_ssh_config()`, `detect_auth_type()`, `parse_ssh_compatibility_error()`, `iterative_compatibility_fix()`

## Connection Flow

### Optimized Connection Flow (Primary)

```text
1. User runs: nssh [user@]<search>
   └─> nssh-wrapper.sh extracts optional user@ prefix

2. Wrapper calls: uv run python -m nssh.core.connect <search> [username]
   └─> Single Python invocation (eliminates duplicate bootstrap)

3. connect.py checks ~/.ssh/.nssh_host_index for exact match
   ├─> Index hit: proceeds immediately with hostname|filepath (~1ms)
   └─> Index miss: parses SSH configs, follows Include directives (~200ms)

4. Host matching:
   ├─> Exact match: proceeds to credential resolution
   └─> Multiple matches: outputs list, exits for fzf selection

5. If fzf needed:
   ├─> Bash wrapper presents menu
   ├─> User selects hostname
   └─> Re-invokes connect.py with selected hostname

6. Credential resolution:
   ├─> Loads ~/.ssh/nssh_credentials.age (age decrypt)
   ├─> Searches host-specific credentials (matching username if specified)
   ├─> Falls back to context fallback credential from the matching Include file
   └─> Returns: hostname|filepath|username|@fd:<number>

7. connect.py outputs result and exits

8. nssh-wrapper.sh receives output:
   ├─> If credential found: sshpass -d "$pipe_fd" ssh -o User="$username" "$hostname" (password streams through in-memory pipe)
   └─> If no credential: ssh "$hostname" (key-based auth)

9. SSH connection established

10. Shell history integration (optional):
    └─> Records actual hostname in shell history (not search term)
```

**Performance:** Single Python invocation (minimal overhead beyond SSH connection time)

### Fallback Behavior

When the pre-compiled index (`~/.ssh/.nssh_host_index`) is missing or doesn't contain a match:

1. `connect.py` falls back to full SSH config parsing
2. Reads `~/.ssh/config` and follows all `Include` directives
3. Extracts all `Host` entries with their source files
4. Performs fuzzy matching against the search term
5. Results are identical to index-based lookup, just slower (~200ms vs ~1ms)

The index is automatically rebuilt by `nssh host` commands (`add`, `remove`, `sort`, `update`), so this fallback is rare in normal usage.

## SSH Compatibility Detection and Remediation

nssh automatically detects and fixes SSH compatibility issues with legacy network equipment by monitoring SSH stderr for error patterns (kex, macs, ciphers, hostkey mismatches). When `nssh host add` tests a connection and detects an issue, it prompts to apply the appropriate fix and retests iteratively (default: 3 iterations, configurable with `--max-iterations`).

Legacy algorithms are appended to modern defaults using the `+` prefix (e.g., `KexAlgorithms +diffie-hellman-group1-sha1`) to maintain security for modern hosts while enabling compatibility for legacy devices. Users can manually apply fixes with `nssh host update hostname --compat <type>`.

Implementation details in `src/nssh/core/ssh_compat.py`. For user-facing documentation, see [USER_GUIDE.md - SSH Config Management](USER_GUIDE.md#nssh-host).

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
   └─> nssh-wrapper.sh checks recording configuration

2. Configuration loading (Python):
   ├─> Check ~/.config/nssh/config.toml for [recording] section
   ├─> Parse include_hosts/exclude_hosts patterns (glob or regex)
   ├─> Check environment overrides (NSSH_RECORD, NSSH_RECORD_DIR)
   └─> Determine if recording should happen for this host

3. Recording decision:
   ├─> If enabled=false or host excluded: proceed without recording
   ├─> If enabled=true and host included: prepare recording session
   └─> If NSSH_RECORD=force: fail if asciinema not available

4. Recording initialization (if enabled):
   ├─> Acquire recording lock (prevent concurrent recording conflicts)
   ├─> Create session directory: <dir>/<hostname>/<YYYY-MM-DD>/
   ├─> Determine session file:
   │   ├─> append_mode=true: session-000.cast (daily file)
   │   └─> append_mode=false: session-NNN.cast (new file per session)
   ├─> Write metadata.json with session info
   └─> Prepare asciinema command with --append flag

5. SSH connection via asciinema:
   └─> Execute: asciinema rec [--append] <session-file> --command "ssh ..."

6. Post-connection cleanup:
   └─> Release recording lock and exit
```

### Lock Mechanism Implementation

To prevent corrupted recordings from concurrent sessions, nssh uses per-session lock directories at `~/.local/state/nssh/casts/<hostname>/<date>/.session-NNN.lock/` containing `.lockinfo` files with process PIDs. Lock validity is checked via `os.kill(pid, 0)`. When append mode is enabled, nssh searches for unlocked sessions (most recent first) or allocates a new sequence number. Stale locks from crashed processes are automatically detected and ignored. Implementation in `src/nssh/cli/log.py`.

### Session Metadata Format

Each recording directory includes `metadata.json` with hostname, username, ISO 8601 timestamp, session number, cast file path, terminal dimensions, and recording configuration.

### Configuration Loading and Pattern Matching

Recording configuration is loaded from `~/.config/nssh/config.toml` with pattern matching for include/exclude hosts (glob or regex with `regex:` prefix). Pattern precedence: NSSH_RECORD environment variable (highest), include_hosts (allowlist), exclude_hosts (denylist), default (record all). Include and exclude patterns are mutually exclusive.

### Cleanup Policy Implementation

The `cleanup_old_recordings()` function exists but is not currently called automatically. Users must manually manage old recordings with `rm -rf` or `find ... -mtime +N -delete` commands.

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
| `nssh log list` | Scans recording directory, parses metadata, filters by keyword search |
| `nssh log play` | Locates `.cast` file, invokes `asciinema play` |
| `nssh log upload` | Locates `.cast` file, invokes `asciinema upload --server-url` with ASCIINEMA_SERVER_URL or --server option |
| `nssh log export` | Locates `.cast` file, invokes `asciinema convert` or `asciicast2gif`, saves to current directory |

**Internal commands:**

| Command | Implementation |
|---------|---------------|
| `nssh recording-check` | Internal-only command called by nssh wrapper. Loads config, evaluates patterns, returns recording plan. Optimized for minimal startup time (no CLI framework imports). |

**Export behavior notes:**

- By default, exports to the current working directory (where user is working)
- Uses descriptive filename format: `{hostname}_{date}_{session}.{format}` (e.g., `acm-lab-agg-sw1_2025-11-15_session-000.txt`)
- Avoids cluttering recording storage directory with export artifacts
- Supports both text (default, via `asciinema convert`) and GIF (via `asciicast2gif`) formats

**File selection methods:**

1. **Direct file path** (via `--file`):
   - User provides full or relative path to `.cast` file
   - File existence is validated before proceeding

2. **Interactive picker** (default when `--file` not provided):
   - Scans `recording_dir/*/{date}/session-*.cast` for all recordings on the specified date
   - Sorts results by modification time (newest first)
   - Launches fzf with formatted entries: `hostname | date | session | full_path`
   - User selects via fuzzy search and arrow keys
   - Selected file path is extracted and used

Implementation in `src/nssh/cli/log.py::pick_recording_interactive()`.

## API Reference

This section documents the programmatic APIs and data formats for developers integrating `nssh` into their tools or scripts. The CLI commands (`nssh`, `nssh cred`, `nssh host`) are built on these Python APIs. For end-user CLI usage, see the [User Guide](USER_GUIDE.md).

### Python API

For programmatic access to credential and SSH config management:

#### CredentialManager Class

Located in `nssh.core.credentials`:

```python
from nssh.core.credentials import CredentialManager

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

Located in `nssh.core.ssh_config`:

```python
from nssh.core.ssh_config import SSHConfigParser

parser = SSHConfigParser()
```

**Key Methods:**

| Method | Parameters | Returns | Description |
|--------|------------|---------|-------------|
| `find_include_files()` | - | `List[Path]` | Get all Include files from main config |
| `parse_ssh_config(file_path)` | `file_path: Path` | `Tuple[List[str], List[Tuple]]` | Parse config into header and hosts |
| `write_ssh_config(file_path, header, hosts)` | `file_path: Path`, `header: List[str]`, `hosts: List[Tuple]` | `None` | Write config file |
| `create_backup(file_path)` | `file_path: Path` | `None` | Create timestamped backup |
| `rebuild_index()` | - | `None` | Rebuild `~/.ssh/.nssh_host_index` |
| `host_exists(file_path, hostname, hosts)` | `file_path: Path`, `hostname: str`, `hosts: Optional[List]` | `bool` | Check if host exists in file |
| `find_host_in_files(hostname, files)` | `hostname: str`, `files: List[Path]` | `Tuple[Path, List[str]]` or `None` | Locate host across multiple config files |

### Data Formats

#### Credential File Format

File: `~/.ssh/nssh_credentials.age` (age-encrypted JSON containing contexts and host-specific credentials). Passwords are plaintext in decrypted JSON (encrypted in `.age` file). Edit via `nssh cred` commands, not manually.

See [docs/examples/nssh_credentials.json](examples/nssh_credentials.json) for complete format and examples.

#### Host Index Format

File: `~/.ssh/.nssh_host_index` (plaintext, auto-generated). Format: `hostname|filepath` pairs (one per line). Automatically rebuilt by `nssh host` commands. Used for fast exact-match lookups (~1ms vs ~200ms full parse).

See [docs/examples/.nssh_host_index](examples/.nssh_host_index) for format example.

#### Timing Log Format

Output format when `NSSH_DEBUG=1` or via `nssh benchmark capture`: `[timestamp] TIMING: stage-name - XXXms`

See [benchmark-capture-local.txt](examples/benchmark-capture-local.txt) and [benchmark-capture-remote.txt](examples/benchmark-capture-remote.txt) for complete output examples. For stage definitions, see [Performance Metrics](#performance-metrics).

### Exit Codes

| Code | Meaning | Context |
|------|---------|---------|
| `0` | Success | Command completed successfully |
| `1` | Error | User error, validation failure, or runtime error |
| `2` | Budget violation | Performance budget exceeded (nssh benchmark only) |

**Examples:**

- Credential not found → Exit 0 (fallback to SSH keys is not an error)
- Invalid hostname → Exit 1
- Missing required option → Exit 1
- `nssh benchmark` total time exceeds `--total-budget` → Exit 2
- `nssh benchmark` stage exceeds `--stage-budget` → Exit 2

## Host Index Implementation

### Index File Format

Location: `~/.ssh/.nssh_host_index`

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
- `nssh host update` - Host authentication type or compatibility options changed

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
    write_file("~/.ssh/.nssh_host_index", "\n".join(hosts))
```

### Lookup Performance

**Index-based lookup (exact match):**

```bash
grep "^hostname|" ~/.ssh/.nssh_host_index
# Result: ~1ms (single grep operation)
```

**Full config parsing (index miss or partial match):**

```python
# Parse ~/.ssh/config
# Follow all Include directives
# Extract all Host entries
# Result: ~200ms (Python initialization + file I/O + parsing)
```

**Why the index is fast:**

- Simple text file, no Python parsing needed for exact matches
- Single grep operation on small file (<100KB for 1000+ hosts)
- No Include directive resolution required
- No SSH config syntax parsing needed

## Performance Metrics

### Current Measurements

Measured with `nssh benchmark capture` on Apple M3 Pro silicon (recording disabled via `NSSH_RECORD=0`):

| Stage | LAN (rpi-a) | VPN (test-host) | Notes |
|-------|-------------|-----------------|-------|
| **wrapper-start** | 177.7ms | 148.8ms | Python/uv bootstrap (constant) |
| **host-selection** | 0.2ms | 0.1ms | Index-based lookup (constant) |
| **credential-vault** | 5.9ms | 5.8ms | Age decryption (constant) |
| **connection-orch.** | negligible | negligible | Command preparation |
| **recording-setup** | 0.2ms | N/A | Recording plan check (skipped when `NSSH_RECORD=0`) |
| **ssh-connection** | **40.5ms** | **324.0ms** | **Network-dependent (8x difference!)** |
| **wrapper-teardown** | 0.2ms | 1.0ms | Cleanup |
| **TOTAL** | **219.8ms** | **477.4ms** | |

**With recording enabled** (`NSSH_RECORD=1`):

| Stage | LAN (rpi-a) | Notes |
|-------|-------------|-------|
| **recording-setup** | 99.2ms | Config load + plan computation (Python imports optimized) |
| **ssh-connection** | 65.1ms | Slightly higher due to asciinema wrapper overhead |
| **TOTAL** | 357.9ms | Recording adds ~138ms overhead |

See [benchmark-capture-local.txt](examples/benchmark-capture-local.txt) and [benchmark-capture-remote.txt](examples/benchmark-capture-remote.txt) for full benchmark output.

**Key insights:**

1. **Python/uv bootstrap dominates nssh overhead** (~178ms constant)
2. **Credential operations are fast** (~6ms constant)
3. **Network type dominates total time:** LAN is 8x faster than VPN for SSH connection (40ms vs 324ms)
4. **Recording overhead is isolated:** The `recording-setup` stage shows ~99ms when enabled, <1ms when disabled
5. **Total time:** LAN ~220ms (recording disabled), ~358ms (recording enabled); VPN ~477ms

### Optimization History

The unified architecture eliminated duplicate Python invocations:

- **Before:** Separate host selection + credential resolution (2× Python bootstrap)
- **After:** Single `connect.py` invocation (1× Python bootstrap)
- **Result:** Removed ~140ms duplicate bootstrap overhead

### Key Optimizations

1. **Pre-compiled Host Index** (`~/.ssh/.nssh_host_index`)
   - Format: `hostname|filepath` (one per line)
   - Exact match lookups: ~1ms (grep) vs ~200ms (full Python parse)
   - Automatically rebuilt by `nssh host add/remove/sort/update`
   - Falls back to full config parsing if index miss

2. **Unified Architecture**
   - Single Python invocation instead of separate select + connect
   - Eliminates duplicate uv bootstrap overhead (~140ms saved)
   - Combines host selection + credential resolution in `connect.py`
   - Returns `hostname|filepath|username|@fd:<number>` in single output

3. **Recording Setup Optimization**
   - Dedicated `nssh recording-check` command (no CLI framework imports)
   - Retired the heavier `nssh log check` Typer command (removing ~165ms overhead)
   - Fast-path for `NSSH_RECORD=0` skips Python entirely in shell wrapper
   - Result: 244ms → 99ms when recording enabled, <1ms when disabled

4. **Timing Instrumentation**
   - Enable with: `NSSH_DEBUG=1`
   - Outputs: `[timestamp] TIMING: operation - XXXms`
   - Tracks: Python bootstrap, index lookup, credential resolution, recording setup, SSH connection
   - Isolated `recording-setup` stage shows recording overhead separately from SSH connection

## Debugging and Profiling

For common user issues, see [USER_GUIDE.md - Troubleshooting](USER_GUIDE.md#troubleshooting). For contributor debugging, see [CONTRIBUTING.md](../CONTRIBUTING.md).

### Testing Python Modules

Test modules without installing:

```bash
# Test credential resolution
uv run python -m nssh.core.connect hostname [username]

# Test credential manager
uv run python -c "from nssh.core.credentials import CredentialManager; print(CredentialManager().decrypt_credentials())"

# Rebuild host index manually
uv run python -c "from nssh.core.ssh_config import SSHConfigParser; SSHConfigParser().rebuild_index()"
```

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

See [benchmark-capture-local.txt](examples/benchmark-capture-local.txt) and [benchmark-capture-remote.txt](examples/benchmark-capture-remote.txt) for example output. Performance expectations: wrapper-start ~178ms, host-selection ~0.2ms, credential-vault ~6ms, recording-setup ~0.2ms (disabled) or ~99ms (enabled), ssh-connection varies by network (LAN: 40-65ms, VPN: 200-400ms).

### Performance Profiling

Use `nssh benchmark capture` for systematic performance analysis. Expected timings (M3 Pro): LAN ~220ms total (recording disabled), ~358ms total (recording enabled), VPN ~477ms total. If significantly higher, check disk I/O, credential file size, network filesystem usage, or large SSH configs (>10,000 hosts).

### Credential Decryption Testing

Test age decryption:

```bash
# Manually decrypt credentials

age -d -i ~/.config/age/keys.txt ~/.ssh/nssh_credentials.age | jq .

# If decryption fails:
# - Check file permissions (should be 600)
# - Verify age key hasn't been rotated
# - Ensure credentials.age was encrypted with your current public key
```

---

For end-user documentation and operational workflows, see [USER_GUIDE.md](USER_GUIDE.md). For development setup and contribution guidelines, see [CONTRIBUTING.md](../CONTRIBUTING.md).
