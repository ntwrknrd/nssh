# Provider-Backed Credentials Agent Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace nssh's custom age-backed credential vault with provider-backed credentials using Pass, 1Password, and Bitwarden, while keeping an nssh agent only for background runtime state, provider-session brokering, and non-secret metadata caching.

**Architecture:** Providers own credential storage and authentication. nssh resolves inventory host/group context, selects the credential provider bound to that host or group, asks the provider for an SSH username/password record through the provider resolver or agent, and injects that record into the SSH/SCP PTY. The nssh agent is the background runtime for provider sessions, non-secret metadata caches, and maintenance jobs; it must not become a password manager or long-lived password cache.

**Tech Stack:** Go, Cobra, OpenSSH, Pass/password-store CLI, 1Password CLI (`op`), Bitwarden CLI (`bw`), Unix domain sockets, existing nssh `internal/secret`, existing nssh recording archiver.

---

## Contract

This document is the handoff contract for the credential-provider refactor. The
implementation must follow these decisions unless the user explicitly changes
the contract.

### Product Direction

- nssh must stop owning local credential encryption.
- nssh must stop maintaining a custom age credential vault as an active backend.
- nssh must default to external password-manager/provider integrations.
- Supported credential provider types for this phase are exactly:
  - `pass` (default suggestion)
  - `1password`
  - `bitwarden`
- Hardware-key support is already removed and must not be reintroduced.
- `age` is not a supported credential provider after this work.
- nssh must support multiple inventory sources. Each inventory source owns its
  generated SSH include file and routes discovered hosts into configured
  groups.
- nssh must support multiple named credential provider instances. Credential
  provider selection happens at the host/group credential binding, not as one
  global backend.
- `nssh self init` must make inventory sources and credential provider
  assignments explicit.
- Migrating existing `credentials.age` data is not in scope. Users are
  responsible for ensuring equivalent credentials exist in their chosen
  password manager before upgrading nssh and switching providers.

### Boundary

The central boundary is:

```text
inventory host/group resolution -> credential binding -> credential provider instance/session -> request-scoped secret -> PTY password injection
```

Provider responsibilities:

- Store credentials at rest.
- Own user authentication.
- Own provider-specific session mechanisms.
- Own provider-specific item/key lifecycle.

nssh responsibilities:

- Resolve which host/group credential binding is needed.
- Read/write credential records through the provider instance selected by that
  binding.
- Broker provider sessions where that avoids repeated provider authentication.
- Cache only non-secret lookup metadata by default.
- Inject credentials into SSH/SCP prompt handling.
- Provide status and diagnostics for the nssh background runtime.

nssh must not:

- Store provider master passwords.
- Store provider session tokens.
- Store GPG private material.
- Store Bitwarden `BW_SESSION`.
- Store resolved SSH passwords beyond the active request by default.
- Recreate provider vault/key lifecycle under a different name.

### Inventory Contract

Inventory is multi-source:

- Local inventory is implicit and backed by
  `~/.ssh/nssh.d/provider_local.conf`.
- External inventory uses named `[inventory.provider.<name>]` entries.
- Multiple NetBox and containerlab providers may be configured at the same
  time.
- Each external provider writes only its own generated SSH include file,
  `~/.ssh/nssh.d/provider_<name>.conf`.
- Each provider routes discovered objects into configured inventory groups.
- Inventory group is the join point for group credential selection.
- Inventory provider name and credential provider name are independent.

## Command UX

### Remove Vault-Centric User Model

`nssh unlock` and `nssh lock` do not make sense as permanent commands after the
age vault is gone. They imply nssh controls the password manager's lock state,
which is false for Pass, 1Password, and Bitwarden.

The replacement user model is:

```text
Provider owns authentication. nssh agent owns short-lived runtime state.
```

### New Agent Commands

Add a top-level `nssh agent` command group:

```text
nssh agent
nssh agent status
nssh agent stop
nssh agent restart
nssh agent doctor
```

`nssh agent`:

Shows command help and available subcommands.

`nssh agent status`:

- Show nssh background runtime status.
- Show configured credential provider instances.
- Show provider-session status where applicable.
- Show non-secret cache counts without exposing hosts, groups, refs, usernames,
  item names, or secrets by default.
- Show whether any explicit secret cache is enabled; default should be disabled.
- Show idle and max lifetime expiry.
- Show recording archival status if enabled.

`nssh agent stop`:

- Stop the nssh background runtime.
- End nssh-owned provider sessions.
- Clear all nssh non-secret metadata caches.
- Stop background maintenance loops.
- Do not attempt to lock Pass, GPG, 1Password, or Bitwarden.

`nssh agent restart`:

- Stop the current nssh agent if running.
- Start a clean background-runtime agent.
- Do not prefetch or warm credentials.

`nssh agent doctor`:

- Diagnose configured provider instance availability.
- Diagnose agent socket state.
- Diagnose provider-session policy and request-scoped secret handling.
- Diagnose provider CLI setup enough to give actionable next steps.
- Do not reveal secret values.

Do not add these commands:

```text
nssh agent clear
nssh agent warm HOST
nssh agent warm -g GROUP
```

Those commands expose cache internals and train users to manage entries
manually. Cache invalidation must be automatic.

### Compatibility Aliases

No compatibility aliases are needed.

## Credential Provider Contract

### Shared Provider Interface

Keep the current provider interface shape as the contract for one configured
provider instance:

```go
type Record struct {
    Username string
    Secret   *secret.Secret
    Ref      string
}

type Provider interface {
    GetHost(host string) (*Record, error)
    SetHost(host string, record *Record) error
    RemoveHost(host string) (bool, error)
    GetGroup(group string) (*Record, error)
    SetGroup(group string, record *Record) error
    RemoveGroup(group string) (bool, error)
    Status() Status
}
```

Do not keep `credential.NewProvider(cfg)` as a global backend factory. Replace
it with a credential registry/resolver that loads named provider instances and
selects the right instance from the host/group binding.

Add a binding model similar to:

```go
type Binding struct {
    Provider    string
    Ref         string
    Username    string
    UsernameRef string
}
```

Resolution order:

1. Host binding for the exact host.
2. Group binding for the resolved inventory group.
3. No password credential.

A binding must include `Provider`. nssh does not infer a credential provider
from global config.

Provider callers must not know the provider's storage format.

Add provider capability contracts similar to:

```go
type ProviderSessionPolicy string

const (
    ProviderSessionExternal   ProviderSessionPolicy = "external"
    ProviderSessionAgentOwned ProviderSessionPolicy = "agent_owned"
    ProviderSessionNone       ProviderSessionPolicy = "none"
)

type Capabilities struct {
    ProviderSessionPolicy ProviderSessionPolicy
    SupportsHostCRUD      bool
    SupportsGroupCRUD     bool
    SupportsSecretRefs    bool
    SupportsStatusCheck   bool
}
```

The exact names may change, but the behavior must not:

- Pass lets `gpg-agent` own provider authentication/session caching.
- 1Password defaults to agent-owned provider-session brokering.
- 1Password must not cache resolved SSH passwords by default.
- Bitwarden defaults to external provider-session handling.
- Bitwarden must not store `BW_SESSION`; `BW_SESSION` is provider auth/decrypt
  material.
- Resolved password caching is not a supported feature in this plan.

### Config Contract

Replace the single global `credential.type = "age"` model with named provider
instances and host/group credential bindings.

Default config:

```toml
[credential]
[credential.provider.pass-local]
type = "pass"

[credential.provider.pass-local.config]
command = "pass"
prefix = "nssh"

[inventory.group.default]
provider = "pass-local"
ref = "nssh/groups/default"
```

Provider type values:

```toml
type = "pass"
type = "1password"
type = "bitwarden"
```

Provider-specific config should stay under
`[credential.provider.<name>.config]` unless there is a strong reason to split
it.

Suggested additional Pass provider:

```toml
[credential.provider.pass-local]
type = "pass"

[credential.provider.pass-local.config]
command = "pass"
prefix = "nssh"
```

Suggested 1Password config:

```toml
[credential.provider.op-network]
type = "1password"

[credential.provider.op-network.config]
account = ""
vault = "Network"
session = "agent"
```

Suggested Bitwarden config:

```toml
[credential.provider.bw-lab]
type = "bitwarden"

[credential.provider.bw-lab.config]
session = "external"
```

Suggested credential bindings:

```toml
[inventory.group.default]
provider = "pass-local"
ref = "nssh/groups/default"

[inventory.group.prod]
provider = "op-network"
ref = "Network Shared Admin"

[inventory.host.edge01]
provider = "bw-lab"
ref = "nssh host edge01"
username = "netops"
```

Config rules:

- Provider instance names must be TOML bare-key safe.
- Provider binding names must reference configured provider instances.
- Every host/group credential binding must specify a configured provider.
- `age` must not validate as a provider type.
- Invalid provider-session policy must fail validation.
- Host credential bindings override group credential bindings.
- Group credential bindings are independent of inventory provider names. Do not
  infer credential provider from inventory provider.

### Credential Paths

Use deterministic item paths/names for nssh-owned credentials.

Pass:

```text
nssh/hosts/<host>
nssh/groups/<group>
```

1Password:

```text
nssh host <host>
nssh group <group>
```

Bitwarden:

```text
nssh host <host>
nssh group <group>
```

For Pass entries, use this format:

```text
<password>
username: <username>
```

Parsing rules:

- First line is the password.
- `username: <value>` is the username.
- Unknown metadata lines are ignored.
- Empty password is invalid when setting a credential.
- Empty username is invalid when setting a credential.

Do not invent a JSON format inside Pass. It makes manual use worse.

### Provider Notes

Pass:

- Use the `pass` CLI.
- Detect `pass` and `gpg`.
- Detect initialization by checking the configured store has `.gpg-id` or by
  classifying the standard "run pass init" failure.
- Let `gpg-agent` own authentication and caching.
- Do not cache plaintext in nssh by default.
- `gopass` compatibility can be considered only through a configurable command
  if it behaves like Pass for the required operations. The provider remains
  named `pass`.

1Password:

- Keep the existing `op` integration as the starting point.
- Keep deterministic item names and explicit `op://` refs.
- Current investigation showed repeated `op` calls from one long-lived process
  context reuse 1Password authorization after one user approval; separate
  short-lived nssh processes do not reliably share that authorization.
- Move 1Password reads/writes behind the nssh agent when
  `session = "agent"`.
- Let the agent process be the stable `op` caller so 1Password app integration
  can reuse its provider-owned authorization session.
- Use `op signin` or the first needed `op` command idempotently to establish
  authorization when the provider session is missing or expired.
- Do not cache resolved SSH passwords or revealed item JSON in the agent.
- Never cache 1Password account auth state, `OP_SESSION`, or service account
  tokens.
- Do not implement this by exporting or storing `OP_SESSION`; that is provider
  auth material.
- If the 1Password authorization expires, let the next provider request trigger
  provider authentication again.

Bitwarden:

- Use the `bw` CLI.
- Do not cache `BW_SESSION`.
- Bitwarden CLI unlock produces a session key used for vault-data commands.
- If the user wants Bitwarden session behavior, they must manage `BW_SESSION`
  through Bitwarden's own documented mechanism.
- Do not add agent-owned Bitwarden sessions in this plan.
- Do not cache resolved SSH passwords in the nssh agent by default.
- Support deterministic item names for nssh-owned records.

## Agent Contract

### Product Meaning

The nssh agent is the nssh background runtime. It is not a vault unlock daemon
after age is removed.

It owns:

- Unix socket IPC.
- Same-user peer verification.
- Daemon lifecycle and readiness.
- Idle timeout and max lifetime tracking.
- Signal handling.
- Status reporting.
- Connection and request resource limits.
- Provider-session brokering.
- Non-secret provider metadata cache.
- Recording archival background task.

It must stop owning:

- age decrypt operations.
- age recipient operations.
- any concept of a local nssh vault key.

### Agent Status Model

Status should be module-oriented:

```text
Agent: active
Credential providers: pass-local (Pass), op-network (1Password)
Provider sessions: op-network active, idle expiry in 8m
Recording archival: enabled, next run unknown
```

Default status output must not show:

- host names
- group names
- credential refs
- usernames
- secret values
- provider item names

Verbose status may show provider-session and non-secret cache metadata if
useful, but still must not show secret values.

### Provider Session and Request Secret Security

The agent must not become a long-lived password cache. The design is
provider-session brokering plus non-secret metadata caching. Resolved passwords
are request-scoped only.

Required properties:

- Memory only.
- No disk persistence.
- Same-UID peer verification on every client connection.
- Secure socket directory permissions.
- Restrictive socket umask.
- Socket creation lock remains in place.
- Request size limits remain in place.
- Concurrent connection limits remain in place.
- Idle timeout remains in place.
- Max lifetime remains in place.
- Any request-scoped secrets are stored using secret-aware memory handling where
  possible.
- Request-scoped secret values are zeroed or destroyed after use where
  possible.

Allowed cached data:

- Provider-session handles or process context that do not expose provider
  tokens to nssh.
- Non-secret lookup metadata: provider instance name, item IDs, vault IDs,
  account IDs, cache miss markers, timestamps, and source metadata needed for
  correctness.
- Short-lived negative lookup markers.

Forbidden cached data:

- provider master passwords
- provider session tokens
- GPG private keys
- age identities
- Bitwarden `BW_SESSION`
- 1Password session/auth material
- resolved SSH passwords beyond the active request
- revealed 1Password item JSON that includes concealed field values

Cache keys:

- Must include provider instance name.
- Must include provider type.
- Must include provider account/vault/profile scope where relevant.
- Must include normalized host/group/ref input.
- Must be hashed before storage or status output.
- Must not expose host/group/ref names in status by default.

Invalidation:

- `inv set --credential-ref` invalidates affected host/group metadata cache.
- `inv set --credential-clear` invalidates affected host/group metadata cache.
- `inv set --credential-ref` invalidates affected host/group metadata cache.
- provider config changes invalidate provider sessions and metadata cache.
- `agent stop` ends provider sessions and clears all metadata cache.
- `agent restart` ends provider sessions and clears all metadata cache.
- negative lookup entries have a short TTL.

If precise per-entry invalidation is not available in the first patch, the
fallback is to stop/restart the agent after credential mutation.

1Password-specific session behavior:

- `session = "agent"` means the agent executes `op` commands for that provider
  instance.
- The client process should not call `op` directly for connect-time reads when
  agent session mode is active.
- One provider authorization should cover repeated requests until 1Password's
  own app-integration session expires.
- The agent may keep non-secret session context alive by remaining the stable
  `op` caller, but it must not extract or store `OP_SESSION`.
- The agent must return only the requested credential record to the requesting
  nssh process and then destroy its local copy as soon as practical.

### Recording Archival

Recording archival is currently hosted by the agent. Keep that in mind when
renaming and refactoring:

- If the agent remains the nssh background runtime, recording archival can stay
  there.
- If the agent is narrowed to only provider-session brokering, recording
  archival must move to a different runtime. This plan chooses the first
  option.

## Setup Contract

### `nssh self init`

`nssh self init` should become a first-run setup wizard. It must not be a
vault/passphrase setup screen.

The wizard must configure two explicit concepts:

```text
Inventory sources
Credential provider assignments
```

Do not silently collapse these into one provider choice. Existing config values
may be preselected as defaults, but the user still needs to see the inventory
sources and credential provider assignments.

Inventory source choices:

- `local` - default/recommended for first setup. This is the implicit local
  inventory backed by `~/.ssh/nssh.d/provider_local.conf`, not an
  `[inventory.provider.*]` entry.
- `netbox` - add one or more named `[inventory.provider.<name>]` entries with
  `type = "netbox"`.
- `containerlab` - add one or more named `[inventory.provider.<name>]` entries
  with `type = "containerlab"`.

Each external inventory source owns its own generated SSH include file:

```text
~/.ssh/nssh.d/provider_<inventory-provider-name>.conf
```

Each source routes discovered hosts into one or more groups. Group names are
the bridge between inventory and credential selection; inventory provider names
must not imply credential provider names.

Credential provider assignment choices:

- `pass` - default/recommended local credential provider.
- `1password` - use the 1Password CLI provider.
- `bitwarden` - use the Bitwarden CLI provider.

Credential providers are named instances. A user may configure multiple
instances, including multiple instances of the same provider type. Host and
group credential bindings choose one of those instances.

Wizard flow:

1. Detect existing config, inventory sources, groups, credential providers, and
   host/group credential bindings.
2. Ensure nssh directories exist.
3. Ask which inventory sources to configure. Default is local only.
4. Allow adding multiple NetBox and containerlab sources.
5. Collect source-specific settings and route each source into groups.
6. Ask which credential provider instances to configure. Default is
   `pass-local`.
7. Assign a credential provider instance to each configured group.
8. Allow host-level credential provider overrides only for existing hosts; new
   host overrides can be handled later through `nssh inv set --credential-ref`.
9. Validate provider dependencies and config.
10. Write or merge `config.toml` only after validation succeeds.
11. Back up existing `config.toml` before overwriting it.
12. Ensure the SSH config include is installed.
13. Print concise status and next steps.

`--dry-run` should show selected inventory sources, credential provider
instances, credential assignments, dependency checks, and planned file changes
without writing. `--yes` may accept safe defaults for missing config (`local`
inventory, `pass-local` credential provider, and an explicit `default` group
bound to `pass-local`), but it must not create GPG keys, create provider accounts, store
tokens, or make provider-auth decisions.

Local inventory setup:

- Ensure an explicit `default` group exists.
- Ensure `~/.ssh/nssh.d/provider_local.conf` can be used.
- Do not require a first host during init.

NetBox setup:

- Ask for provider name.
- Ask for `base_url` or `url_env`.
- Ask for `token_env` and optional `env_file`; never ask for or store the
  token value itself.
- Ask for at least one route group.
- Allow multiple NetBox sources.
- Validate the provider block without requiring a live NetBox request unless
  the required env vars are present.

Containerlab setup:

- Ask for provider name.
- Ask for `jump_host`.
- Ask for `sudo` and `strict_host_key_checking`.
- Ask for at least one route group.
- Allow multiple containerlab sources.
- Validate that `jump_host` is not empty.

Pass setup:

1. Detect `pass`.
2. Detect `gpg`.
3. Detect whether the configured password store is initialized.
4. If initialized, configure a named Pass provider instance.
5. If not initialized, inspect existing GPG secret keys.
6. If one usable key exists, offer to run `pass init <key>`.
7. If multiple usable keys exist, prompt for one.
8. If no usable key exists, offer guided GPG key generation.
9. GPG key generation must be explicit; do not silently create keys.
10. Validate with a temporary write/read/delete round trip.

1Password setup:

- Detect `op`.
- Ask for provider instance name.
- Ask for account if needed.
- Ask for vault.
- Default `session` to `agent`.
- Verify the CLI is usable enough to diagnose auth/setup state.
- Do not cache or store 1Password auth/session material.

Bitwarden setup:

- Detect `bw`.
- Ask for provider instance name.
- Default `session` to `external`.
- Run `bw status` for diagnostics when available.
- Do not ask for, cache, or store `BW_SESSION`.
- If Bitwarden is locked or unauthenticated, print provider-owned next steps
  for login/unlock and `BW_SESSION`.

Credential assignment setup:

- Ask for the default credential provider instance.
- Ask for the provider instance for each configured group.
- Use the default provider for a group only when the user accepts that default.
- Preserve existing host overrides.
- Allow changing host overrides when existing hosts are visible during init.
- Do not probe every provider for a host credential during normal connection
  resolution. Use the binding.

Package-manager behavior:

- On macOS, show `brew install pass gnupg`, `brew install 1password-cli`, or
  `brew install bitwarden-cli` when a configured provider instance dependency
  is missing.
- On Linux, show distro package hints.
- Do not run package-manager commands automatically.

### Existing Age Data

No age migration/export workflow is in scope.

- Do not add an age migration command.
- Do not add an age export command.
- Do not read `credentials.age` as part of provider setup.
- Do not preserve `internal/vault` solely for migration.
- Users must manually recreate or import equivalent credentials in Pass,
  1Password, or Bitwarden before switching providers.

Legacy context credentials:

- Do not preserve the old `ctx` command family as a long-term credential model.
- If `ctx` is still needed for one release, mark it legacy in help text and
  docs, but do not use it as a migration mechanism.

## Implementation Tasks

### Task 1: Add Provider Config Types

**Files:**

- Modify: `internal/config/inventory.go`
- Modify: `internal/config/settings.go`
- Modify: `docs/examples/config/config.example.toml`
- Test: `internal/config/inventory_test.go`
- Test: `internal/config/settings_test.go`

- [x] **Step 1: Write config tests**

Add tests that verify:

- default credential config creates `pass-local`
- `pass`, `1password`, and `bitwarden` validate as provider instance types
- `age` no longer validates as a provider instance type
- provider instance names must be TOML bare-key safe
- host/group credential bindings must reference configured provider instances
- host/group credential bindings must specify a provider explicitly
- invalid provider-session policy fails
- Pass command defaults to `pass`

Run:

```bash
go test ./internal/config -run 'Test.*Credential' -count=1
```

Expected: tests fail before implementation because provider registry config and
binding validation are not implemented.

- [x] **Step 2: Implement config changes**

Add provider constants for `pass`, `1password`, and `bitwarden`. Remove `age`
from active validation. Replace global `credential.type` with
`credential.provider.<name>` and host/group binding provider fields. Add
provider config fields for command, prefix, account, vault, and
provider-session policy.

- [x] **Step 3: Update example config**

Make `docs/examples/config/config.example.toml` show Pass as the default and
1Password/Bitwarden as alternatives.

- [x] **Step 4: Verify config package**

Run:

```bash
go test ./internal/config -count=1
```

Expected: pass.

### Task 2: Redesign `nssh self init` Source and Credential Selection

**Files:**

- Modify: `internal/cli/self/init.go`
- Test: `internal/cli/self/*_test.go`
- Modify: `docs/examples/config/config.example.toml`
- Modify: `docs/examples/help/**/*.txt`

- [x] **Step 1: Write init provider-selection tests**

Cover:

- interactive init shows inventory sources and credential provider assignments
- local inventory plus Pass writes `pass-local` and binds an explicit `default`
  group to it
- multiple inventory sources write multiple `[inventory.provider.<name>]`
  blocks
- NetBox setup writes provider config without storing a token value
- Containerlab setup requires `jump_host`
- 1Password setup writes a named account/vault provider instance and does not
  store auth material
- 1Password setup defaults to agent-owned provider session
- Bitwarden setup writes a named provider instance and does not store
  `BW_SESSION`
- different groups can be assigned to different credential provider instances
- `--yes` accepts local inventory plus Pass for missing config, but does not
  create GPG keys or provider accounts silently
- existing config values are preselected and config writes are backed up

Run:

```bash
go test ./internal/cli/self -run 'TestInit.*Provider|TestInit.*Credential' -count=1
```

Expected: fail before the init wizard is refactored.

- [x] **Step 2: Refactor init into a testable plan/apply flow**

Separate provider selection, dependency checks, config merge, file writes, and
SSH include setup. Use injected prompt and command-runner interfaces so provider
setup can be tested without real Pass, GPG, `op`, or `bw` commands.

- [x] **Step 3: Implement the provider-selection wizard**

The UI should use short labels:

```text
Inventory sources: Local SSH config, NetBox, Containerlab
Credential providers: Pass, 1Password, Bitwarden
Credential assignment: default -> pass-local
```

Collect only settings nssh needs. Do not ask for or store provider secrets.

- [x] **Step 4: Verify self init**

Run:

```bash
go test ./internal/cli/self -count=1
```

Expected: pass.

### Task 3: Replace Age Provider With Pass Provider

**Files:**

- Create: `internal/credential/pass.go`
- Create: `internal/credential/pass_test.go`
- Modify: `internal/credential/provider.go`
- Remove or quarantine: `internal/credential/age.go`
- Test: `internal/credential/pass_test.go`

- [x] **Step 1: Write Pass provider tests**

Cover:

- deterministic host path `nssh/hosts/<host>`
- deterministic group path `nssh/groups/<group>`
- `pass show` parsing first-line password and `username:`
- `pass insert --multiline --force` receives the expected entry body
- missing entries return nil
- status reports unavailable when `pass` is missing

Run:

```bash
go test ./internal/credential -run 'TestPass' -count=1
```

Expected: fail before provider exists.

- [x] **Step 2: Implement Pass provider**

Use an injected command runner like the existing 1Password provider. Do not call
the shell. Do not parse command output with fragile prompts; treat command exit
status and stdout/stderr deliberately.

- [x] **Step 3: Wire provider registry**

The credential registry must construct Pass provider instances for configured
providers with `type = "pass"` and must no longer construct age providers for
active config.

- [x] **Step 4: Verify credential package**

Run:

```bash
go test ./internal/credential -count=1
```

Expected: pass.

### Task 4: Add Bitwarden Provider

**Files:**

- Create: `internal/credential/bitwarden.go`
- Create: `internal/credential/bitwarden_test.go`
- Modify: `internal/credential/provider.go`
- Test: `internal/credential/bitwarden_test.go`

- [x] **Step 1: Write Bitwarden provider tests**

Cover:

- deterministic host item name `nssh host <host>`
- deterministic group item name `nssh group <group>`
- `bw get item` JSON parsing
- `bw item create` or edit command construction
- missing items return nil
- no test or implementation stores `BW_SESSION` in the nssh agent

Run:

```bash
go test ./internal/credential -run 'TestBitwarden' -count=1
```

Expected: fail before provider exists.

- [x] **Step 2: Implement Bitwarden provider**

Use an injected command runner. Do not cache Bitwarden session tokens. Let the
user or Bitwarden CLI environment own Bitwarden authentication.

- [x] **Step 3: Verify credential package**

Run:

```bash
go test ./internal/credential -count=1
```

Expected: pass.

### Task 5: Make Provider Session Policy Explicit

**Files:**

- Modify: `internal/credential/provider.go`
- Modify: `internal/credential/onepassword.go`
- Modify: `internal/credential/pass.go`
- Modify: `internal/credential/bitwarden.go`
- Test: `internal/credential/provider_test.go`
- Test: `internal/credential/onepassword_test.go`

- [x] **Step 1: Write provider-session policy tests**

Cover:

- Pass reports provider session ownership as external.
- 1Password reports agent-owned provider session by default.
- Bitwarden reports external provider-session ownership by default.
- Bitwarden does not report `BW_SESSION` as agent-owned session state.
- Invalid provider-session policy values fail validation.
- Providers do not advertise resolved-password retention as a capability.

Run:

```bash
go test ./internal/credential -run 'Test.*Session|Test.*Cache' -count=1
```

Expected: fail before capabilities exist.

- [x] **Step 2: Add capability metadata**

Add provider capabilities without leaking provider-specific internals into
connect/SCP callers.

- [x] **Step 3: Verify provider tests**

Run:

```bash
go test ./internal/credential -count=1
```

Expected: pass.

### Task 6: Refactor Agent Into Provider Session Runtime

**Files:**

- Modify: `internal/agent/protocol.go`
- Modify: `internal/agent/daemon.go`
- Modify: `internal/agent/provider.go`
- Modify: `internal/agent/provider_cache.go`
- Modify: `internal/agent/client.go`
- Modify: `internal/agent/doc.go`
- Modify: `internal/credential/onepassword.go`
- Test: `internal/agent/*_test.go`
- Test: `internal/credential/onepassword_test.go`

- [x] **Step 1: Write agent session tests**

Cover:

- agent can serve provider requests without age provider methods
- 1Password provider requests can be routed through the agent
- repeated 1Password requests use the same long-lived agent process context
- 1Password request handling does not store resolved passwords in the agent
  cache
- metadata cache entries can be put/get/delete by key or namespace
- metadata cache can be fully cleared
- status includes provider-session state and metadata cache counts but not raw
  keys
- stop ends provider sessions and clears metadata cache because process exits

Run:

```bash
go test ./internal/agent ./internal/credential -run 'Test.*Session|Test.*Cache|Test.*Status|TestOnePassword.*Agent' -count=1
```

Expected: fail before protocol/session changes.

- [x] **Step 2: Remove age-specific provider requirements from agent runtime**

Do not require `Decrypt` or `Recipient` behavior for provider-session runtime.
Remove age decrypt behavior from the main provider-session path.

- [x] **Step 3: Add provider request handling**

Add agent protocol/client support for provider-scoped requests needed by
1Password reads and writes. The agent should execute `op` itself for
`session = "agent"` providers and return only the requested credential record to
the client.

- [x] **Step 4: Remove default secret cache**

Remove the current default behavior that writes 1Password passwords or revealed
item JSON into the agent cache. Keep only non-secret metadata cache by default.

- [x] **Step 5: Preserve runtime responsibilities**

Keep socket security, peer verification, lifecycle timers, signal handling,
resource limits, and recording archival behavior.

- [x] **Step 6: Verify agent package**

Run:

```bash
go test ./internal/agent ./internal/credential -count=1
```

Expected: pass.

### Task 7: Add `nssh agent` CLI

**Files:**

- Create: `internal/cli/agent/agent.go`
- Create: `internal/cli/agent/agent_test.go`
- Modify: `cmd/nssh/main.go`
- Modify: `docs/examples/help/nssh.txt`
- Create: `docs/examples/help/agent.txt`
- Create: `docs/examples/help/agent/status.txt`
- Create: `docs/examples/help/agent/stop.txt`
- Create: `docs/examples/help/agent/restart.txt`
- Create: `docs/examples/help/agent/doctor.txt`

- [x] **Step 1: Write command tests**

Cover:

- `nssh agent` prints status help or status output
- `nssh agent status` does not reveal metadata cache keys or secrets
- `nssh agent stop` stops the agent if running and succeeds if not running
- `nssh agent restart` starts a clean provider-session runtime
- help does not include `clear` or `warm`

Run:

```bash
go test ./internal/cli/agent ./cmd/nssh -count=1
```

Expected: fail before command exists.

- [x] **Step 2: Implement command group**

Use the existing Cobra patterns and styled help. Keep descriptions short:

```text
agent   Manage nssh agent
status  Show agent status
stop    Stop agent
restart Restart agent
doctor  Diagnose agent
```

- [x] **Step 3: Wire root command**

Add `agent` to root subcommands and preprocessing subcommand map.

- [x] **Step 4: Verify command tests**

Run:

```bash
go test ./internal/cli/agent ./cmd/nssh -count=1
```

Expected: pass.

### Task 8: Replace Connect-Time Age Use With Provider Resolution

**Files:**

- Modify: `internal/cli/resolve/resolve.go`
- Modify: `internal/cli/cp/cp.go`
- Modify: `internal/cli/self/bench/preflight.go`
- Test: `internal/cli/resolve/resolve_test.go`
- Test: `internal/cli/cp/cp_test.go`
- Test: `internal/cli/self/bench/preflight_test.go`

- [x] **Step 1: Write resolution tests**

Cover:

- connect path does not instantiate `vault.Manager`
- provider errors do not crash normal SSH key-based connect
- host credential binding wins over group credential binding
- group credential binding fallback still works
- different groups can resolve through different provider instances
- metadata cache keys include provider instance name
- resolved passwords are not stored in the agent cache by default
- `nssh cp` uses the same provider resolution path as SSH connect

Run:

```bash
go test ./internal/cli/resolve ./internal/cli/cp ./internal/cli/self/bench -count=1
```

Expected: fail before `cp` and bench are refactored.

- [x] **Step 2: Refactor shared provider resolution**

Keep one shared resolver for SSH and SCP so behavior cannot drift. The resolver
must select provider instance by host binding first, then group binding.

- [x] **Step 3: Remove vault unlock preflight**

Benchmarks should not unlock a local nssh vault. Provider auth should happen
naturally if the provider requires it.

- [x] **Step 4: Verify packages**

Run:

```bash
go test ./internal/cli/resolve ./internal/cli/cp ./internal/cli/self/bench -count=1
```

Expected: pass.

### Task 9: Refactor Credential CRUD and Legacy Context Commands

**Files:**

- Modify: `internal/cli/cred/*.go`
- Modify: `internal/cli/host/*.go` where credential management still uses vault
- Modify: `internal/cli/ctx/*.go`
- Test: existing CLI tests plus new targeted tests

- [x] **Step 1: Write CRUD tests**

Cover:

- `inv set/get auth mapping` uses the provider instance bound to the target scope
- `inv set/get auth mapping --provider <name>` can choose or change a host/group
  provider binding
- credential mutation invalidates cache
- host edit credential menu shows and can change the credential provider
  binding
- legacy `ctx` commands are marked legacy or removed according to final user
  decision before implementation

Run:

```bash
go test ./internal/cli/inv -count=1
```

Expected: fail where code still assumes age vault.

- [x] **Step 2: Route all credential CRUD through the credential registry**

Do not instantiate `vault.Manager` for normal credential CRUD. Resolve the
target provider instance from the host/group binding or explicit `--provider`.

- [x] **Step 3: Invalidate cache after mutation**

Prefer targeted invalidation. If targeted invalidation is not ready, stop the
agent after credential mutation.

- [x] **Step 4: Verify CLI packages**

Run:

```bash
go test ./internal/cli/inv -count=1
```

Expected: pass.

### Task 10: Remove Active Age Vault Surface

**Files:**

- Modify or remove: `internal/vault`
- Modify or remove: `internal/vault/software`
- Modify or remove: `internal/cli/session`
- Modify or remove: `internal/cli/unlock`
- Modify or remove: `internal/cli/lock`
- Modify: `go.mod`
- Modify: `go.sum`
- Modify: docs and help snapshots

- [x] **Step 1: Search active age usage**

Run:

```bash
rg -n 'CredentialProviderAge|credentials\\.age|age.key|age.pub|vault\\.Manager|nssh unlock|nssh lock|filippo.io/age' .
```

Expected: only historical docs if intentionally retained should remain. There
must be no migration command, migration tests, active age provider, or active
age vault path.

- [x] **Step 2: Remove active code paths**

Remove imports, providers, commands, docs, and help text that present age as an
active credential backend.

- [x] **Step 3: Remove age dependencies**

Remove `filippo.io/age` from `go.mod` and `go.sum` unless another
non-credential feature still requires it. Do not keep the dependency for
migration.

- [x] **Step 4: Verify repository**

Run:

```bash
go mod tidy
go test ./...
go build -trimpath -buildvcs=false ./cmd/nssh
git diff --check
```

Expected: all pass.

### Task 11: Documentation and Help Snapshots

**Files:**

- Modify: `README.md`
- Modify: `docs/ARCHITECTURE.md`
- Modify: `docs/USER_GUIDE.md`
- Modify: `docs/examples/config/config.example.toml`
- Modify: `docs/examples/help/**/*.txt`

- [x] **Step 1: Update user docs**

Docs must describe:

- Pass as default local provider.
- 1Password and Bitwarden as supported providers.
- provider-owned authentication.
- `nssh agent` as background runtime.
- no `unlock`/`lock` primary workflow.
- no automated age migration; users must recreate or import equivalent
  credentials themselves.

- [x] **Step 2: Update architecture docs**

Architecture must describe:

- provider abstraction.
- provider-session policy.
- request-scoped secret handling.
- agent runtime responsibilities.
- recording archival staying in agent.
- removed age vault as active backend.

- [x] **Step 3: Regenerate help snapshots**

Use the repo's existing help snapshot workflow. If no workflow exists, update
snapshots by running the built CLI help commands and capturing output in the
existing format.

- [x] **Step 4: Validate markdown**

Run:

```bash
/Users/cj/.local/bin/validate-markdown --file README.md
/Users/cj/.local/bin/validate-markdown --file docs/ARCHITECTURE.md
/Users/cj/.local/bin/validate-markdown --file docs/USER_GUIDE.md
/Users/cj/.local/bin/validate-markdown --file docs/superpowers/plans/2026-05-29-provider-backed-credentials-agent.md
```

Expected: all pass.

### Task 12: Final Verification

**Files:**

- All touched files.

- [x] **Step 1: Run full tests**

Run:

```bash
go test ./...
```

Expected: pass.

- [x] **Step 2: Run pure-Go build**

Run:

```bash
CGO_ENABLED=0 go build -trimpath -buildvcs=false ./cmd/nssh
```

Expected: pass.

- [x] **Step 3: Run diff hygiene**

Run:

```bash
git diff --check
```

Expected: no output.

- [x] **Step 4: Manual smoke tests**

Run with a temporary config/home where possible:

```bash
nssh agent status
nssh agent stop
nssh self status
nssh cp -h
nssh self status
```

Expected:

- agent commands work and do not mention unlock/lock.
- credential status reports configured provider instances and host/group
  bindings.
- `cp` help remains correct.
- self status shows inventory sources, credential provider instances, bindings,
  and agent runtime state.

## Open Decisions Before Coding

These are the only decisions an implementation agent should ask about before
coding if they are still unresolved:

1. Should `nssh unlock` and `nssh lock` be removed immediately or retained as
   one-release deprecation aliases?
2. Should legacy `nssh ctx` commands be removed in the same refactor or marked
   legacy for one release?
3. Should `gopass` be accepted only as `credential.config.command = "gopass"`
   under the `pass` provider, or rejected until explicitly requested?

Everything else in this document is decided enough to implement.
