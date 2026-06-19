# Connections, Logs, And Troubleshooting

Audience: agents answering nssh connection, agent, recording, and operational
questions.

## Connection Behavior

Source paths:

- `internal/app/command.go`
- `internal/app/app.go`
- `internal/connect/lookup.go`
- `internal/connect/resolve.go`
- `internal/connect/connect.go`
- `internal/ssh/connector/`
- `internal/ssh/compat/`

`nssh [ssh-options] HOST [command...]` is rewritten by
`internal/app.PreprocessArgs` to hidden `smart-connect`. Use `nssh --select` for
the smart picker and `nssh --target HOST` for literal destinations that collide
with nssh command names or should bypass fuzzy resolution.

Smart lookup behavior:

1. Check the nssh host catalog for an exact host.
2. If one partial match exists, use it.
3. If multiple partial matches exist, open `fzf` selection.
4. If lookup misses, offer local inventory creation with `nssh inv set`.

Smart lookup does not refresh external inventory providers automatically on a
miss; use `nssh inv refresh` when provider caches need to be updated.

Username precedence in `internal/connect.ResolveHostForConnect`:

1. Explicit `user@host` or `-l user`.
2. Inventory host auth `username`.
3. Inventory provider group auth `username`.
4. Credential item username.

OpenSSH owns transport. nssh starts OpenSSH inside a PTY and injects a resolved
password only after prompt detection.

## Agent Runtime

Commands:

```bash
nssh agent status
nssh agent stop
nssh agent restart
nssh agent doctor
```

The agent is a Unix socket daemon used for provider-session requests and
background archive maintenance. It is not a password cache and does not lock or
unlock external password managers.

Relevant config:

```yaml
agent:
  auto_start: true
  idle_timeout: 1h
  activity_increment: 15m
  max_lifetime: 24h
```

If a `session = "agent"` provider cannot connect to the agent and
`agent.auto_start` is true, nssh starts the runtime automatically.

## Host Keys And Legacy SSH

Host-key behavior is configured under `ssh.security`:

```yaml
ssh:
  security:
    accept_once_mode: pin
    host_key_policy: pin
    compat_persist_probes: false
```

`accept_once_mode = "pin"` uses a stricter accept-once flow. `accept-new` uses
OpenSSH trust-on-first-use behavior.

On legacy SSH algorithm failures, `internal/connect.handleCompatibilityFixes`
maps stderr through `internal/ssh/compat`, selects supported algorithm floors,
and can persist them under the owning provider YAML host `ssh.compatibility`
field.

## Recordings And Logs

Commands:

```bash
nssh log list
nssh log search <pattern>
nssh log play <id>
nssh log export <id>
nssh log upload <id>
nssh log delete <id>
nssh log auth
```

Recording config lives under `logging.session`. Recording is disabled by
default. When enabled, nssh wraps the outer command with asciinema and guards the
inner connection with `NSSH_RECORDING_INNER=1`.

Recording files default to `~/.local/state/nssh/casts`. Archive maintenance is
configured under `logging.session.archive` and executed by the agent runtime.

Audit logging is separate from session recording and lives under
`logging.audit`.

## Diagnostics

Use these first:

```bash
nssh self status
nssh inv status
nssh inv refresh local
nssh agent status
nssh agent doctor
nssh -v <command>
NSSH_DEBUG=1 nssh <host>
```

For performance questions, benchmark the actual host:

```bash
nssh self bench ssh <host>
nssh self bench scp <host>
```

Benchmark artifacts are written under `~/.local/share/nssh/benchmarks/`.

For provider credential failures, separate these causes:

- Auth mapping missing or points at the wrong provider/ref.
- Provider CLI not logged in or locked.
- Provider item exists but username does not match selected SSH username.
- Inventory host is in a provider-owned group without password auth and
  defaults to key mode.
- External inventory cache is stale; run `nssh inv refresh`.
