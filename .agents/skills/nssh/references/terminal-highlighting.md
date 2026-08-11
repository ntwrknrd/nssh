# Terminal Highlighting

Use this reference for highlighting configuration and troubleshooting. The
repository `SPEC.md` owns the durable rendering boundary; current source and
tests own scanner implementation details.

## Supported Behavior

- Interactive SSH output is never highlighted. It remains a raw PTY stream so
  typing, prompts, cursor movement, and terminal resize behavior are preserved.
- Remote-command stdout may be highlighted after complete lines are captured.
  Stderr is not highlighted.
- Existing ANSI, unsupported control bytes, and oversized lines pass through
  unchanged.
- `none` disables highlighting. `junos` is the supported network-oriented
  profile.

## Configuration

Global policy is top-level:

```yaml
highlight:
  enabled: false
  profile: none
```

Provider groups and hosts may override it:

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

Resolution order is global, provider group, then provider host. Configuration
validation rejects unknown profiles and `enabled: true` with `profile: none`.

## Junos Profile

The profile conservatively highlights operator-significant configuration and
command-output tokens, including actions, major hierarchies, routing protocols,
addresses, prefixes, interfaces, routing tables, policy names, URLs, comments,
strings, and operational states.

It intentionally avoids coloring generic words and standalone numbers. Free
text, banners, prompts, and prose should not gain configuration colors merely
because they contain a Junos keyword.

## Troubleshooting

If expected color is absent:

1. Confirm the command is non-interactive and writes the content to stdout.
2. Inspect the resolved host's global, group, and host highlighting policy.
3. Confirm `enabled: true` and `profile: junos`.
4. Check whether the output already contains ANSI or unsafe control data.
5. Reproduce against focused tests before changing scanner categories.

If an interactive session shows injected colors or delayed echo, treat it as a
regression in the raw PTY boundary rather than adding buffering or more token
rules.
