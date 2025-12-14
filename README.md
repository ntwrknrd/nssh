# nssh

SSH wrapper for power users: manage hosts and credentials, inject passwords automatically, and record sessions.

## Table of Contents

- [Demo](#demo)
- [Features](#features)
- [Installation](#installation)
- [Learn More](#learn-more)
- [Acknowledgements](#acknowledgements)
- [Roadmap](#roadmap)

## Demo

![Demo](docs/examples/demo.gif)

## Features

- **Interactive PTY connector** - In-process password injection without external tools (see [ARCHITECTURE.md](docs/ARCHITECTURE.md#pty-connector-architecture))
- **Fuzzy host selection** - Exact matches connect instantly; partial matches use `fzf` for interactive filtering
- **Agent-based credential management** - Background daemon holds decrypted credentials with configurable idle/lifetime timeouts; supports passphrase-protected keys and YubiKey PIV hardware tokens
- **Age-encrypted vault** - Context-aware storage with domain-based resolution and host-specific overrides; passwords never in plaintext or CLI args (streamed directly through the PTY connector)
- **SSH config management** - Create, remove, sort, and update host entries in SSH config files with automatic alphabetical sorting, timestamped backups, and indexed lookups across SSH 'Include' config files
- **Legacy device compatibility** - Auto-detects SSH algorithm mismatches and applies KEX/cipher/MAC fixes for older network equipment (see [ARCHITECTURE.md](docs/ARCHITECTURE.md#ssh-compatibility-detection-and-remediation))
- **Shell integration** - History tracking (Bash/Zsh/Fish) and tab completion for hostnames, contexts, and commands
- **Session recording & playback** - Automatic asciinema integration with host-based filtering, idle time limiting, automatic archival, and comprehensive session management via `nssh log` CLI (list/play/upload/export/delete with pattern matching and interactive selection)
- **File transfers** - Standard SCP CLI with credential vault integration (see [USER_GUIDE.md](docs/USER_GUIDE.md#nssh-cp-scp-wrapper))
- **Host key pinning** - Pin-on-first-use security model with configurable trust-on-first-use fallback

## Installation

### Install

```bash
# Automated install (downloads to ~/.local/bin)
curl -fsSL https://raw.githubusercontent.com/ntwrknrd/nssh/main/scripts/install.sh | sh

# Or build from source
git clone https://github.com/ntwrknrd/nssh.git
cd nssh
make build

# Initialize nssh (interactive setup)
nssh self init
```

The `init` command guides you through: passphrase creation, SSH config setup, shell integration, include file creation, and optional context credential setup.

> **TIP:** After installation, run `nssh self status` to see what's configured and get actionable next steps.

For detailed instructions & manual setup options see [Getting Started](docs/USER_GUIDE.md#getting-started).

### Upgrade

```bash
nssh self reinstall
```

### Uninstall

1. If you installed shell helpers, remove them first:
   ```bash
   nssh self uninstall
   # add --dry-run to preview what would be removed
   ```
2. Remove the binary:
   ```bash
   rm ~/.local/bin/nssh
   ```

## Learn More

- Users: [User Guide](docs/USER_GUIDE.md)
- Contributors: [Contributing](CONTRIBUTING.md)
- Contributors: [Architecture](docs/ARCHITECTURE.md)

## Acknowledgements

nssh is built on the shoulders of exceptional open-source tools and communities. We are deeply grateful to the maintainers and contributors of:

**Core Dependencies:**

- [OpenSSH](https://www.openssh.com/) (BSD/ISC) - The OpenBSD project's SSH connectivity suite
- [age](https://github.com/FiloSottile/age) (BSD-3-Clause) - Modern file encryption tool
- [fzf](https://github.com/junegunn/fzf) (MIT) - Command-line fuzzy finder
- [asciinema](https://github.com/asciinema/asciinema) (GPLv3) - Terminal session recorder (optional - session recording)

**Go Ecosystem:**

- [Go](https://go.dev/) (BSD-3-Clause) - The Go programming language
- [Cobra](https://github.com/spf13/cobra) (Apache-2.0) - CLI framework
- [memguard](https://github.com/awnumar/memguard) (Apache-2.0) - Secure memory management
- [go-piv](https://github.com/go-piv/piv-go) (Apache-2.0) - YubiKey PIV library (optional - hardware key support)

**License Compatibility:**
This project is licensed under GPL-3.0, which is compatible with all the above dependencies.

## Roadmap

- **FIDO2/WebAuthn Support:** Hardware key authentication via FIDO2 tokens
- **macOS Secure Enclave:** Native Secure Enclave integration for credential protection
- **Native Recording Engine:** Potentially replace asciinema subprocess
