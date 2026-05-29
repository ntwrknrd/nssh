# nssh

[![Release](https://img.shields.io/github/v/release/ntwrknrd/nssh)](https://github.com/ntwrknrd/nssh/releases)
[![Build](https://github.com/ntwrknrd/nssh/actions/workflows/build.yml/badge.svg)](https://github.com/ntwrknrd/nssh/actions/workflows/build.yml)
[![Go Version](https://img.shields.io/github/go-mod/go-version/ntwrknrd/nssh)](go.mod)
[![Go Report Card](https://goreportcard.com/badge/github.com/ntwrknrd/nssh)](https://goreportcard.com/report/github.com/ntwrknrd/nssh)
[![Homebrew](https://img.shields.io/badge/homebrew-available-orange)](https://github.com/ntwrknrd/homebrew-nssh)
[![Platforms](https://img.shields.io/badge/platform-macOS%20%7C%20Linux-blue)]()

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
- **Agent-based credential management** - Background daemon holds decrypted credentials with configurable idle/lifetime timeouts and passphrase-protected keys
- **Age-encrypted vault** - Context-aware storage with domain-based resolution and host-specific overrides; passwords never in plaintext or CLI args (streamed directly through the PTY connector)
- **SSH config management** - Create, remove, sort, and update host entries in SSH config files with automatic alphabetical sorting, timestamped backups, and indexed lookups across SSH 'Include' config files
- **Legacy device compatibility** - Auto-detects SSH algorithm mismatches and applies KEX/cipher/MAC fixes for older network equipment (see [ARCHITECTURE.md](docs/ARCHITECTURE.md#ssh-compatibility-detection-and-remediation))
- **Shell integration** - History tracking (Bash/Zsh/Fish) and tab completion for hostnames, contexts, and commands
- **Session recording & playback** - Automatic asciinema integration with host-based filtering, idle time limiting, automatic archival, and comprehensive session management via `nssh log` CLI (list/play/upload/export/delete with pattern matching and interactive selection)
- **File transfers** - Standard SCP CLI with credential vault integration (see [USER_GUIDE.md](docs/USER_GUIDE.md#nssh-cp-scp-wrapper))
- **Host key pinning** - Pin-on-first-use security model with configurable trust-on-first-use fallback

## Installation

### Install

Automated install script

```bash
curl -fsSL https://raw.githubusercontent.com/ntwrknrd/nssh/main/scripts/install.sh | sh
```

Homebrew install (if applicable)

```bash
brew install ntwrknrd/nssh/nssh
```

Initialize nssh (interactive setup)

```bash
nssh self init
```

The `init` command guides you through: passphrase creation, SSH config setup, shell integration, include file creation, and optional context credential setup. **TIP:** After installation, run `nssh self status`

For detailed instructions & manual setup options see [Getting Started](docs/USER_GUIDE.md#getting-started).

### Uninstall

```bash
nssh self uninstall
# add --dry-run to preview what would be removed
```

This removes shell integration, the binary, config, and recordings. Use `--keep-config` or `--keep-recordings` to preserve specific data.

## Learn More

- Users: [User Guide](docs/USER_GUIDE.md)
- Contributors: [Contributing](CONTRIBUTING.md)
- Contributors: [Architecture](docs/ARCHITECTURE.md)

## Acknowledgements

nssh is built on the shoulders of exceptional open-source tools and communities. We are deeply grateful to the maintainers and contributors of:

**Core Dependencies:**

- [OpenSSH](https://www.openssh.com/) (BSD/ISC) - The OpenBSD project's SSH connectivity suite
- [fzf](https://github.com/junegunn/fzf) (MIT) - Command-line fuzzy finder (optional - enhanced fuzzy finding)
- [asciinema](https://github.com/asciinema/asciinema) (GPLv3) - Terminal session recorder (optional - session recording)

**Go Ecosystem:**

- [Go](https://go.dev/) (BSD-3-Clause) - The Go programming language
- [creack/pty](https://github.com/creack/pty) (MIT) - PTY handling
- [Cobra](https://github.com/spf13/cobra) (Apache-2.0) - CLI framework
- [Charm](https://github.com/charmbracelet) (MIT) - Terminal UI libraries (Huh, Lipgloss, Bubble Tea)
- [age](https://github.com/FiloSottile/age) (BSD-3-Clause) - Modern file encryption
- [memguard](https://github.com/awnumar/memguard) (Apache-2.0) - Secure memory management

**License Compatibility:**
This project is licensed under [GNU GPL-3.0](https://github.com/ntwrknrd/nssh/blob/main/LICENSE), which is compatible with all the above dependencies.

## Roadmap

- **Native Recording Engine:** Potentially replace asciinema subprocess
