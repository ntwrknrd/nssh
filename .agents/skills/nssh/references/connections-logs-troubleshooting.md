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
Unix socket. Credential resolution is represented lazily, but a cold managed
password connection starts provider lookup concurrently with OpenSSH setup. A
hot multiplexed session skips that prefetch and does not need an askpass server
or provider lookup.

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
key before using the target credential; a managed password-backed proxy may use
its own credential to reach the target during that scan. New-key prompts offer
reject, accept once, or accept always. Changed-key prompts label both acceptance
choices dangerous; accept-always removes the stale entry and writes the verified
replacement only after explicit confirmation.

On legacy SSH algorithm failures, `internal/connect.handleCompatibilityFixes`
maps stderr through `internal/ssh/compat`, selects supported algorithm floors,
and can persist them under the owning provider YAML host `ssh.compatibility`
field.

## Recordings And Logs

Commands:

```bash
nssh log list
nssh log search <pattern>
nssh log play
nssh log export
nssh log upload
nssh log delete
nssh log auth
nssh log archive
```

`play`, `export`, and `upload` open an interactive recording picker; they do not
accept a recording ID argument. Bare `delete` opens a multi-select picker. For
non-interactive deletion, use `delete --select <pattern>` or
`delete --older-than <days>`, with `--yes` where confirmation must be skipped.
Use the generated help for the complete flags.

Recording config lives under `logging.session`. Recording is disabled by
default. When enabled, nssh wraps the outer command with asciinema and guards the
inner connection with `NSSH_RECORDING_INNER=1`.

Runtime recording defaults are append mode, a two-second playback idle limit,
and title `nssh:{host}`. Configuration can be overridden with
`NSSH_RECORD`, `NSSH_RECORD_DIR`, `NSSH_RECORD_IDLE_TIME_LIMIT`,
`NSSH_RECORD_IDLE_TIME_LIMIT_MODE`, and `NSSH_RECORD_TITLE_FORMAT`.

Recording files default to `~/.local/state/nssh/casts`. Archive maintenance is
configured under `logging.session.archive` and runs only when an operator invokes
`nssh log archive`. Use cron, launchd, or systemd timers for automation.

Audit logging is separate from session recording and lives under
`logging.audit`. It is enabled by default, writes
`$XDG_STATE_HOME/nssh/audit.log` with mode `0600`, and defaults to a 10 MB
rotation threshold with three rotated files. Connection audit events include
the host, SSH arguments, remote command text when present, outcome, and error or
exit-code details. They must not contain credential secrets, but command text
and arguments can still be operationally sensitive.

## Self Lifecycle And Data Preservation

`nssh self reset` is destructive. It stops the runtime agent, then recursively
removes the entire nssh XDG config, data, and state directories, including
provider cache, audit history, archives, and recordings. Use `--dry-run` first;
`--force` skips the required `DESTROY` confirmation. A canceled reset exits 2.

`nssh self uninstall` removes the installed binary, an adjacent
`nssh-askpass` helper, and nssh-installed optional dependencies. Without
preservation flags it also removes `config.yaml` and the recording directory,
then removes XDG directories only when empty.
`--keep-config` skips removal of `config.yaml` and the empty-directory cleanup
pass, but it does not imply `--keep-recordings`; use both flags to preserve both
surfaces.
Use `--dry-run` before migration or rollback-sensitive work.

Shell completion entrypoints are unsupported in 0.3. Remove stale generated
completion files and shell startup references only after verifying the new CLI.

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

The standard data summary is:

- config and provider includes: `$XDG_CONFIG_HOME/nssh/`
- non-secret external inventory cache: `$XDG_STATE_HOME/nssh/inventory/providers/`
- recordings and indexes: `$XDG_STATE_HOME/nssh/casts/`
- recording archives: `$XDG_STATE_HOME/nssh/archives/`
- audit history: `$XDG_STATE_HOME/nssh/audit.log*`
- benchmark artifacts: `$XDG_DATA_HOME/nssh/benchmarks/`

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
