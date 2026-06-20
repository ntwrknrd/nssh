# Credential Provider Contract

This document is the source of truth for nssh credential-provider behavior in
`release-0.3`. It records the contracts already executed in code, the decisions
behind them, and the remaining work. It is not a credential-cache plan.

## Executed Contracts

### Benchmark Contract

- Public benchmarks are real connection benchmarks only:
  - `nssh self bench ssh <host>`
  - `nssh self bench cp ...`
- `nssh self bench credential <host>` is removed, not hidden.
- Provider-only benchmarks must not exist as debug commands, hidden commands, or
  standalone artifacts.
- Credential lookup timing appears only as part of real SSH/CP benchmark runs.
- The benchmark answers one question: where did this nssh connection spend time?

### Provider Naming Contract

- The default Pass provider name is `pass`.
- `pass-local` is gone as a clean cut.
- There is no alias, migration layer, deprecation warning, or compatibility
  fallback for `pass-local`.
- The only supported credential provider config schema is `credential.provider`.
- The old top-level `credentials:` schema is rejected.
- Credential provider include files live under `credential/*.yaml`.
- The old `credentials/` provider include directory is not part of the contract.

### Provider Setup Contract

- Provider setup lives under:
  - `nssh self init`
  - `nssh self init pass`
  - `nssh self init 1password`
  - `nssh self init bitwarden`
- Provider-specific flags such as `nssh self init --pass` are not part of the
  public CLI.
- `nssh self init <provider-type>` creates or updates provider instances.
- Provider setup does not assign credentials to hosts or groups.
- First-run `nssh self init` may still create onboarding inventory and its
  initial credential assignment.

### Inventory Assignment Contract

- `nssh inv set` owns host and group auth assignment.
- Public noninteractive auth flags are:
  - `--auth password|key`
  - `--cred <provider>`
  - `--cred <provider>:<ref>`
  - `--cred none`
- Provider-qualified group targets are supported:
  - `nssh inv set local/default`
  - `nssh inv set netbox-prod/custcbb`
- The old public flags are removed:
  - `--credential-provider`
  - `--password-ref`
  - `--credential-clear`
- `inv auth` is not registered as a public command in the current CLI.
- Group auth prompts reuse the same provider item/manual-ref picker used by host
  credential prompts.

### Status Contract

- `nssh self status` is read-only diagnostics.
- Status may report provider names, provider type, session policy, and readiness.
- Status must not repair config, write probes, print secret refs, or read target
  passwords.
- Pass readiness checks are limited to local readiness state:
  - `pass`
  - `gpg`
  - `gpgconf`
  - GPG secret key presence
  - password store directory
  - `.gpg-id`

### Credential Ref Contract

- Conventional default refs are derived in one shared helper.
- Pass host refs use `nssh/hosts/<host>`.
- Pass group refs use `nssh/groups/<provider>/<group>` where the group target is
  provider-qualified, such as `local/default`.
- 1Password-style and Bitwarden-style host refs use `nssh host <host>`.
- 1Password-style and Bitwarden-style group refs use
  `nssh group <provider>/<group>`.

### Cache Contract

- nssh has no credential cache in this phase.
- No cache design is approved until Pass and Bitwarden behavior are measured
  through normal SSH/CP benchmark runs.
- A future cache, if justified, must not reintroduce a local vault, lock/unlock
  UI, rekey UI, disk cache, or user-managed encryption key workflow.

## Decision Log

- nssh coordinates credential providers; it does not become a password manager.
- No nssh vault.
- No nssh lock/unlock UI.
- No nssh rekey UI.
- No user-managed nssh encryption key workflow.
- No hidden provider-only benchmark.
- No automatic 1Password keepalive loop.
- No silent third-party install.
- No silent GPG key generation.
- No silent `gpg-agent.conf` mutation.
- GPG passphrases belong to `pinentry` and `gpg-agent`, not nssh.
- Provider setup and inventory assignment stay separate.
- Status diagnoses; init configures providers; inv assigns auth.

## Completion State

Completed:

- Removed `nssh self bench credential <host>`.
- Kept benchmark UX scoped to SSH/CP connection timing.
- Renamed the default Pass provider from `pass-local` to `pass`.
- Added `nssh self init [pass|1password|bitwarden]`.
- Made provider setup update provider config without assigning host/group auth.
- Added provider readiness to `nssh self status`.
- Added Pass readiness checks for local Pass/GPG state.
- Added provider-qualified group targets to `nssh inv set`.
- Replaced long `inv set` credential flags with `--auth` and `--cred`.
- Removed stale benchmark and credential CLI help fixtures.
- Updated config examples and nssh skill references for `pass`.
- Centralized default credential ref derivation.
- Renamed the auth patch type to reflect host and group use.
- Changed bare existing-config `nssh self init` to print next commands instead of
  implying a fresh initialization.
- Removed the legacy top-level `credentials:` config alias.
- Renamed credential provider include paths from `credentials/` to
  `credential/`.
- Reused the richer credential picker for group auth prompts.
- Updated the live user config to include `credential/*.yaml` and
  `inventory/*.yaml`.
- Updated live provider files to use `credential.provider`.
- Renamed the live `~/.config/nssh/credentials/` directory to
  `~/.config/nssh/credential/`.
- Removed stale live bootstrap `pass` provider config because Pass is not
  installed.
- Verified the installed dev binary against the live config with
  `nssh self status`, `nssh inv get acm-lab-agg-sw1`, and
  `nssh self bench --help`.

Partially complete:

- Pass readiness checks exist, but write-capable Pass/GPG setup actions do not.
- Bitwarden provider config exists, but local behavior is untested.

Not complete:

- Interactive Pass setup actions:
  - launch GPG key generation after confirmation
  - select a GPG key
  - run `pass init <key-id>` after confirmation
  - optionally tune `gpg-agent` TTLs after confirmation
  - run a disposable provider read/write probe
- Bitwarden setup and behavior testing.
- Provider comparison through normal SSH/CP benchmarks.
- Any credential cache decision.

Deferred:

- Disk credential cache.
- Agent memory credential cache.
- Provider-specific cacheability policy.
- Automatic provider install flows.
- Automatic GPG or `gpg-agent` mutation.

## Remaining Work

### Pass Setup Actions

The Pass setup flow should stay explicit and short:

1. Print detected Pass/GPG state.
2. If no usable GPG secret key exists, ask before launching key generation.
3. If multiple usable keys exist, ask which key should own the password store.
4. If the password store is not initialized, ask before running
   `pass init <key-id>`.
5. Ask before tuning `gpg-agent` TTLs.
6. Ensure the nssh provider named `pass` exists.
7. Run a disposable provider read/write probe.
8. Print next steps for adding host or group credentials.

Suggested GPG agent TTLs, if the user explicitly opts in:

```text
default-cache-ttl 14400
max-cache-ttl 14400
```

If the user declines GPG agent tuning, nssh should continue and report that
prompts may occur according to existing `gpg-agent` policy.

### Bitwarden Testing

Before designing Bitwarden setup or cache behavior, test Bitwarden through the
normal connection benchmark paths:

- cold or locked provider
- immediately warm provider
- idle long enough for provider-native session expiry
- after OS lock or provider lock where practical

Capture:

- connection latency
- credential lookup latency when present
- whether a provider prompt appears
- whether locked/unlocked state is detectable without prompting
- whether provider-native session management is enough

### Provider Comparison

Compare Pass, 1Password, and Bitwarden only through equivalent SSH/CP benchmark
runs. Do not add provider-only benchmark commands.

Expected hypotheses:

- 1Password may benefit from agent memory caching because CLI authorization can
  expire independently from the app lock state.
- Pass may not need nssh credential caching because `gpg-agent` owns passphrase
  caching and TTL policy.
- Bitwarden should not be judged until `bw` is installed and tested.

### Future Cache Boundary

If provider testing proves a cache is needed, the likely safe boundary is:

- agent memory only
- no disk cache
- no nssh-managed encryption keys
- no user-facing vault
- cache entries stored as `*secret.Secret`
- TTL capped by agent lifetime
- auth failure purges the matching entry and retries provider resolution once
- `nssh agent stop` clears cached credentials
- status shows counts and TTLs only, never refs or secret material
