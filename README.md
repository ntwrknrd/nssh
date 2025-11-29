# nssh

SSH tooling for operators who need fast, repeatable access to network equipment with age-encrypted credentials, context-aware host management, and terminal session recording.

## Table of Contents

- [Demo](#demo)
- [Features](#features)
- [Installation](#installation)
- [Learn More](#learn-more)
- [Acknowledgements](#acknowledgements)
- [Roadmap](#roadmap)

## Demo

_Coming soon: asciinema embed highlighting fuzzy host search + credential resolution._

## Features

- **Interactive PTY connector** - In-process password injection without external tools (see [ARCHITECTURE.md](docs/ARCHITECTURE.md#pty-connector-architecture))
- **Fuzzy host selection** - Exact matches connect instantly; partial matches use `fzf` for interactive filtering
- **Age-encrypted credentials** - Context-aware vault with environment/host/user overrides; passwords never in plaintext or CLI args (streamed directly through the PTY connector)
- **SSH config management** - Create, remove, sort, and update host entries in SSH config files with automatic alphabetical sorting, timestamped backups, and indexed lookups across SSH 'Include' config files
- **Shell integration** - History tracking (Bash/Zsh/Fish/Atuin) and tab completion for hostnames, contexts, and commands
- **Performance telemetry** - Built-in benchmarking with stage-level timing and regression detection; see [Performance Analysis](docs/USER_GUIDE.md#nssh-benchmark-performance-analysis)
- **Session recording & playback** - Automatic asciinema integration with host-based filtering, append mode for concurrent sessions, automatic cleanup, and comprehensive session management via `nssh log` CLI (list/play/upload/export with pattern matching and interactive selection)
- **File transfers** - Standard SCP CLI with credential vault integration (see [File Transfer](docs/USER_GUIDE.md#nssh-cp-file-transfer))

## Installation

### Quick Install

```bash
# Install prerequisites: OpenSSH, age, fzf, uv, Python 3.14

# Clone and install
git clone https://github.com/ntwrknrd/nssh.git
cd nssh
uv tool install .

# Initialize nssh (interactive setup)
nssh self init
```

The `init` command guides you through: age key creation, SSH config setup, shell integration, include file creation, and optional context credential setup.

> **TIP:** After installation, run `nssh self status` to see what's configured and get actionable next steps.

For detailed prerequisites, manual setup options, and platform-specific installation instructions, see [Getting Started](docs/USER_GUIDE.md#getting-started).


### Uninstall

1. If you installed shell helpers, remove them first:
   ```bash
   nssh self cleanup
   # add --keep-config to preserve credentials and configuration
   # add --keep-recordings to preserve session recordings
   ```
2. Remove the uv-installed tool:
   ```bash
   uv tool uninstall nssh
   ```

## Learn More

- Operators: [User Guide](docs/USER_GUIDE.md)
- Internals: [Architecture](docs/ARCHITECTURE.md)
- Contributors: [CONTRIBUTING.md](CONTRIBUTING.md)

## Acknowledgements

nssh is built on the shoulders of exceptional open-source tools and communities. We are deeply grateful to the maintainers and contributors of:

**Core Dependencies:**

- [OpenSSH](https://www.openssh.com/) (BSD/ISC) - The OpenBSD project's SSH connectivity suite, maintained by Damien Miller, Darren Tucker, and the OpenBSD team
- [age](https://github.com/FiloSottile/age) (BSD-3-Clause) - Modern file encryption tool by Filippo Valsorda and contributors
- [fzf](https://github.com/junegunn/fzf) (MIT) - Command-line fuzzy finder by Junegunn Choi
- [asciinema](https://github.com/asciinema/asciinema) (GPLv3) - Terminal session recorder by Marcin Kulik and contributors (optional - session recording)
- [uv](https://github.com/astral-sh/uv) (MIT/Apache-2.0) - Fast Python package installer by Astral

**Python Ecosystem:**

- [Python](https://www.python.org/) (PSF License) - The Python Software Foundation and core development team
- [Click](https://github.com/pallets/click) (BSD-3-Clause) - CLI framework by Pallets
- [Rich](https://github.com/Textualize/rich) (MIT) - Terminal formatting library by Will McGugan and the Textualize team

**License Compatibility:**
This project is licensed under GPL-3.0-or-later, which is compatible with all the above dependencies. The GPL license ensures that nssh and any derivatives remain free and open-source software, while respecting the more permissive licenses of our dependencies.

## Roadmap

- **Programmable Sessions:** Expose PTY connector as Python API for CI and GUI integration
- **Native Recording Engine:** Replace asciinema subprocess with native v3 format writer in PTY relay loop for zero-overhead recording
