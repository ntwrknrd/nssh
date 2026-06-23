# Terminal Highlighting Design

## Summary

Add disabled-by-default, host-context-aware terminal highlighting for live nssh
sessions. V1 should be a fast built-in token highlighter, not a generic syntax
engine.

The first useful target is Junos-oriented network output. The implementation
should make important operator signals easier to scan while keeping terminal
throughput effectively unchanged.

## Goals

- Keep highlighting opt-in and disabled by default.
- Resolve highlighting from the same host context nssh already uses for
  inventory-backed connections.
- Support group-level defaults and host-level overrides.
- Keep prompt detection, password filtering, compatibility parsing, and
  recording decisions based on raw output.
- Avoid ChromaTerm-style overhead by using built-in scanners instead of
  user-defined regex profiles.
- Leave a clean interface for future real Junos grammar support.

## Non-Goals

- Do not add arbitrary user-defined regex rules in v1.
- Do not implement full Junos syntax parsing in v1.
- Do not recolor output that already contains ANSI color or complex terminal
  control sequences.
- Do not make highlighting part of SSH transport policy.
- Do not enable highlighting automatically for all sessions.

## Config Shape

Highlighting is top-level display policy, beside `ssh`, not nested under it.

```yaml
highlight:
  enabled: false
  profile: none
```

Inventory groups and hosts can override it:

```yaml
inventory:
  providers:
    netbox-prod:
      groups:
        juniper-core:
          highlight:
            enabled: true
            profile: junos
      hosts:
        edge01:
          highlight:
            enabled: false
```

Resolution order:

1. Global `highlight`
2. Provider group `highlight`
3. Provider host `highlight`

The effective config should be carried on the resolved host, similar to how SSH
options are resolved today.

## V1 Profiles

V1 exposes only built-in profile names:

- `none`: no highlighting
- `junos`: Junos/network operator token highlighting

The config should not expose profile definitions in v1. Custom regexes are too
easy to make slow, hard to reason about, and hard to debug in a live PTY stream.

Future config can add profile customization without changing the connector
pipeline:

```yaml
highlight:
  enabled: false
  profile: junos
  profiles:
    junos-custom:
      extends: junos
      tokens:
        ip_prefix: cyan
        interface: blue
        warning: yellow
        error: red
```

That future shape should allow token category styling, not arbitrary regexes, at
least until there is evidence that user-defined matching is worth the cost.

## Runtime Pipeline

The connector should apply highlighting only to bytes being displayed:

```text
PTY read
-> host-key and password prompt detection
-> password injection
-> password prompt and password echo filtering
-> highlight display output
-> os.Stdout
```

Raw output must remain available for:

- ring-buffer prompt detection
- password prompt matching
- password echo filtering
- `LastOutput()` compatibility parsing
- recording wrapper behavior

Highlighting should decorate only the final stdout payload after sensitive data
filtering.

## Package Boundaries

Add a low-level package under the SSH subsystem:

```text
internal/ssh/highlight
```

This package should not import `internal/ui`, Cobra, recording, agent, or config
loading. It should receive a plain resolved options struct from `connect`.

Proposed core types:

```go
type Options struct {
    Enabled bool
    Profile string
}

type Span struct {
    Start int
    End   int
    Style Style
}

type Profile interface {
    Scan(line []byte) []Span
}
```

The connector owns buffering and calls the highlighter on display-safe chunks or
lines.

## Scanner Model

V1 should use hand-coded byte scanners, not a list of regexes.

The Junos scanner should recognize:

- IPv4 addresses and prefixes
- IPv6 addresses and prefixes, if cheap enough for v1
- MAC addresses
- Junos interface names, including `ge-`, `xe-`, `et-`, `ae`, `irb`, `lo0`,
  `reth`, `vlan`, and common logical unit forms
- VLAN IDs
- ASNs
- common route targets and route distinguishers
- important operator state words
- common Junos config/action words

The scanner should walk each line once, collect spans, then render ANSI styles
in one pass. It should avoid per-token heap allocation where practical.

## Initial Word Groups

Healthy states:

```text
up, enabled, established, active, selected, forwarding, reachable
```

Bad states:

```text
down, disabled, error, errors, failed, reject, rejected, discard, dropped,
unreachable, timeout, denied
```

Warning or transitional states:

```text
inactive, pending, hold, stale, hidden, suppressed, flapping, degraded
```

Routing and protocol words:

```text
static, direct, local, bgp, ospf, isis, evpn, ldp, rsvp, aggregate
```

Config and change words:

```text
set, delete, deleted, deactivate, activate, annotate, replace, commit,
rollback, changed
```

Major Junos hierarchy words:

```text
interfaces, protocols, routing-options, policy-options, firewall,
class-of-service, routing-instances, vlans, bridge-domains, system, chassis
```

The table should stay conservative. Add words only when they help operators
notice important output quickly.

## Color Semantics

Colors should express operational meaning, not generic syntax categories:

- Red: failure, drop, reject, down, denied, unreachable
- Yellow: warning, transitional, hidden, suppressed, stale
- Green: healthy, up, established, forwarding, active
- Cyan: IPs, prefixes, route targets, route distinguishers
- Blue: interfaces
- Magenta: routing protocols and major Junos hierarchies
- Dim: comments or lower-value structure only after config parsing exists

Avoid heavy color density. Over-coloring makes output harder to read and costs
more CPU.

## Safety Rules

- If a line contains ANSI escape sequences, pass it through unchanged.
- If a line contains terminal control bytes beyond tab, carriage return, and
  newline, pass it through unchanged.
- If a line exceeds a fixed maximum length, pass it through unchanged.
- If output rate exceeds a hardcoded threshold, bypass highlighting until output
  calms down.
- Do not process alternate-screen or full-screen application output if it can be
  detected cheaply.
- Do not attempt multiline semantic parsing in v1.

These rules intentionally favor preserving terminal behavior over coloring more
text.

## Performance Contract

The implementation should be benchmarked as part of the feature. Success means
highlighting adds no operator-visible latency and negligible CPU overhead on
large plain-text command output.

Suggested checks:

- large plain lines with no tokens
- large plain lines with many tokens
- ANSI-heavy output, which should hit the bypass path
- long single-line output beyond the processing cap
- realistic Junos command output fixtures

If benchmarking shows measurable impact on high-volume output, the feature
should auto-bypass more aggressively or remain off.

## Error Handling

Invalid config should fail at config validation time:

- unknown profile name
- `enabled: true` with `profile: none`
- malformed future profile definitions, once those exist

Runtime highlighting errors should fail closed by bypassing highlighting for the
affected line or chunk. A highlighter bug must not break an SSH session.

## Testing

Unit tests:

- profile validation accepts `none` and `junos`
- global, group, and host highlight inheritance
- host override can disable group highlighting
- ANSI and control-sequence bypass
- token scanners for IPs, interfaces, MACs, VLANs, ASNs, state words, config
  words, and hierarchy words
- overlap handling produces deterministic spans
- renderer emits valid ANSI and preserves original text content

Connector tests:

- password prompt filtering runs before highlighting
- password echo filtering still masks secrets before highlighting
- `LastOutput()` remains raw enough for compatibility parsing

Benchmarks:

- scanner benchmarks in `internal/ssh/highlight`
- connector-level benchmark or focused test that exercises the stdout transform

Manual verification:

```bash
nssh self bench ssh <host>
NSSH_DEBUG=1 nssh <junos-host>
```

Use before and after comparisons to confirm the feature does not move the hot
path in a meaningful way.

## Future Grammar Path

The v1 `Profile` interface should allow a future Junos grammar to replace or
extend the token scanner without changing connector plumbing.

Future grammar can understand:

- Junos `set` command hierarchy
- config stanza boundaries
- command verbs versus hierarchy keys
- comments and inactive configuration
- values versus keywords
- operational command tables

That work should come only after v1 proves the display pipeline is fast and
safe.
