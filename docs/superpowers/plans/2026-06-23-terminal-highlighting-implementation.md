# Terminal Highlighting Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add disabled-by-default, host-context-aware Junos terminal highlighting without noticeable terminal output slowdown.

**Architecture:** Add top-level config and resolved-host inheritance, then keep the hot path display-only: raw PTY bytes still feed prompt detection and `LastOutput()`, while filtered stdout bytes pass through a fast built-in scanner. The scanner is hand-coded, single-pass, allocation-light, and bypasses unsafe or high-volume output.

**Tech Stack:** Go, PTY connector, YAML config, built-in ANSI SGR output.

---

### Task 1: Config Surface And Resolution

**Files:**

- Modify: `internal/config/settings.go`
- Modify: `internal/config/inventory.go`
- Modify: `internal/config/settings_test.go`
- Modify: `internal/connect/catalog.go`
- Modify: `internal/connect/resolve.go`
- Modify: `internal/connect/catalog_test.go`

- [ ] **Step 1: Write failing config validation tests**

Add tests proving `none` and `junos` are accepted, unknown profiles fail, and `enabled: true` with `profile: none` fails.

- [ ] **Step 2: Write failing inheritance test**

Add a catalog test proving resolution order is global `highlight`, provider group `highlight`, provider host `highlight`, and host-level `enabled: false` disables a highlighted group.

- [ ] **Step 3: Implement config types**

Add `HighlightConfig` with pointer `Enabled *bool` so unset and explicit false can be distinguished during inheritance, plus profile validation and `MergeHighlight`.

- [ ] **Step 4: Carry resolved highlight policy**

Add `Highlight config.HighlightConfig` to `ResolvedHostData` and `ResolvedHost`, and compute it in catalog and literal-host resolution.

- [ ] **Step 5: Verify config tests**

Run: `go test ./internal/config ./internal/connect`

Expected: pass.

### Task 2: Fast Built-In Highlighter

**Files:**

- Create: `internal/ssh/highlight/highlight.go`
- Create: `internal/ssh/highlight/highlight_test.go`
- Create: `internal/ssh/highlight/highlight_bench_test.go`

- [ ] **Step 1: Write failing highlighter tests**

Cover disabled/no-profile passthrough, ANSI/control bypass, long-line bypass, IPv4/prefixes, MACs, interfaces, ASNs, route targets, state words, config words, hierarchy words, valid ANSI wrapping, and original text preservation.

- [ ] **Step 2: Implement scanner and renderer**

Implement a hand-coded byte scanner with no regex, deterministic non-overlap, one render allocation only when spans exist, and fail-closed passthrough on unsafe chunks.

- [ ] **Step 3: Add benchmarks**

Benchmark no-token lines, token-heavy Junos output, ANSI bypass, and long-line bypass.

- [ ] **Step 4: Verify highlighter package**

Run: `go test ./internal/ssh/highlight`

Run: `go test -bench=. ./internal/ssh/highlight`

Expected: tests pass and bypass benchmarks stay effectively allocation-free.

### Task 3: Connector Plumbing

**Files:**

- Modify: `internal/ssh/connector/connector.go`
- Modify: `internal/ssh/connector/relay_unix.go`
- Modify: `internal/ssh/connector/password_unix.go`
- Create: `internal/ssh/connector/highlight_unix_test.go`
- Modify: `internal/connect/connect.go`

- [ ] **Step 1: Write failing connector tests**

Add tests proving password prompt removal and password echo masking happen before highlighting, and raw ring-buffer output remains unhighlighted.

- [ ] **Step 2: Implement connector display transform**

Add `SetHighlightOptions`, a private display-prep method, and call highlighting only after `filterOutput`.

- [ ] **Step 3: Pass resolved options from connect**

Map resolved `config.HighlightConfig` into `highlight.Options` in both initial and retry connector creation.

- [ ] **Step 4: Verify connector tests**

Run: `go test ./internal/ssh/connector ./internal/connect`

Expected: pass.

### Task 4: Final Verification

**Files:**

- Modify only docs that are directly required by changed public config shape.

- [ ] **Step 1: Run touched packages**

Run: `go test ./internal/config ./internal/connect ./internal/ssh/...`

Expected: pass.

- [ ] **Step 2: Run highlighter benchmarks**

Run: `go test -bench=. -benchmem ./internal/ssh/highlight`

Expected: passthrough/bypass paths have zero allocations and token-heavy path has bounded allocations.

- [ ] **Step 3: Validate Markdown**

Run: `validate-markdown --file docs/superpowers/plans/2026-06-23-terminal-highlighting-implementation.md`

Expected: pass.
