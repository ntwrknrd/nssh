# AGENTS.md

Audience: coding agents working in this repository or with the 'nssh' application.

Read source before reasoning about behavior. For durable product contracts,
package boundaries, and non-goals, start with [SPEC.md](SPEC.md), then inspect
the current source for implementation details.
For nssh usage and operations questions, use
[`skills/nssh/SKILL.md`](skills/nssh/SKILL.md).

## Commands

We recommend running nssh builds and executable tests in a Linux environment.
On macOS, use a Linux container through an installed container runtime, such as
Docker, Podman, or containerd. Keep the checkout, binaries, caches, and temporary
test files on Linux storage, outside macOS shared mounts. See
[CONTRIBUTING.md](CONTRIBUTING.md) for setup and commands.

Use `make container-test` for the same Linux verification image and checks as CI.
Set `CONTAINER_RUNTIME=podman` or `CONTAINER_RUNTIME=nerdctl` when appropriate.

```bash
make build
make test
make verify
go test ./internal/agent -run TestDaemon
go test ./cmd/nssh -args -update-snapshots
go run ./cmd/nssh <command>
nssh -v <command>
NSSH_DEBUG=1 nssh ...
```

`make test` checks the Go version, dependencies, and formatting, then runs vet and
tests without rewriting source. `make verify` also builds both binaries, checks
the feature output, runs lint, and cross-compiles all release platforms.

## Rules

- Keep changes surgical.
- Keep shared connect/SCP host and credential resolution in `internal/connect`.
- Use `*secret.Secret` for resolved passwords and `secret.Use()` for byte access.
- Update `docs/examples/help/` snapshots when command help changes.
- Validate edited Markdown with the configured `validate-markdown` skill helper.
