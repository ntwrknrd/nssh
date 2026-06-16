# nssh

[![Release](https://img.shields.io/github/v/release/ntwrknrd/nssh)](https://github.com/ntwrknrd/nssh/releases)
[![Build](https://github.com/ntwrknrd/nssh/actions/workflows/build.yml/badge.svg)](https://github.com/ntwrknrd/nssh/actions/workflows/build.yml)
[![Go Version](https://img.shields.io/github/go-mod/go-version/ntwrknrd/nssh)](go.mod)
[![Go Report Card](https://goreportcard.com/badge/github.com/ntwrknrd/nssh)](https://goreportcard.com/report/github.com/ntwrknrd/nssh)
[![Homebrew](https://img.shields.io/badge/homebrew-available-orange)](https://github.com/ntwrknrd/homebrew-nssh)
![Platforms](https://img.shields.io/badge/platform-macOS%20%7C%20Linux-blue)

`nssh` is an SSH wrapper for operators who manage many hosts. It keeps host
inventory, auth policy, routing, and SSH options in nssh YAML config, resolves
credentials from external password managers, injects passwords through OpenSSH
prompts, and can record sessions.

## Demo

![Demo](docs/examples/demo.gif)

## Features

- Smart connect: `nssh HOST`, `nssh user@HOST`, and `nssh connect` route through
  nssh inventory lookup, partial host matching, and optional `fzf` selection.
- Inventory: `nssh inv` manages local hosts and external providers; current
  providers are NetBox and containerlab.
- Credentials: Pass, 1Password, and Bitwarden providers are selected by
  inventory host or group auth mappings. nssh has no local password vault.
- Agent runtime: `nssh agent` brokers provider-session requests and runs
  background recording archive maintenance.
- Connection behavior: OpenSSH still owns transport; nssh wraps it with a PTY
  connector for prompt detection, password injection, host-key handling, timing,
  and typed SSH option rendering.
- Recordings: optional asciinema session capture is managed with `nssh log`.
- SCP: `nssh cp` uses the same host and credential resolution path as connect.

Run `nssh --help` or read the generated help snapshots under
[docs/examples/help](docs/examples/help). The full example config is
[docs/examples/config/config.example.yaml](docs/examples/config/config.example.yaml).

## Installation

```bash
curl -fsSL https://raw.githubusercontent.com/ntwrknrd/nssh/main/scripts/install.sh | sh

# or
brew install ntwrknrd/nssh/nssh

nssh self init
nssh self status
```

`nssh self init` creates the config file, credential provider defaults, and
inventory group bindings. To remove local nssh state:

```bash
nssh self uninstall
```

Use `--dry-run`, `--keep-config`, or `--keep-recordings` when needed. External
password-manager records are not removed.

## Documentation

- [CONTRIBUTING.md](CONTRIBUTING.md) - development workflow, tests, releases,
  and doc rules.
- [.agents/skills/nssh/SKILL.md](.agents/skills/nssh/SKILL.md) - nssh skill
  entrypoint for usage, configuration, inventory, credentials, operations,
  troubleshooting, and architecture references.

## Dependencies

nssh builds around OpenSSH, Cobra, Charm terminal UI packages, `creack/pty`,
optional `fzf`, optional `asciinema`, and external credential CLIs such as
`pass`, `op`, and `bw`. See [LICENSE](LICENSE).
