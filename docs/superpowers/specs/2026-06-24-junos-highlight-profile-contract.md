# Junos Highlight Profile Contract

## Summary

This contract defines what `highlight.profile: junos` is allowed to color in
live `nssh` sessions.

The profile is not a full Junos parser. It is a conservative, fast scanner for
operator-significant tokens. It should help the eye find important shape,
state, and identifiers without turning Junos config into a rainbow grammar.

## Goals

- Keep the profile fast enough that output speed remains effectively unchanged.
- Preserve the existing display-only pipeline: raw PTY output stays raw for
  prompt detection, password handling, recordings, and compatibility parsing.
- Color broad operator categories, not every Junos keyword.
- Keep color density low enough that repeated config lines remain readable.
- Make the first few tokens in common `set ...` lines visually distinct enough
  that they do not all collapse into one color.

## Non-Goals

- Do not implement full Junos grammar parsing.
- Do not color every known Junos statement or leaf keyword.
- Do not add user-defined regex rules.
- Do not require multiline context.
- Do not color structural path words just because they are syntactically valid.

## Runtime Contract

The `junos` profile must remain a byte scanner over display-safe chunks or
lines. It must not use regular expressions in the hot path.

The scanner may classify a token using only:

- the token bytes
- the current line's cheap config-shape context
- fixed built-in token sets
- cheap token-shape checks

It must not depend on:

- previous completed lines or next lines
- remote command history
- terminal cursor position
- user-defined rules
- heap-heavy parsing

The live highlighter may carry only the current open line's cheap context and a
bounded unfinished trailing token across PTY display chunks. It must not buffer
whole lines waiting for a newline, and it must reset carried context when a
newline, bypass, unsafe chunk, or oversized line is seen.

Broad word categories must be gated by line shape. Actions, major hierarchies,
protocols, and routing families may be colored only when the current line looks
like Junos configuration.

For v1, a config-shaped line is one of:

- a line whose first token is an allowed action word, such as `set` or `delete`
- a stanza opener whose first token is an allowed major hierarchy, protocol, or
  routing-family word followed by `{`

Free text, login banners, MOTD text, shell prompts, and command output prose
must not color broad Junos words just because the word appears in the text. For
example, `This system is for authorized users only` must not color `system`.
Identifier shapes and operational states may still be colored outside config
shape because they carry useful signal in command output.

## Safety Contract

The profile must pass output through unchanged when the existing highlighter
safety rules apply:

- output already contains ANSI escape sequences
- output contains unsupported control bytes
- a line exceeds the maximum processing length
- output rate crosses the bypass threshold

Any profile bug should fail closed by returning unmodified bytes for the
affected chunk or line.

## Performance Contract

The profile must preserve the current benchmark shape:

- no-token text: zero allocations
- ANSI bypass: zero allocations
- long-line bypass: zero allocations
- token-heavy text: bounded allocations per chunk, not per token

Any expansion of the token contract must include focused tests and should not
materially degrade the scanner benchmarks.

## Allowed Categories

The profile may color only these categories in v1.

### Actions

Actions are top-level or operator-visible configuration verbs. They should be
distinct from hierarchy and protocol words.

Allowed action words:

```text
set, delete, deleted, activate, deactivate, annotate, replace, commit,
rollback, changed
```

### Major Hierarchies

Major hierarchies are broad Junos config families that identify the section of
configuration being shown.

Allowed major hierarchy words:

```text
interfaces, protocols, routing-options, policy-options, firewall,
class-of-service, routing-instances, vlans, bridge-domains, system, chassis,
version, groups, apply-groups, services, security, snmp, forwarding-options,
event-options, accounting-options
```

### Protocols And Routing Families

Protocols and routing families identify the routing or control-plane subsystem.
They are broad enough to color because they help scan a long config quickly.

Allowed protocol and routing words:

```text
bgp, ospf, ospf3, isis, evpn, ldp, mpls, rsvp, lldp, l2-learning,
static, direct, local, aggregate
```

### Identifiers

Identifiers are values that operators commonly search for or compare across
lines.

Allowed identifier shapes:

- IPv4 addresses and prefixes
- IPv6 addresses and prefixes
- MAC addresses
- ASNs in `AS64512` form
- route targets and route distinguishers in compact numeric forms
- Junos interface names, including logical units

IPv6 matching should remain conservative: plain hextet and `::` compressed
forms are allowed, but IPv4-embedded IPv6 forms can wait until there is a real
operator need.

### States

States should keep operational color semantics.

Healthy states:

```text
up, enabled, established, active, selected, forwarding, reachable
```

Warning or transitional states:

```text
inactive, pending, hold, stale, hidden, suppressed, flapping, degraded
```

Bad states:

```text
down, disabled, error, errors, failed, reject, rejected, discard, dropped,
unreachable, timeout, denied
```

## Explicitly Omitted Tokens

The profile must not color structural config path words. These words are too
common, too low-signal, and make repeated config output noisy.

Omitted structural words:

```text
neighbor, group, family, area, interface, type, unit, address
```

The profile must also omit specific Junos knobs and leaf names unless a later
contract revision promotes them into a broader category.

Omitted specific knobs:

```text
minimum-interval, bfd-liveness-detection, interface-type, graceful-restart,
transport-address, router-id, signaling, unicast, inet, inet-vpn,
full-neighbors-only, ipsec-sa, reference-bandwidth, peer-as, multihop,
passive, multiplier
```

These tokens may still appear next to colored identifiers. For example, in:

```text
set protocols bgp group edge neighbor 100.64.128.1 peer-as 65551
```

the profile may color `set`, `protocols`, `bgp`, and `100.64.128.1`, but it
must not color `group`, `neighbor`, or `peer-as`.

## Color Semantics

Colors should express broad meaning:

- actions: one visually distinct command/action style, not a near-match for
  hierarchy coloring
- major hierarchies: one hierarchy style
- protocols and routing families: one protocol style
- identifiers: existing identifier styles, with interfaces separate from
  addresses where possible
- states: green/yellow/red by operational meaning

Do not add more colors just because another token class exists. The profile
should prefer omission over low-value coloring.

## Test Contract

Tests for this profile must verify:

- broad Junos words are gated by config-shaped lines
- login banners and free text do not color broad Junos words
- omitted words remain uncolored in realistic Junos `set` lines
- actions, major hierarchies, protocols, identifiers, and states are colored
- original text is preserved after stripping ANSI sequences
- spans are deterministic and non-overlapping
- bypass paths remain unchanged
- benchmarks keep zero-allocation bypass and no-token behavior

## Change Control

Adding a token is allowed only when it belongs to an allowed category or this
contract is updated first.

When in doubt, do not color the token.
