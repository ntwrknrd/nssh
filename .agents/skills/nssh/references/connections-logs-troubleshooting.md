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

OpenSSH owns transport. Interactive connections still use a PTY, but 0.3 sends
resolved passwords through `nssh-askpass` over an authenticated, request-scoped
Unix socket. Credential lookup remains demand-driven; a hot multiplexed session
does not need an askpass server or provider lookup.

For a single `ProxyJump` that resolves to an nssh inventory host, nssh renders a
managed proxy command and maintains separate target and proxy askpass channels.
Password autofill is disabled for arbitrary `ProxyCommand`, unresolved
`ProxyJump`, comma-separated jumps, and nested proxy chains.

## Agent Runtime

Commands:

```bash
nssh agent status
nssh agent stop
nssh agent reset
```

The agent is a Unix socket daemon used for credential-provider requests. It must
not own recording archive maintenance or general background jobs.

Relevant config:

```yaml
agent:
  auto_start: true
  idle_timeout: 1h
  activity_increment: 15m
  max_lifetime: 24h
  provider_request_timeout: 2m
```

Credential lookups run directly in the foreground unless the provider config
requires retained access. If a retained-access provider needs the agent and
`agent.auto_start` is true, nssh starts it automatically. `nssh agent reset`
stops any running daemon and clears retained access state; it does not start a
new daemon. Provider requests are bounded by `agent.provider_request_timeout`.

`nssh agent status` reports retained provider access state, not configured
provider inventory. SOPS+age should not appear there. 1Password appears only
when keepalive is configured; Bitwarden appears only when `warm_session` is
configured. Status must not expose refs, provider stdout/stderr, usernames,
passwords, or `BW_SESSION`.

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
OpenSSH trust-on-first-use behavior. Password-backed setup scans the presented
key before credential use. New-key prompts offer reject, accept once, or accept
always. Changed-key prompts label both acceptance choices dangerous;
accept-always removes the stale entry and writes the verified replacement only
after explicit confirmation.

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
nssh log archive
```

Recording config lives under `logging.session`. Recording is disabled by
default. When enabled, nssh wraps the outer command with asciinema and guards the
inner connection with `NSSH_RECORDING_INNER=1`.

Recording files default to `~/.local/state/nssh/casts`. Archive maintenance is
configured under `logging.session.archive` and runs only when an operator invokes
`nssh log archive`. Use cron, launchd, or systemd timers for automation.

Audit logging is separate from session recording and lives under
`logging.audit`.

## Diagnostics

Use these first:

```bash
nssh self status
nssh inv status
nssh inv refresh local
nssh agent status
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
- `nssh-askpass` is not installed beside the `nssh` binary.
- A password-backed target uses an unmanaged or nested SSH proxy; only a
  single-hop inventory-resolved proxy receives nssh password autofill.
