# Contributing to nssh

This file is for humans and coding agents changing the repo. Read
[SPEC.md](SPEC.md) before changing product contracts or package boundaries,
then inspect current source for implementation details.

## Setup

We recommend running builds and executable tests in a Linux environment. See
[Build and test environment](#build-and-test-environment) for setup.

```bash
git clone https://github.com/YOUR_USERNAME/nssh.git
cd nssh
make build
```

Optional runtime tools depend on what you are testing: `sops`, `age`, `op`, `bw`,
`fzf`, `asciinema`, Docker, and VHS.

## Build and test environment

We recommend running nssh builds and executable tests in a Linux environment.

On macOS, use a Linux container through an installed container runtime, such as
Docker, Podman, or containerd.

Use the repository's [test image](test/docker/test-runner/Dockerfile):

```bash
make container-test
```

The target defaults to Docker. For Podman or containerd's `nerdctl` client, use
`make container-test CONTAINER_RUNTIME=podman` or
`make container-test CONTAINER_RUNTIME=nerdctl`.

Linux CI runs the same command and image. The image provides the Go compiler from
`.go-version`, the pinned golangci-lint version from `Makefile`, Git, Make, and
OpenSSH. The compiler pin is separate from the minimum Go version in `go.mod`.
It runs as a non-root user with a private `XDG_RUNTIME_DIR` and `CGO_ENABLED=0`.
The current checkout is copied into the image, keeping source, binaries, caches,
and temporary test files on Linux storage without macOS bind mounts.

The image runs `make verify`: read-only dependency and formatting checks, vet,
tests, both binaries, the build feature check, lint configuration validation,
lint, and cross-compilation for all
four release platforms. Native macOS CI runs `make check` with the same Go version
and core checks. CI additionally verifies the pushed revision and native macOS
behavior. Tagged releases also pass the shared Linux verification before
packaging and publishing; demo recording generation remains a separate job.

For native Linux or an existing Linux checkout, run:

```bash
make build
make test
make verify
go test ./cmd/nssh -run TestREPLProcess -v
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

`make test` checks the Go version, module tidiness, and formatting before vet and
tests. These checks report failures without rewriting source or module files.
`make check` adds builds and the feature check; `make verify` also requires the
pinned golangci-lint version and cross-compiles all release platforms. The
container image supplies that linter automatically.
Its matching configuration schema is included in the image for offline checks.

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
2. Run `make container-test` (or `make verify` in the configured Linux environment)
   before asking for review.
3. Use `gh` for GitHub actions, for example `gh pr create`.
4. Call out command-surface, config, credential, inventory, and docs changes in
   the PR body.
