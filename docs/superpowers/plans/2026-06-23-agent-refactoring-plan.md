# Agent Refactoring Plan

## Goal

Refocus the nssh agent on credential-provider runtime concerns and remove
unrelated background maintenance work from the agent process.

The first refactor is recording maintenance. Recording archival/pruning should be
an explicit `nssh log archive` command, not an agent background loop or hidden
automatic cleanup.

## Contract

The agent is a credential-provider access runtime. Anything outside this
contract is a breach.

The agent may do only these things:

- Hold opt-in Bitwarden `BW_SESSION` provider access material across nssh
  invocations.
- Run provider-specific authentication refresh or keepalive when explicitly
  configured.
- Broker a single credential-provider request and return one resolved credential
  response.
- Expose minimal runtime status for provider access state.
- Stop/reset itself and clear retained provider access material.

The agent must not do these things:

- Own recording archival or pruning.
- Retain resolved SSH passwords after a credential request completes.
- Retain decrypted provider payloads after a credential request completes.
- Cache decrypted SOPS documents.
- Cache 1Password item JSON, field values, or resolved passwords.
- Cache Bitwarden item JSON, field values, or resolved passwords.
- Write provider access material or credential values to disk.
- Log provider access material or credential values.
- Export provider access material into the user's shell.
- Persist provider access material or credential values anywhere outside agent
  process memory.
- Own SSH connection state, SSH process state, SSH key-agent behavior,
  inventory state, recording state, benchmark state, or log maintenance.
- Grow into a general background task runner.

Allowed retained state:

- Provider registry metadata: provider name, type, account, vault, file path, and
  other non-secret provider configuration.
- Bitwarden `BW_SESSION`, only when warm session is enabled on that Bitwarden
  provider instance. It must be held only in agent memory, passed only to `bw`
  child process environment, and cleared on agent stop, reset, idle expiry, and
  max-lifetime expiry.
- Operational timers and counters needed for agent idle/lifetime management.

Disallowed retained state:

- SSH passwords.
- Usernames resolved from provider secret fields, unless they already exist as
  literal non-secret inventory config.
- Decrypted SOPS document objects.
- 1Password item payloads.
- Bitwarden item payloads.
- Provider command stdout/stderr containing secret material.
- Any provider access token other than opt-in Bitwarden `BW_SESSION`.

Per-provider rules:

- SOPS+age: decrypt per credential request, extract only requested scalar values,
  return one response, then release the decrypted document before the request
  returns. No document cache.
- 1Password: call `op` per credential request. Do not retain item payloads,
  field values, resolved passwords, or provider access tokens. Optional
  keepalive may only run non-secret `op` operations and must be explicitly
  enabled on that 1Password provider instance.
- Bitwarden: retain `BW_SESSION` only when explicitly enabled on that Bitwarden
  provider instance. Call `bw` per credential request. Do not retain item
  payloads, field values, or resolved passwords.

Secret handling rules:

- Provider stdout that may contain secrets is parsed inside the request scope.
- The returned secret is copied into a `secret.Secret` only in the foreground
  nssh process for injection.
- The connector destroys that `secret.Secret` after injection or cleanup.
- Tests must cover that repeated provider requests re-read provider data rather
  than returning cached resolved credentials.
- Tests must cover that retained Bitwarden warm access never includes item JSON,
  field values, or resolved credentials.
- Tests must cover that `nssh agent status` reveals whether Bitwarden warm access
  is active without exposing the `BW_SESSION` value.

## Part 1: Move Recording Maintenance Out Of The Agent

### Current Behavior

The agent currently starts a recording archive loop when archive config is
enabled.

Current wiring:

- `internal/app/agent_mode.go` passes `cfg.Logging.Session.Archive` into the
  agent runtime config.
- `internal/agent/daemon.go` creates `recording.NewArchiveRunner(...)`.
- `internal/agent/daemon.go` starts `archiver.RunLoop(...)`.
- `internal/recording/archiver.go` archives old recordings, deletes archived
  source files, and prunes old archive bundles.

This works, but it couples credential-agent lifetime to recording maintenance.

### Target Behavior

Recording maintenance has one public command:

- `nssh log archive` runs recording archive maintenance once on explicit operator
  request.

nssh does not schedule archive maintenance internally. Users who want automation
should run `nssh log archive` from cron, launchd, systemd timers, or their
preferred scheduler.

No `nssh` command should run archive implicitly.

### Archive Configuration

Phase 1 should also clean-cut the archive configuration because the current
shape still reflects an agent-owned daily loop.

Keep archive settings under `logging.session.archive` because archival belongs
to session recording, not the credential agent.

Final public shape:

```yaml
logging:
  session:
    archive:
      dir: ~/.local/state/nssh/archives
      min_age: 720h
      max_bundles: 12
      max_run_bytes: 0
      timeout: 30s
```

Keep these options:

- `dir`: archive bundle output directory.
- `min_age`: minimum recording age before archiving.
- `max_bundles`: monthly archive bundle retention count.
- `max_run_bytes`: per-run byte cap, where `0` means unlimited.
- `timeout`: maximum time an archive maintenance pass may run before it is
  canceled.

Remove these options:

- `enabled`: obsolete once nssh no longer owns scheduling.
- `jitter`: obsolete once there is no daily agent loop.
- `min_interval`: obsolete once nssh no longer owns scheduling.

Do not add these options:

- `schedule`.
- `interval`.
- `opportunistic`.

Do not expose these as config:

- Lock path.
- Trigger list.
- Background interval.
- Agent ownership.

The lock path is an internal implementation detail derived from the nssh state
directory.

### Archive Command

Add a public command:

```shell
nssh log archive
```

Manual archive must:

- Run synchronously.
- Print a short summary: files archived, bytes archived, bundles pruned, and any
  skipped reason.
- Return a real error when archive maintenance fails.
- Use the configured archive paths and limits.
- Use the configured `timeout` so an explicit archive cannot run indefinitely.
- Use a non-blocking lock so concurrent manual or scheduled archive runs do not
  overlap.

### Maintenance Rules

Archive maintenance must:

- Never prompt.
- Use a lock file so only one maintenance pass runs at a time.
- Use the configured `timeout`.
- Reuse the existing archive implementation, but split manual execution from
  scheduler enablement. `nssh log archive` must run whenever archive paths are
  valid; it must not depend on the removed `logging.session.archive.enabled`
  field.
- Respect existing archive limits such as `max_run_bytes`.

Recommended defaults:

- `timeout`: `30s`.
- Lock path: nssh state directory, under a recording-maintenance-specific name.

### Implementation Shape

Add a small recording archive helper, likely in `internal/recording`:

```go
type ArchiveMaintenanceConfig struct {
    Archive       config.SessionArchiveConfig
    RecordingDir  string
    StateDir      string
}

func RunArchive(ctx context.Context, cfg ArchiveMaintenanceConfig, logger *slog.Logger) (ArchiveSummary, error)
```

The helper should:

- Acquire a non-blocking lock.
- Run one archive pass with a timeout.
- Return errors to callers so `nssh log archive` can report failures and tests
  can assert behavior.
- Return a clear skipped summary when another archive run already holds the lock.

Implementation detail:

- Remove or narrow the current `ArchiveRunner.Enabled()` gate so it only applies
  to deleted scheduler behavior. Manual `RunArchive` must validate
  `SourceDir`/`ArchiveDir` directly and run one pass without an `enabled` flag.

### Call Sites

Add the public command in the existing log command tree:

- `internal/cli/log`

Do not schedule archive from:

- Recorded-session completion.
- Other `nssh log ...` commands.
- Credential lookup.
- SSH connector internals.
- Agent status.

### Scheduling Guidance

Document that automated archiving belongs to the user's scheduler, not nssh.
Examples should be concise and platform-specific, such as:

```cron
0 3 * * * /Users/cj/.local/bin/nssh log archive
```

Do not add scheduler management commands or config.

### Agent Cleanup

Remove recording maintenance from:

- `internal/app/agent_mode.go`
- `internal/agent/daemon.go`
- `internal/agent` runtime config

After this phase, the agent should have no import path dependency on
`internal/recording`, no `Archive` field in `agent.RuntimeConfig`, and no
`RecordingDir` field in `agent.RuntimeConfig`.

### Tests

Add focused tests for:

- Config validation accepts `logging.session.archive.timeout`.
- Config writing emits `timeout` when it differs from defaults.
- Config no longer includes or writes `logging.session.archive.enabled`.
- Config no longer includes or writes `logging.session.archive.jitter`.
- Config no longer includes or writes `logging.session.archive.min_interval`.
- Maintenance uses a lock to prevent concurrent runs.
- Maintenance uses configured `timeout`.
- Manual archive runs without any `logging.session.archive.enabled` gate.
- `nssh log archive` runs synchronously, prints a summary, and returns real
  failures.
- `nssh log archive --opportunistic` does not exist.
- Recorded-session completion does not launch archive maintenance.
- Other `nssh log ...` commands do not run archive implicitly.
- Command help or docs mention external schedulers for automation.
- Agent package no longer imports `internal/recording` or carries archive config.

Verification commands:

```shell
go test ./internal/recording ./internal/connect ./internal/cli/log ./internal/agent
go test ./...
```

## Phased Credential Agent Refactor

Phase 1 removes unrelated recording maintenance. The remaining phases tighten
the credential-agent boundary.

### Phase 2: Request-Scoped Credential Results

Goal: prove and enforce that resolved credentials and provider payloads live
only for one credential request, without changing SOPS document-decryption
behavior yet.

Provider requests must also have a bounded lifetime. The agent must not call
credential providers with `context.Background()` from the socket request path.
Each brokered provider request should use a request-scoped context with a
configured timeout so a stuck `op`, `bw`, or `sops` child process cannot outlive
the credential request indefinitely.

Provider request timeout is one agent-global setting:

```yaml
agent:
  provider_request_timeout: 2m
```

Rules:

- `provider_request_timeout` applies to every brokered credential-provider
  request, including `get` and any provider-auth request.
- The timeout bounds provider child commands launched by the agent, such as
  `op`, `bw`, and `sops`.
- Default is `2m`.
- Valid range is `5s` through `10m`.
- Zero or omitted means the default, not disabled.
- This is intentionally not per provider unless real provider behavior proves
  the global value is wrong.

This phase owns the generic credential-request boundary: no resolved SSH
passwords, resolved usernames, 1Password item payloads, Bitwarden item payloads,
provider stdout, or provider stderr may be retained after a request returns.
SOPS decrypted-document retention is handled in Phase 3 because the current SOPS
implementation has a provider-level document cache that needs a focused
refactor.

Steps:

1. Add tests around the agent-brokered credential path proving that two
   identical credential requests invoke the provider twice.
2. Add tests proving the agent does not retain resolved passwords, username
   values read from provider secrets, 1Password item JSON, Bitwarden item JSON,
   provider stdout, or provider stderr between requests.
3. Add tests proving a timed-out provider command returns a sanitized failure and
   does not leave a provider child running indefinitely.
4. Add `ProviderRequestTimeout config.Duration` as
   `provider_request_timeout` on `config.AgentConfig`.
5. Validate `provider_request_timeout` with default `2m`, minimum `5s`, and
   maximum `10m`.
6. Inspect `internal/agent/provider_cache.go`,
   `internal/agent/provider.go`, `internal/credential/onepassword.go`,
   `internal/credential/bitwarden.go`, and `internal/credential/sops_age.go`.
7. Replace the agent daemon's provider-request `context.Background()` call with
   a request-scoped timeout derived from `agent.provider_request_timeout`.
8. Keep provider registry/config objects cached only when they contain
   non-secret metadata.
9. Remove any cache entry whose value is a resolved credential, provider stdout,
   1Password item payload, or Bitwarden item payload.
10. Keep `secret.Secret` ownership in the foreground nssh process and connector,
   not in the agent.

Verification:

```shell
go test ./internal/agent ./internal/credential
```

### Phase 3: SOPS+age Request-Scoped Decrypt

Goal: SOPS+age decrypts only during a credential request and releases the
decrypted document before returning.

This phase owns removal of the SOPS document cache. The runtime may keep only
non-secret SOPS provider metadata, such as provider name, encrypted file path,
and optional age key file path. It must not keep a `sopsdoc.Document`, decrypted
YAML/JSON, provider stdout, extracted field values, resolved usernames, or
resolved passwords after the request returns.

Steps:

1. Update SOPS+age tests to expect one decrypt operation per credential request,
   not one decrypt per provider instance or agent session.
2. Remove the provider-level `sopsdoc.Document` cache from the agent runtime.
3. Refactor the SOPS+age provider so `GetRef`, `GetHost`, and `GetGroup` decrypt,
   extract only the requested password and optional username scalar values, and
   drop the parsed document before returning.
4. Keep configured SOPS file path, optional age key file, and provider name as
   non-secret provider metadata.
5. Ensure provider stdout containing decrypted document content is never stored
   on the provider struct, agent state, logs, or disk.
6. Add tests proving no decrypted SOPS document object or extracted SOPS scalar
   survives across provider requests.

Verification:

```shell
go test ./internal/credential ./internal/agent
```

### Phase 4: Bitwarden Warm Access Boundary

Goal: keep Bitwarden warm auth without caching Bitwarden item payloads or
resolved credentials.

Provider config shape:

```yaml
credential:
  provider:
    bw-work:
      type: bitwarden
      warm_session: true
```

Steps:

1. Add `WarmSession bool` as `warm_session` on credential provider config and
   default it to `false`.
2. Reject `warm_session` on non-Bitwarden provider types.
3. For `warm_session: false`, foreground nssh runs `bw unlock --raw` only when a
   Bitwarden credential lookup needs it, passes the returned `BW_SESSION` on
   that single `get` provider request, and the agent uses it only for that one
   `bw get item` child process. The agent must not store the token.
4. For `warm_session: true`, foreground nssh may send `BW_SESSION` to the agent
   as warm access material, and the agent may retain it in memory for later
   Bitwarden requests.
5. Keep `BW_SESSION` as the only Bitwarden secret allowed to survive between
   requests, and only when `warm_session: true`.
6. Store retained `BW_SESSION` only in agent memory.
7. Pass `BW_SESSION` only to `bw` child process environment.
8. Clear retained `BW_SESSION` on `nssh agent stop`, reset, idle expiry, and
   max-lifetime expiry.
9. Add status output showing whether each Bitwarden provider has warm access
   active without exposing the token.
10. Add tests proving Bitwarden item JSON and resolved passwords are not retained
   after a request.
11. Add tests proving `warm_session: false` does not store `BW_SESSION` in the
    agent after a successful request-scoped lookup.
12. Add tests proving `warm_session: true` is the only path that stores
    `BW_SESSION` in the agent.
13. Manually validate Bitwarden unlock once Bitwarden is configured locally.

Verification:

```shell
go test ./internal/agent ./internal/credential
```

Manual check:

```shell
nssh self init --cred bitwarden
nssh self bench ssh <bitwarden-backed-host>
```

### Phase 5: 1Password Keepalive

Goal: implement explicit per-provider 1Password keepalive without reading item
payloads, retaining provider tokens, or prompting unexpectedly before the user has
already authorized a real credential lookup.

Rules:

- 1Password keepalive must be explicit provider config and disabled by default.
- It may run only `op whoami`, with `--account` when the provider config includes
  an account.
- It must never read, store, log, or cache item payloads or field values.
- It must stop when the agent stops.
- It must not persist anything to disk.
- It must not start merely because the agent started.
- It must arm only after a successful user-initiated credential request for that
  provider.
- It must suspend after failure or timeout, and may re-arm only after the next
  successful user-initiated credential request.

Provider config shape:

```yaml
credential:
  provider:
    op-expedient:
      type: 1password
      vault: Network
      keepalive: true
      keepalive_interval: 5m
```

Steps:

1. Measure whether `op whoami` refreshes the same 1Password CLI authorization
   window used by `op read` during normal nssh credential lookup. If it does not,
   stop this phase and do not add keepalive config.
2. Add `Keepalive bool` as `keepalive` and `KeepaliveInterval config.Duration`
   as `keepalive_interval` on credential provider config.
3. Default `keepalive` to `false`.
4. Reject `keepalive` and `keepalive_interval` on non-1Password provider types.
5. Default `keepalive_interval` to `5m`.
6. Enforce bounded keepalive intervals so the agent cannot run a busy loop. The
   allowed range is `1m` through `9m`; values above `9m` are rejected because
   1Password CLI authorization expires after 10 minutes of inactivity.
7. Add a per-provider keepalive state machine:
   - `disabled`: config does not enable keepalive.
   - `idle`: keepalive enabled, but no successful credential lookup has armed it.
   - `active`: successful credential lookup armed periodic refresh.
   - `suspended`: refresh failed or timed out; wait for the next successful
     credential lookup before trying again.
8. After `handleOnePasswordGet` returns a found credential successfully, arm that
   provider's keepalive state.
9. Run keepalive ticks in the agent with context tied to agent lifetime.
10. On each tick, run only:

   ```shell
   op whoami --account <account>
   ```

   If the provider has no configured account, run:

   ```shell
   op whoami
   ```

11. Use a short per-tick timeout, default `10s`, so a stuck `op` process cannot
    run indefinitely.
12. On keepalive failure or timeout, record sanitized error state, suspend that
    provider's keepalive, and do not retry until the next successful credential
    request.
13. Do not store any 1Password access token, item JSON, field value, or resolved
   credential.
14. Show keepalive state in `nssh agent status` without exposing refs, item names,
    field values, stdout, or stderr.

Implementation notes:

- Add `Keepalive`, `KeepaliveInterval`, and `KeepaliveTimeout` fields to
  `config.CredentialProviderConfig` and `config.CredentialProviderDetailConfig`.
- Add matching fields to `agent.OnePasswordProviderConfig`.
- Keepalive state belongs in `internal/agent`, not `internal/credential`.
- The existing foreground provider still performs normal `get` requests through
  the agent. The keepalive path must never return credential data to callers.
- The initial `op` authorization remains user-driven by a normal credential
  lookup. Keepalive only tries to refresh that authorization before it goes idle.

Verification:

```shell
go test ./internal/agent ./internal/credential
nssh self bench ssh <1password-backed-host>
```

### Phase 6: Agent CLI Cleanup

Goal: keep the public agent CLI small, and make `nssh agent status` the complete
read-only monitoring surface for everything the agent owns.

Public commands:

- `nssh agent status`: inspect agent runtime, provider access state, timers,
  process health, and resource usage.
- `nssh agent stop`: stop the agent and clear retained provider access material.
- `nssh agent reset`: stop any running agent and clear retained provider access
  material without starting a new daemon.

Do not add back split-purpose agent commands such as `doctor`, `auth`, `debug`,
or provider-specific subcommands unless a concrete operator workflow proves
`status` cannot cover it.

`nssh agent status` must include:

- Agent state: active/inactive, protocol version, PID, socket path, and peer
  verification mode.
- Process state: expected process count, detected process count, and duplicate
  process warning when more than one runtime agent appears active.
- Resource state: resident memory, heap allocation, goroutine count, and open file
  descriptor count when available on the platform.
- Lifecycle timers: uptime, idle shutdown deadline, max lifetime deadline, idle
  timeout, max lifetime, and stop reason when known.
- Agent-managed access inventory only: provider name, provider type, enabled
  runtime functions, and non-secret config relevant to retained access state.
  Do not list providers that have no retained agent access state.
- Bitwarden state: whether `warm_session` is configured, whether warm access is
  active, last sanitized error, and time since last successful auth if known.
- 1Password state: whether keepalive is configured, keepalive state (`disabled`,
  `idle`, `active`, `suspended`), interval, last successful tick, next scheduled
  tick, and last sanitized error.
- SOPS+age must not appear in agent access status because the agent has no warm
  SOPS access state after request-scoped decrypt is implemented. Tests must
  enforce that no decrypted SOPS document is retained.
- Broker counters: total provider requests, per-provider request counts, last
  success time, and last sanitized failure time.

Default output must stay compact and operational. It answers: is the agent
active, how long will it stay alive, which provider access is warm, and is
anything unhealthy?

```text
──────────────────────────── AGENT ────────────────────────────

  [✓] Agent: active (pid 48291, 38 MB)
  [-] Lifetime: idle 3h42m, max 21h46m
  [✓] Access: op-expedient keepalive, bw-work warm session
  [✓] Health: 1 process, 17 requests, 0 failures

────────────────────────────── OK ──────────────────────────────
```

If there is a warning:

```text
──────────────────────────── AGENT ────────────────────────────

  [✓] Agent: active (pid 48291, 41 MB)
  [-] Lifetime: idle 3h12m, max 20h04m
  [!] Access: op-expedient keepalive suspended, bw-work warm session
  [!] Health: 2 processes, 42 requests, 3 failures

──────────────────────────── WARNING ───────────────────────────
```

Verbose output may expand into sections, but must still stay short:

```text
──────────────────────────── AGENT ────────────────────────────

  ─── Runtime
  [✓] Agent: active
  [-] PID: 48291
  [-] Socket: ~/.local/state/nssh/agent.sock
  [-] Uptime: 2h14m
  [-] Idle shutdown in: 3h42m
  [-] Max lifetime ends in: 21h46m

  ─── Access
  [✓] op-expedient: 1Password keepalive active (next 4m12s)
  [✓] bw-work: Bitwarden warm session active

  ─── Health
  [✓] Processes: 1
  [-] Memory: 38 MB RSS, 10 MB heap
  [-] Activity: 17 requests, 0 failures

────────────────────────────── OK ──────────────────────────────
```

Use tables only when the agent-managed access entry count makes single-line
access status hard to scan. Never use tables in the default output for a small
access set.

Steps:

1. Remove any public agent commands outside `status`, `stop`, and `reset`.
2. Keep `status` read-only. It must never repair, authenticate, unlock, refresh,
   prune, or mutate state.
3. Extend the agent status protocol so the agent reports its own runtime state.
4. Gather self process metrics inside the agent process where possible. Use
   platform-specific helpers for RSS/open-FD counts, and fall back to `unknown`
   rather than shelling out from the agent.
5. Let the CLI perform a separate best-effort process scan only for duplicate
   agent detection. This scan must not decide credential state.
6. Replace configured-provider counts/names with agent-managed access state.
7. Show whether Bitwarden warm access is active per provider without exposing
   `BW_SESSION`.
8. Show whether 1Password keepalive is enabled per provider without exposing item
   refs, payloads, stdout, stderr, or field values.
9. Do not show SOPS+age in agent access status.
10. Sanitize all status errors. Status may show error class and age, but not raw
   provider output.
11. Add tests proving status does not include secrets, refs that identify secret
   items, provider stdout/stderr, `BW_SESSION`, resolved usernames, or passwords.
12. Add tests for duplicate-process reporting and resource metric fallbacks.

Verification:

```shell
go test ./internal/cli/agent ./internal/agent
nssh agent status
```

### Phase 7: Contract Verification And Reference Cleanup

Goal: close out the credential solution contract with full tests, manual checks,
and stale reference cleanup.

Steps:

1. Run full repository tests after all agent refactor phases are complete.
2. Reinstall the development binary.
3. Manually validate a real SOPS+age credential lookup through a normal SSH
   benchmark.
4. Manually validate Bitwarden warm auth with `warm_session: true` through a
   normal SSH benchmark.
5. Manually validate 1Password keepalive with `keepalive: true` through a normal
   SSH benchmark.
6. Search project docs, examples, help fixtures, and skill/reference material for
   stale Pass credential-provider guidance.
7. Remove stale Pass guidance. Historical mentions are allowed only when they
   explicitly describe removed behavior.
8. As the final step, update the credential solution contract's completed and
   remaining-work sections so the contract reflects verified reality.

Verification:

```shell
go test ./...
nssh self reinstall --dev
nssh self bench ssh <sops-backed-host>
nssh self bench ssh <bitwarden-backed-host>
nssh self bench ssh <1password-backed-host>
rg -n "pass|Pass|password-store|GPG|gpg" docs internal .agents
```

## Contract Coverage

The credential solution contract's remaining work maps to this plan as follows:

1. Request-scoped credential/provider payload tests: Phase 2.
2. SOPS decrypted-document retention: Phase 3.
3. Bitwarden `warm_session` opt-in: Phase 4.
4. 1Password `keepalive` opt-in: Phase 5.
5. Agent CLI and provider access status output: Phase 6.
6. Full repository tests and reinstall checks: Phase 7.
7. Real SOPS+age SSH benchmark validation: Phase 7.
8. Bitwarden warm-auth manual validation: Phase 4 and Phase 7.
9. 1Password keepalive manual validation: Phase 5 and Phase 7.
10. Pass reference cleanup: Phase 7.
