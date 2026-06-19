# Contributing to nssh

This file is for humans and coding agents changing the repo. Read the
[nssh architecture reference](.agents/skills/nssh/references/architecture.md)
before changing architecture or command behavior.

## Setup

```bash
git clone https://github.com/YOUR_USERNAME/nssh.git
cd nssh
make build
```

Optional runtime tools depend on what you are testing: `pass`, `op`, `bw`,
`fzf`, `asciinema`, Docker, and VHS.

## Commands

```bash
make build
make test
make darwin-arm64
make darwin-amd64
make linux-amd64
make linux-arm64
```

`make test` runs `go vet ./...`, `gofmt -w .`, and `go test ./...`.

Useful focused commands:

```bash
go test ./internal/agent -run TestDaemon
go test ./cmd/nssh -args -update-snapshots
go run ./cmd/nssh <command>
nssh self reinstall --dev
nssh self bench ssh <host>
nssh self bench scp <host>
```

Use `nssh -v <command>` for debug logging and `NSSH_DEBUG=1 nssh ...` for
connector timing markers.

## Coding Rules

- Read the current source before changing behavior. Do not patch docs from
  memory.
- Keep `cmd/nssh/main.go` thin; it should delegate to `internal/app`.
- Keep shared host lookup and credential resolution in `internal/connect`.
- Keep provider storage and auth ownership in `internal/credential`; do not
  reintroduce a local nssh credential vault.
- Keep external inventory reconciliation in `internal/inventory`; generated SSH
  config files are provider-owned.
- Keep `internal/agent` free of CLI, UI, and SSH imports. Import boundary tests
  enforce this.
- Keep `internal/ssh/...` below orchestration packages; SSH packages must not
  import CLI, UI, recording, or agent code.
- Wrap resolved passwords in `*secret.Secret`, access bytes only through
  `secret.Use()`, and destroy request-scoped secrets when done.
- Return scripting failures through `internal/exit`: connection is 2, auth is 3,
  host-not-found is 4, not-executable is 126, not-found is 127.
- Route user-facing terminal output through `internal/ui`.

## Generated Files

- CLI help snapshots live in `docs/examples/help/` and are tested by
  `cmd/nssh/help_test.go`.
- The embedded example config is
  `docs/examples/config/config.example.yaml`; `internal/config/embed.go`
  exposes it to `nssh self init`.
- Demo media and example command outputs under `docs/examples/` are retained as
  examples/assets, not narrative docs.

After changing command flags, help text, or generated examples, run:

```bash
go test ./cmd/nssh -args -update-snapshots
```

When creating or editing Markdown, validate it:

```bash
/Users/cj/.local/bin/validate-markdown --file <path>
```

Keep narrative docs limited to [README.md](README.md),
[CONTRIBUTING.md](CONTRIBUTING.md), and the nssh skill under
[.agents/skills/nssh/](.agents/skills/nssh/). Prefer links to code paths,
function names, generated help, or example config over duplicating volatile
details.

## Opening a PR

1. Run the relevant focused tests.
2. Run `make test` before asking for review.
3. Use `gh` for GitHub actions, for example `gh pr create`.
4. Call out command-surface, config, credential, inventory, and docs changes in
   the PR body.
