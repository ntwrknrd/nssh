# Contributing to nssh

This file is for humans and coding agents changing the repo. Read
[SPEC.md](SPEC.md) before changing product contracts or package boundaries,
then inspect current source for implementation details.

## Setup

On macOS, run the build and test commands in an OrbStack Linux machine.

```bash
git clone https://github.com/YOUR_USERNAME/nssh.git
cd nssh
make build
```

Optional runtime tools depend on what you are testing: `sops`, `age`, `op`, `bw`,
`fzf`, `asciinema`, Docker, and VHS.

## macOS test environment

Run nssh builds and executable tests inside an OrbStack Linux machine. Keep the
checkout, generated binaries, Go cache, and temporary test files in the machine's
Linux filesystem. Use `orb list` to select the machine, then run the commands
below from its checkout.

```bash
orb -m alma -w /path/to/linux/checkout make test
orb -m alma -w /path/to/linux/checkout go test ./cmd/nssh -run TestREPLProcess -v
```

The process tests use fake SSH executables and local PTYs, covering separate
streams, per-host failures, stdin ownership, cancellation, completion, history,
resize, and host-key decisions.

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
go test -race ./internal/repl ./internal/cli/repl ./internal/connect ./internal/ssh/captured ./internal/credential/...
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
- Keep external inventory reconciliation in `internal/inventory`. Operator YAML
  owns policy; provider state is a refreshable cache and SSH files are projections.
- Keep `internal/agent` free of CLI, UI, and SSH imports. Import boundary tests
  enforce this.
- Keep `internal/ssh/...` below orchestration packages; SSH packages must not
  import CLI, UI, recording, or agent code.
- Keep REPL parsing, scheduling, and retained result state in `internal/repl`.
  `internal/cli/repl` owns presentation only; it must not bypass `internal/connect`
  for catalog, credential, host-key, or captured-command behavior. The maintained
  terminal frontend is Go/Charm. See the
  [frontend evaluation record](skills/nssh/references/frontend-alternatives.md)
  for the deferred Rust approach and reconstruction procedure.
- Wrap resolved passwords in `*secret.Secret`, access bytes only through
  `secret.Use()`, and destroy request-scoped secrets when done.
- Return scripting failures through `internal/exit`: connection is 2, auth is 3,
  host-not-found is 4, not-executable is 126, not-found is 127.
- Route user-facing terminal output through `internal/ui`.

## Generated Files

- CLI help snapshots live in `docs/examples/help/` and are tested by
  `cmd/nssh/help_test.go`.
- The embedded first-run config template is
  `internal/config/example_config.yaml`; `internal/config/embed.go` exposes it
  to `nssh self init`.
- Demo media and example command outputs under `docs/examples/` are retained as
  examples/assets, not narrative docs.

After changing command flags, help text, or generated examples, run:

```bash
go test ./cmd/nssh -args -update-snapshots
```

When creating or editing Markdown, validate it:

Use the configured `ntwrknrd-workflows:mdcheck` skill and its bundled helper:

```bash
uv run <mdcheck-skill>/scripts/mdcheck.py --file <path>
uv run <mdcheck-skill>/scripts/mdcheck.py --check <path>
```

Keep narrative docs limited to [README.md](README.md),
[CONTRIBUTING.md](CONTRIBUTING.md), [SPEC.md](SPEC.md), and the nssh skill under
[skills/nssh/](skills/nssh/). Prefer current source, generated
help, or the config template over duplicating volatile implementation details.

For REPL changes, exercise fake SSH processes with isolated XDG directories;
cover mixed host failures, output limits, remote EOF, and cancellation cleanup.
Check terminal editing, completion, scrolling, resize, and host-key handoff with a
local PTY. These checks require no production targets or real credentials.

## Opening a PR

1. Run the relevant focused tests.
2. Run `make test` before asking for review.
3. Use `gh` for GitHub actions, for example `gh pr create`.
4. Call out command-surface, config, credential, inventory, and docs changes in
   the PR body.
