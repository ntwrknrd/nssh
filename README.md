# nssh

SSH tooling for operators who need fast, repeatable access to network equipment with age-encrypted credentials, context-aware host management, and terminal session recording.

## Table of Contents

- [Demo](#demo)
- [Features](#features)
- [Installation](#installation)
  - [Prerequisites](#prerequisites)
  - [Install](#install)
  - [Uninstall](#uninstall)
- [Learn More](#learn-more)
- [Acknowledgements](#acknowledgements)
- [Roadmap](#roadmap)

## Demo

_Coming soon: asciinema embed highlighting fuzzy host search + credential resolution._

## Features

- **Fuzzy host selection** - Exact matches connect instantly (sub-200ms); partial matches use `fzf` for interactive filtering
- **Age-encrypted credentials** - Context-aware vault with environment/host/user overrides; passwords never in plaintext or CLI args (streamed via in-memory pipes to `sshpass`)
- **SSH config management** - Create, remove, sort, and update host entries in SSH config files with automatic alphabetical sorting, timestamped backups, and indexed lookups across SSH 'Include' config files
- **Shell integration** - History tracking (Bash/Zsh/Fish/Atuin) and tab completion for hostnames, contexts, and commands
- **Performance telemetry** - Built-in benchmarking with stage-level timing and regression detection; see [Performance Analysis](docs/USER_GUIDE.md#nssh-benchmark-performance-analysis)
- **Session recording & playback** - Automatic asciinema integration with host-based filtering, append mode for concurrent sessions, automatic cleanup, and comprehensive session management via `nssh log` CLI (list/play/upload/export with pattern matching and interactive selection)

## Installation

### Prerequisites

1. Ensure OpenSSH, `sshpass`, `age`, `jq`, `fzf`, `uv`, and Python 3.14 (installed via `uv python install 3.14`) are available on your system.
2. (Recommended) Install `asciinema` v3+ for session recording features - see [Session Recording & Playback](docs/USER_GUIDE.md#nssh-log-session-recording)
3. Ensure you have an age key configured for credential encryption - see [Key Management](docs/USER_GUIDE.md#key-management).


### Install

1. Clone the repo:
   ```bash
   git clone https://github.com/ntwrknrd/nssh.git
   cd nssh
   ```
2. Install the Python entry points into `~/.local/bin`:
   ```bash
   uv tool install .
   ```
3. Deploy the wrapper + shell helpers (installs to `~/.local/bin` and `~/.local/share/nssh` by default). This drops a single `nssh` wrapper, the Python shim the wrapper calls, and refreshed Fish/Bash helpers:
   ```bash
   nssh install-shell --shell-profile ~/.bashrc
   # add --dry-run to preview, --bin-dir/--share-dir to customize paths
   ```

### Uninstall

1. Remove the uv-installed tool:
   ```bash
   uv tool uninstall nssh
   ```
2. Delete any symlinks or shell-integration lines you added (e.g., remove the `nssh` entry in `~/.local/bin` or clean up shell rc files).
3. Optionally remove caches and encrypted stores if you no longer need them:
   ```bash
   rm -f ~/.ssh/.nssh_host_index ~/.ssh/nssh_credentials.age
   rm -rf ~/.ssh/backups
   ```
4. Optionally remove recording data and configuration:
   ```bash
   rm -rf ~/.local/state/nssh/casts       # Session recordings, lock directories, session counters, and index files
   rm -f ~/.config/nssh/config.toml        # Recording configuration
   ```

## Learn More

- Operators: [User Guide](docs/USER_GUIDE.md) for CLI walkthroughs, [Security Best Practices](docs/USER_GUIDE.md#security-best-practices), and [Performance Analysis](docs/USER_GUIDE.md#nssh-benchmark).
- Internals: [Architecture](docs/ARCHITECTURE.md) for algorithms, data formats, and [Performance Metrics](docs/ARCHITECTURE.md#performance-metrics).
- Contributors: [CONTRIBUTING.md](CONTRIBUTING.md) for coding standards, tooling, and required tests.
- CLI help snapshots: [docs/examples/help/](docs/examples/help) contains captured `--help` output for `nssh`, `nssh cred`, `nssh host`, `nssh log`, and `nssh benchmark`.

## Acknowledgements

nssh is built on the shoulders of exceptional open-source tools and communities. We are deeply grateful to the maintainers and contributors of:

**Core Dependencies:**

- [OpenSSH](https://www.openssh.com/) (BSD/ISC) - The OpenBSD project's SSH connectivity suite, maintained by Damien Miller, Darren Tucker, and the OpenBSD team
- [sshpass](https://sourceforge.net/projects/sshpass/) (GPLv2) - Non-interactive SSH password authentication by Lingnu Open Source Consulting
- [age](https://github.com/FiloSottile/age) (BSD-3-Clause) - Modern file encryption tool by Filippo Valsorda and contributors
- [jq](https://jqlang.github.io/jq/) (MIT) - Lightweight JSON processor by Stephen Dolan and the jq community
- [fzf](https://github.com/junegunn/fzf) (MIT) - Command-line fuzzy finder by Junegunn Choi
- [asciinema](https://github.com/asciinema/asciinema) (GPLv3) - Terminal session recorder by Marcin Kulik and contributors (optional - session recording)
- [uv](https://github.com/astral-sh/uv) (MIT/Apache-2.0) - Fast Python package installer by Astral

**Python Ecosystem:**

- [Python](https://www.python.org/) (PSF License) - The Python Software Foundation and core development team
- [Typer](https://github.com/fastapi/typer) (MIT) - CLI framework by Sebastián Ramírez (tiangolo) and contributors
- [Rich](https://github.com/Textualize/rich) (MIT) - Terminal formatting library by Will McGugan and the Textualize team

**License Compatibility:**
This project is licensed under GPL-3.0-or-later, which is compatible with all the above dependencies. The GPL license ensures that nssh and any derivatives remain free and open-source software, while respecting the more permissive licenses of our dependencies.

## Roadmap

- **Full Python Execution Path (under research):** we're actively prototyping an all-Python connector that would retire the Bash wrapper, `sshpass`, and external `fzf`. The spike covers: (1) transport via AsyncSSH/Paramiko with first-class agent + keyboard-interactive support, (2) `pyfzf`/`textual`-style interactive host search that works consistently on macOS/Linux/WSL, (3) unified timing + tracing hooks inside the Python process, and (4) a programmable API so CI and GUI front-ends can drive connections without shell glue. Open questions include PTY fidelity (mirroring `ssh -tt`)
