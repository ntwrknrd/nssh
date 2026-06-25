# Terminal Highlighting

Audience: agents working on nssh highlighting behavior, configuration,
remote-command output rendering, or future TUI rendering.

## Decision

Do not apply Junos highlighting in the interactive SSH connection path.

Keep the `highlight` configuration model and the `junos` highlighting
profile, but reserve them for modes where `nssh` owns rendering. Supported
syntax highlighting targets are remote-command stdout and future TUI rendering.

## Why Interactive Highlighting Was Removed

Interactive SSH output is a PTY byte stream. Reads can split anywhere, including inside words:

```text
set protocols os
pf3 area 0.0.0.0
```

Reliable token coloring would require holding bytes until the highlighter knows whether a token is complete. That breaks interactive terminal behavior because remote echo for typed input can also arrive one byte at a time. Withholding a possible token can make typed letters appear late or only after another keypress.

Best-effort stream highlighting is not acceptable for the interactive path either. It can be fast, but it misses tokens at chunk boundaries; fixing those misses adds buffering that hurts typing. For `nssh host`, terminal correctness matters more than color.

## Runtime Boundary

The interactive connector must preserve raw terminal behavior:

```text
PTY read
-> host-key and password prompt detection
-> password injection
-> password prompt and password echo filtering
-> os.Stdout
```

Do not reintroduce connector-owned highlighting, token buffering, or delayed display writes in the live PTY relay.

Remote-command stdout highlighting uses a separate non-interactive path:

```text
resolve host
-> run remote command without interactive PTY relay
-> capture stdout/stderr
-> highlight complete stdout
-> os.Stdout
```

Password-backed remote commands use OpenSSH askpass with the separate
`nssh-askpass` helper, not a fake PTY. A future TUI mode is also an appropriate
place to revisit richer highlighting because the TUI can own the screen model
and renderer instead of injecting ANSI into an arbitrary live PTY stream.

## Config Contract

Highlighting remains top-level display policy, beside `ssh`, not nested under it:

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

The effective config stays on the resolved host for renderer-owned output
paths. It must not be consumed by the interactive PTY connector.

Supported profile names:

- `none`: no highlighting
- `junos`: Junos/network operator token highlighting

Invalid config should fail validation:

- unknown profile name
- `enabled: true` with `profile: none`

## Junos Profile Contract

The `junos` profile is not a full Junos parser. It is a conservative, fast byte scanner for operator-significant tokens.

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

The profile scans each rendered output buffer line by line. It must not carry
config context across `Highlight` calls, and it must not hold undisplayed bytes
waiting for token completion.

Broad word categories must be gated by line shape. Actions, major hierarchies, protocols, and routing families may be colored only when the current line looks like Junos configuration.

Config-shaped lines are:

- lines whose first token is an allowed action word, such as `set` or `delete`
- stanza openers whose first token is an allowed major hierarchy, protocol, or routing-family word followed by `{`

Free text, login banners, MOTD text, shell prompts, and command output prose must not color broad Junos words just because the word appears in the text. Identifier shapes and operational states may still be colored outside config shape because they carry useful signal in command output.

## Junos Categories

Actions:

```text
set, request, delete, deleted, edit, show, protect, protect:, inactive:,
unprotect, activate, deactivate, annotate, replace, commit, rollback, changed
```

Major hierarchies:

```text
system, forwarding-options, routing-options, routing-instances,
logical-systems, vlans, bridge-domains, dynamic-profiles, interfaces, snmp,
poe, ethernet-switching-options, security, policy-options, protocols, chassis,
firewall, applications, multi-chassis, redundant-power-system, version,
services, virtual-chassis, event-options, class-of-service, access,
accounting-options, diameter, fabric, multicast-snooping-options,
switch-options, wlan, smtp, schedulers, groups, apply-groups
```

Protocols and routing families:

```text
bgp, ospf, ospf3, isis, evpn, ldp, mpls, rsvp, lldp, l2-learning,
static, direct, local, aggregate
```

Identifiers:

- IPv4 addresses and prefixes
- IPv6 addresses and prefixes
- MAC addresses
- ASNs in `AS64512` form
- route targets and route distinguishers in compact numeric forms
- Junos interface names, including logical units and common physical,
  aggregate, redundant, service, loopback, management, IRB, and VLAN families

Sublime-inspired config categories:

- `#` line comments and single-line `/* ... */` annotations
- single-quoted and double-quoted strings, including escaped characters
- unquoted one-token `description` values
- user-defined names after known name-bearing Junos keywords, such as
  `group`, `policy-statement`, `term`, `prefix-list`, `filter`,
  `security-zone`, `routing-instances`, `vlans`, `bridge-domains`,
  `application`, `policer`, `condition`, `rib-groups`, `vrf-import`, and
  `vrf-export`
- routing table names such as `inet.0`, `inet6.0`, `mpls.0`,
  `bgp.l2vpn.0`, `bgp.l3vpn.0`, and prefixed table forms
- URLs using `http`, `https`, `scp`, `ftp`, `tftp`, or `sftp`
- targeted numeric values after `unit`, `peer-unit`, `queue`, `queue-num`,
  `destination-port`, or `source-port`

IPv6 matching should remain conservative. Plain hextet and `::` compressed forms are allowed; IPv4-embedded IPv6 forms can wait until there is a real operator need.

States:

- Healthy: `up`, `enabled`, `established`, `active`, `selected`, `forwarding`, `reachable`
- Warning or transitional: `inactive`, `pending`, `hold`, `stale`, `hidden`, `suppressed`, `flapping`, `degraded`
- Healthy policy/filter actions: `accept`, `permit`
- Bad: `down`, `disabled`, `error`, `errors`, `failed`, `reject`,
  `rejected`, `discard`, `dropped`, `unreachable`, `timeout`, `denied`

The Sublime Junos package is a category reference only. Do not vendor its files
or copy its regexes literally unless an upstream license or explicit permission
allows it. The runtime scanner should stay a clean-room byte scanner.

## Explicitly Omitted Tokens

Do not color low-signal structural config path words:

```text
neighbor, group, family, area, interface, type, unit, address
```

Do not color specific Junos knobs unless a later contract revision promotes them into a broader category:

```text
minimum-interval, bfd-liveness-detection, interface-type, graceful-restart,
transport-address, router-id, signaling, unicast, inet, inet-vpn,
full-neighbors-only, ipsec-sa, reference-bandwidth, peer-as, multihop,
passive, multiplier
```

Prefer omission over low-value coloring.

Do not color general standalone numbers. Numeric highlighting is limited to
operator-significant values listed in the Junos categories above.

## Safety And Performance

The profile must pass output through unchanged when safety rules apply:

- output already contains ANSI escape sequences
- output contains unsupported control bytes
- a line exceeds the maximum processing length

Benchmark shape to preserve:

- no-token text: zero allocations
- ANSI bypass: zero allocations
- long-line bypass: zero allocations
- token-heavy text: bounded allocations per rendered buffer, not per token

Runtime must not strip ANSI. Tests may strip nssh-added ANSI only to prove that
highlighting preserves the original text.

Tests should cover config inheritance, validation, line-shape gating, omitted
tokens, text preservation after stripping nssh-added ANSI, semantic span kinds,
deterministic non-overlapping spans, pass-through paths, and scanner benchmarks.

## Future Direction

For non-interactive ad-hoc commands, `nssh` captures stdout and highlights
complete stdout before printing it. That avoids typed input, prompt redraws, and
PTY echo latency.

For TUI mode, highlighting can be more reliable because the TUI owns rendering. That is the better place to revisit grammar-aware highlighting.
