# Evaluated terminal frontend alternative

Go/Charm is the maintained and shipped REPL. A historical Rust frontend is an
evaluated alternative. Preserve its design and reconstruction record without
maintaining two frontends or shipping an unused broker.

## Evidence and tradeoff

Commit `492293261f0261c0a2c4d22fb0f7c69886bd15d5` on the historical `tui` branch
contains a Ratatui/Crossterm frontend and a hidden Go JSON broker. Rust owns input
and rendering; Go owns inventory, credentials, parsing, scheduling, and SSH.
The experiment explores editing, completion, scrolling, selection/copy, split
comparisons, and large transcripts. Source and tests establish a prototype;
this implementation has not rebuilt it or measured an advantage over Go/Charm.

The alternative offers another rendering/input stack but adds a toolchain,
process lifecycle, protocol, and UI implementation. Revisit it when a concrete
Go terminal limitation justifies comparison. Rust alone does not establish
better raw-mode handling, selection, or terminal compatibility.

## Recover the historical experiment

Inspect these paths at the pinned commit:

| Path | Purpose |
| --- | --- |
| `experiments/repl-ratatui/Cargo.toml`, `Cargo.lock` | Toolchain dependency snapshot |
| `experiments/repl-ratatui/src/main.rs` | Historical process launch |
| `experiments/repl-ratatui/src/protocol.rs` | Rust wire messages |
| `internal/repl/broker/broker.go` | Go newline-delimited JSON broker |
| `internal/repl/broker/broker_test.go`, `experiments/repl-ratatui/tests/` | Protocol, state, input, and rendering tests |
| `docs/repl-tui-adapters.md`, `plan.txt` | Evaluation rationale and comparison criteria |

In an isolated checkout of that commit, the historical launch is:

```bash
cargo run --locked --manifest-path experiments/repl-ratatui/Cargo.toml
```

The launcher tries `go run ./cmd/nssh repl broker --json` from its inferred
repository, falling back to installed `nssh`. Inspect and replace that implicit
fallback with an explicit evaluation binary before use. The historical command
is not available in current nssh. The protocol covered submit, suggest, cancel,
history, and exit, plus ready, progress, completion, status, and error events;
completed output bytes were base64 encoded.

## Rebuild against any later Go revision

1. Create an isolated evaluation branch from the chosen Go revision. Record its
   commit, OS, terminal, toolchain versions, and dependency locks. Use historical
   source as design evidence; keep current inventory and execution semantics.
2. Adapt `internal/repl`'s parser, executor, typed events, and results through a
   small experimental process bridge. Reuse `internal/connect` for capture and
   `internal/cli/repl` as the current reference for history, completion, and
   interaction ownership. Map current behavior rather than copying the old
   schema or command-wide scheduling barrier.
3. Carry stream identity, output limits, status, cancellation/cleanup, and
   explicit prompt/response ownership across the bridge. Bound message size and
   backpressure. Keep all credential and SSH operations in Go.
4. Rebuild Rust presentation against that bridge and pass an explicit Go binary
   path. Match both sides to the same evaluation revision. Document any UI-only
   duplicated logic as part of the experiment's cost.
5. Exercise both frontends with synthetic events, fake SSH processes, and local
   PTYs. Compare editing, completion, scrolling, resize/wrapping, selection/copy,
   split comparisons, large output, terminal escapes, and cancellation. Save
   launch commands, fixtures, supported features, and measured results.
6. Decide whether the benefit warrants adoption. Archive the evaluation commit
   and findings when stopping; do not establish ongoing parity or CI for two
   frontends merely to preserve this option.

The reusable preparation is the separation already needed by plain and
interactive Go modes: typed execution results, bounded work, and explicit
terminal ownership. No internal API freeze, maintained wire schema, broker,
or adapter framework is required. Reconstruction may need adaptation to the
then-current Go app; it is not a promise of zero-porting compatibility.
