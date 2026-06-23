# Credential Solution Contract

This document is the source of truth for the `release-0.3` credential-provider
direction. It records what is already complete, what the final credential
solution must be, and what still needs to change.

## Target Model

nssh supports credential backends, not a credential product.

The target provider set is:

- `sops-age`: default suggested provider.
- `1password`: external provider backed by the `op` CLI.
- `bitwarden`: external provider backed by the `bw` CLI.

SOPS+age is the default suggestion because it gives nssh a portable encrypted
credential document without requiring nssh to invent a vault format, key
rotation model, editor, or lock/unlock product. SOPS supports structured YAML and
JSON secret files, age recipients, `.sops.yaml` creation rules, default age key
lookup paths, and explicit key-file overrides.

1Password and Bitwarden remain supported external credential providers. nssh
should adapt to their references and authentication models, but it should not
try to repair their product-specific authentication limitations with local
password caches.

## Credential Solution Contract

The solution must:

- Keep inventory and credentials separate.
- Store host/group auth assignment in inventory config.
- Store no password values in nssh config.
- Resolve credentials through provider refs selected by inventory auth.
- Make SOPS+age the default suggested provider for new installs.
- Let the operator choose the SOPS file path.
- Let the operator choose the age identity path or rely on SOPS defaults.
- Let the operator bring an existing SOPS file.
- Offer to create a starter SOPS+age file only after confirmation.
- Treat the SOPS document as provider-owned storage, not an nssh vault product.
- Use stable key-path refs into the SOPS document.
- Keep 1Password and Bitwarden refs provider-native.
- Keep password injection prompt-driven.
- Keep credential timing inside normal SSH/CP benchmark runs.
- Keep `nssh self init` focused on provider setup and readiness.
- Keep `nssh inv set` responsible for assigning auth to hosts and groups.
- Keep `nssh self status` read-only.
- Clear any warm provider authentication material when `nssh agent stop` runs.
- Keep resolved credential values request-scoped.
- Keep decrypted provider payloads request-scoped.
- Let Bitwarden warm sessions be enabled only by explicit provider config.
- Let 1Password keepalive be enabled only by explicit provider config.
- Keep any 1Password keepalive limited to `op whoami`, with no item reads.
- Make `nssh agent status` reveal Bitwarden warm-access state without exposing
  session details.

The solution must not:

- Reintroduce the old nssh vault UX.
- Add a separate nssh lock/unlock/rekey product surface.
- Invent a custom encrypted credential file format when SOPS already provides
  structured encrypted files.
- Store provider authentication tokens in config files.
- Export provider authentication tokens into the user's shell.
- Add a disk credential cache.
- Persist provider access material or credential values anywhere outside agent
  process memory.
- Cache decrypted SOPS documents in the agent.
- Cache 1Password item payloads, field values, or resolved passwords.
- Cache Bitwarden item payloads, field values, or resolved passwords.
- Persist `BW_SESSION` or any provider access material to disk.
- Retain any provider access token except opt-in Bitwarden `BW_SESSION`.
- Add hidden provider-only benchmark commands.
- Add an implicit or default 1Password keepalive loop.
- Add Pass or GPG-agent specific warm-session workarounds.
- Require Bitwarden Lite or any server for the default suggested flow.
- Make macOS Keychain the only workstation storage answer.
- Hide provider setup inside `self status`.

## Provider Contracts

### SOPS+age

SOPS+age is the default suggested provider.

Configuration shape:

```yaml
credential:
  provider:
    sops:
      type: sops-age
      config:
        file: ~/.local/share/nssh/credentials.sops.yaml
        age_key_file: ~/.config/sops/age/keys.txt
```

`age_key_file` is optional. If unset, SOPS default identity discovery applies.
On macOS, SOPS falls back to
`~/Library/Application Support/sops/age/keys.txt` when `XDG_CONFIG_HOME` is not
set. On Linux, it falls back to `~/.config/sops/age/keys.txt`. Operators can
also use SOPS-supported environment overrides such as `SOPS_AGE_KEY_FILE`.

Credential refs are key paths into the decrypted SOPS document:

```yaml
inventory:
  providers:
    netbox-prod:
      groups:
        custcbb:
          auth:
            mode: password
            username_ref: expedient.username
            credential_provider: sops
            password_ref: expedient.password
```

Example decrypted document shape:

```yaml
expedient:
  username: chris.jones
  password: secret-value
hosts:
  acm-lab-agg-sw1:
    password: host-specific-secret
```

Implementation rules:

- nssh may call the `sops` CLI or a SOPS library.
- nssh decrypts per credential request, extracts only the requested scalar values,
  and releases the decrypted document before the request returns.
- nssh must not cache decrypted SOPS documents in the agent or provider instance.
- nssh must never write decrypted SOPS content to disk.
- nssh must not own SOPS key generation beyond optional guided setup.
- Editing secrets is done through SOPS, not a custom nssh credential editor.
- If nssh offers a write helper later, it must be a thin SOPS wrapper and remain
  optional.

### 1Password

1Password remains supported as an external provider.

Rules:

- Store provider name and item/field refs in nssh config.
- Prefer direct field refs such as `op://Vault/item/password`.
- Support literal usernames in inventory to avoid extra provider calls.
- Do not add provider-only benchmarks.
- Keepalive is allowed only when explicitly enabled on that 1Password provider
  instance.
- Keepalive may run only `op whoami`, with `--account` when the provider config
  includes an account.
- Keepalive arms only after a successful user-initiated credential request for
  that provider.
- Keepalive suspends after failure or timeout and re-arms only after the next
  successful user-initiated credential request.
- A user-initiated 1Password credential request may run `op signin` once after a
  signed-out `op read` or `op item get` failure, then retry the same credential
  command.
- Do not store 1Password output in a disk cache.
- Agent-brokered provider access must remain a request broker, not a password
  cache.
- Do not cache 1Password item JSON, field values, or resolved passwords.
- Do not retain any 1Password provider access token in agent memory.
- Do not start 1Password keepalive merely because the agent started.
- Do not expose item refs, item names, stdout, stderr, or field values in agent
  status.

Opt-in config shape:

```yaml
credential:
  provider:
    op-expedient:
      type: 1password
      vault: Network
      keepalive: true
      keepalive_interval: 5m
```

`keepalive_interval` defaults to `5m` when keepalive is enabled. Valid values are
`1m` through `9m`; values above `9m` are rejected because 1Password CLI
authorization expires after 10 minutes of inactivity.

### Bitwarden

Bitwarden remains supported as an external provider.

Rules:

- Store provider name and item refs in nssh config.
- Retain `BW_SESSION` only when explicitly enabled on that Bitwarden provider
  instance.
- Keep `BW_SESSION` inside `nssh agent`; do not export it to the shell or persist
  it to disk.
- Clear `BW_SESSION` on `nssh agent stop`, `nssh agent reset`, idle expiry, and max-lifetime
  expiry.
- Use normal SSH/CP benchmarks to measure lookup impact.
- Do not require Bitwarden Lite for workstation-only workflows.
- Do not store Bitwarden item payloads in a disk cache.
- Do not cache Bitwarden item JSON, field values, or resolved passwords.
- `nssh agent status` may show that Bitwarden warm access is active, but must not
  expose the session value.

Opt-in config shape:

```yaml
credential:
  provider:
    bw-work:
      type: bitwarden
      warm_session: true
```

## CLI Contract

Provider setup:

```text
nssh self init
nssh self init sops-age
nssh self init 1password
nssh self init bitwarden
```

Inventory assignment:

```text
nssh inv set local/default --auth password --cred sops:expedient.password
nssh inv set netbox-prod/custcbb --auth password --cred sops:expedient.password
nssh inv set netbox-prod/custcbb --auth password --cred op-expedient:op://...
nssh inv set netbox-prod/custcbb --auth password --cred bitwarden:<item>
```

Rules:

- `self init` configures provider readiness only.
- `inv set` assigns auth only.
- Interactive `inv set` should guide the user through provider, ref, username,
  and scope without requiring long flags for normal use.
- Noninteractive flags remain available for scripting.
- Existing command help must not expose removed provider-only benchmark or vault
  concepts.

## Executed Work

Completed:

- Created the inventory/provider separation.
- Added provider-scoped inventory group auth mappings.
- Added `nssh inv set` host and provider-qualified group auth assignment.
- Replaced long credential assignment flags with `--auth` and `--cred`.
- Removed provider-only credential benchmarks.
- Kept credential timing inside real SSH/CP benchmarks.
- Removed old top-level `credentials:` config alias.
- Renamed credential provider include paths from `credentials/` to
  `credential/`.
- Fixed auth patch behavior so credential changes preserve existing usernames.
- Added provider setup under `nssh self init [provider]`.
- Kept `nssh self init` scoped to credential provider setup and readiness; it
  no longer assigns inventory auth.
- Added provider readiness diagnostics under `nssh self status`.
- Added `sops-age` as the default suggested credential provider.
- Added SOPS+age provider resolution for scalar key paths.
- Added SOPS+age readiness checks for `sops`, the configured SOPS file, and an
  explicitly configured age key file.
- Added optional SOPS+age setup helpers that ask before creating an age identity
  and ask before creating a starter encrypted SOPS file.
- Added shared provider execution behind direct and agent transports, with direct
  foreground transport used unless retained access requires the agent.
- Added SOPS+age request-scoped decrypt with no retained decrypted document
  cache.
- Added Bitwarden request-scoped lookup by default and agent-retained access only
  with explicit per-provider `warm_session`.
- Added lazy Bitwarden unlock during credential lookup. When warm session is
  disabled, `BW_SESSION` is passed only for the single credential request. When
  warm session is enabled, `BW_SESSION` is sent to the agent without exporting it
  to the shell.
- Added `agent.provider_request_timeout` so provider calls have a bounded
  request lifetime.
- Added explicit per-provider 1Password `keepalive`, with `op whoami` as the
  only refresh operation and status limited to sanitized state.
- Added one-shot `op signin` retry for user-initiated 1Password credential
  requests when `op read` or `op item get` reports that the account is signed
  out.
- Added `nssh agent status` managed-access, health, lifecycle, and resource
  output without listing providers that have no retained agent access state.
- Removed recording archive maintenance from the runtime agent and added
  explicit `nssh log archive` maintenance with timeout and non-blocking lock.
- Removed Pass provider runtime support, Pass setup UI, password-store
  readiness checks, and GPG setup owned by nssh.
- Updated help fixtures and example config to expose only `sops-age`,
  `1password`, and `bitwarden`.
- Updated project skill/reference docs for SOPS+age, Bitwarden `warm_session`,
  1Password `keepalive`, request-scoped credential handling, and explicit log
  archiving.
- Verified automated coverage with `go test ./...`.
- Reinstalled the development binary with `nssh self reinstall --dev` and
  verified the installed command reports a `dev` build.

Still useful from the completed work:

- Inventory auth mappings remain the right place to select credentials.
- Credential provider refs remain the right config shape.
- The benchmark simplification remains correct.
- Provider setup and inventory assignment remain separate.
- `nssh agent` remains the right boundary for controlled warm access.

## Remaining Work

Required before calling the credential contract fully shipped:

1. Manually validate a real SOPS+age credential lookup through an SSH benchmark.
2. Manually validate Bitwarden warm auth once Bitwarden is configured locally.
3. Manually validate 1Password keepalive behavior with a normal SSH benchmark.
4. Measure whether `op whoami` refreshes the same CLI authorization window used
   by `op read` after the request path signs in through `op signin`.

Deferred by contract:

- Disk credential cache.
- nssh vault, lock, unlock, or rekey UI.
- Provider-only benchmark commands.
- Bitwarden Lite or server-backed workstation requirements.
- macOS Keychain-only credential storage.

## References

- [SOPS documentation](https://getsops.io/docs/)
- [SOPS project overview](https://getsops.io/)
