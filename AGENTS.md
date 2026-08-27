# AGENTS.md

Audience: coding agents working in this repository or with the 'nssh' application.

Read source before reasoning about behavior. For durable product contracts,
package boundaries, and non-goals, start with [SPEC.md](SPEC.md), then inspect
the current source for implementation details.
For nssh usage and operations questions, use
[`skills/nssh/SKILL.md`](skills/nssh/SKILL.md).

## Commands

```bash
make build
make test
go test ./internal/agent -run TestDaemon
go test ./cmd/nssh -args -update-snapshots
go run ./cmd/nssh <command>
nssh -v <command>
NSSH_DEBUG=1 nssh ...
```

`make test` runs `go vet ./...`, `gofmt -w .`, and `go test ./...`.

## Rules

- Keep changes surgical.
- Keep shared connect/SCP host and credential resolution in `internal/connect`.
- Use `*secret.Secret` for resolved passwords and `secret.Use()` for byte access.
- Update `docs/examples/help/` snapshots when command help changes.
- Validate edited Markdown with the configured `validate-markdown` skill helper.
