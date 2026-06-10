# nssh Architecture

Audience: agents who need nssh architecture, package boundaries, and design
context. Source is authoritative. If this file conflicts with current code, fix
this file after reading the code.

## Product Contract

nssh wraps OpenSSH for operators with many SSH targets. It must preserve normal
OpenSSH behavior while adding:

- smart host lookup and fuzzy selection
- local and external inventory rendered as SSH config include files
- provider-backed SSH credentials from Pass, 1Password, or Bitwarden
- request-scoped password injection through a PTY
- optional asciinema session recording and log management
- legacy SSH compatibility detection and persisted remediation
- status, diagnostics, benchmarking, install, reset, and uninstall commands

nssh must not own external password-manager authentication, store long-lived
passwords, or maintain a local credential vault.

## Command Surface

The root command is built in `internal/app.NewRootCmd`.

Public command groups:

- `nssh [host]`: smart connect path. `internal/app.PreprocessArgs` rewrites an
  unknown first non-flag argument to hidden `smart-connect`.
- `nssh connect [host]`: direct connect path. Without a host, opens selection.
- `nssh cp`: SCP wrapper with shared host and credential resolution.
- `nssh inv`: inventory management and provider diagnostics.
- `nssh agent`: background runtime status, stop, restart, and doctor.
- `nssh log`: session recording list, play, delete, upload, export, auth, and
  search.
- `nssh self`: init, status, reinstall, uninstall, reset, version, cfg, and
  benchmarks.

Generated command examples live in `docs/examples/help/` and are verified by
`cmd/nssh/help_test.go`. Prefer those snapshots or `nssh --help` over copying
flag tables into docs.

## Storage Model

Path resolution lives in `internal/config/paths.go`.

- Config: `$XDG_CONFIG_HOME/nssh/config.toml`, defaulting to
  `~/.config/nssh/config.toml`.
- Data: `$XDG_DATA_HOME/nssh`, defaulting to `~/.local/share/nssh`.
- State: `$XDG_STATE_HOME/nssh`, defaulting to `~/.local/state/nssh`.
- Recordings: `config.Paths.RecordingsDir`, defaulting to
  `~/.local/state/nssh/casts`.
- Backups: `config.Paths.BackupDir`, defaulting to
  `~/.local/share/nssh/backups`. This is used for local inventory SSH config
  writes, currently `provider_local.conf`, with fixed tiered retention.
- SSH config: `~/.ssh/config` plus managed include files under `~/.ssh/nssh.d/`.

`nssh self init` should ensure the SSH config includes the managed directory and
uses `docs/examples/config/config.example.toml` through
`internal/config/embed.go` when creating a fresh config.

## Configuration

Configuration loading, include merging, defaults, environment overrides, and
validation live in `internal/config`.

Key structures:

- `config.Config`
- `config.AgentConfig`
- `config.CredentialConfig`
- `config.InventoryConfig`
- `config.LoggingConfig`
- `config.SSHConfig`

TOML include handling is implemented by `internal/config/include.go`. Included
files merge before the importing file overrides them. Use this for modular
credential and inventory config instead of adding new root config files.

The public example config is
`docs/examples/config/config.example.toml`. Keep field-level examples there, not
in narrative docs.

## Inventory

Inventory is the source of host placement and generated SSH config.

Primary packages:

- `internal/cli/inv`: command implementation.
- `internal/inventory`: provider state, group matching, reconciliation, local
  host metadata, and SSH config rendering.
- `internal/inventory/providers`: provider implementations.

There is an implicit local provider named `local`. Its include file is
`inventory.LocalProviderIncludeFile()`, currently `nssh.d/provider_local.conf`.
Writes to that file create timestamped backups under
`~/.local/share/nssh/backups` and prune them with fixed tiered retention:
10 from the last hour, 5 more from the last day, and 1 per day for the previous
7 days.
External providers write deterministic include files through
`inventory.ProviderIncludeFile(provider)`, currently
`nssh.d/provider_<provider>.conf`.

Provider state is non-secret JSON under
`$XDG_STATE_HOME/nssh/inventory/providers/`. It records provider objects, group
membership, last refresh time, include file, auth mode, and persisted compatibility
fixes.

Current external providers:

- NetBox: `internal/inventory/providers/netbox.go`; fetches devices from the
  NetBox API, supports environment-backed URL/token config, and normalizes
  device fields into group selector attributes.
- Containerlab: `internal/inventory/providers/containerlab.go`; runs
  `containerlab inspect --all --format json` on a jump host through
  `internal/ssh/remoteexec`.

Providers own groups and group selectors. The canonical group ID is
`<provider>/<group>`, for example `local/custcbb` or `netbox-prod/custcbb`.
Selectors are matched by `inventory.MatchGroupSelectors`.
`inventory.Reconcile` owns add/update/remove planning.
`inventory.WriteProviderSSHConfig` renders provider-owned SSH config. Auth mode
defaults to password when the target group has password auth, otherwise key.

## Credentials

Credential storage is external. nssh stores only auth mappings that tell it
which provider and item reference apply to a host or provider-owned group.

Primary packages:

- `internal/credential`: provider registry and provider implementations.
- `internal/config/inventory.go`: credential and inventory auth config types.
- `internal/connect/resolve.go`: connect-time credential selection.

Supported credential providers:

- `pass`: `credential.passProvider`, using a `pass`-compatible command.
- `1password`: `credential.onePasswordProvider`, using `op`; default session
  mode is agent-owned.
- `bitwarden`: `credential.bitwardenProvider`, using `bw`; default session mode
  is external.

Auth mappings live under:

- `inventory.host.<host>.auth`
- `inventory.provider.<provider>.group.<group>.auth`

Every set auth mapping must include `credential_provider` and either
`password_ref` or `username_ref`. `username` and `username_ref` are optional and
mutually exclusive. Do not use legacy
`credential.host`, `credential.group`, or root `credential.type`; validation
rejects those forms.

Credential selection order in `internal/connect.resolveBoundCredential`:

1. Host auth override.
2. Inventory provider group auth mapping.
3. No nssh credential; OpenSSH config and keys handle auth.

`internal/connect.ResolveHostForConnect` is shared by connect and SCP. Keep it
that way so provider selection cannot drift between workflows.

Resolved passwords must be wrapped in `*secret.Secret` from `internal/secret`.
Use `secret.Use()` for temporary byte access. Do not format, log, or retain
secret bytes.

Password resolver execution is prompt-driven. The connector must not prefetch a
password before OpenSSH asks for one because an existing control session can
complete without any password prompt, and in that case nssh should not touch the
credential provider at all.

## Agent Runtime

`internal/agent` is a Unix-domain-socket runtime daemon. It is not a password
manager and must not become a long-lived password cache.

Responsibilities:

- provider-session requests for agent-owned providers
- socket path management and stale socket cleanup
- peer credential verification
- idle timeout, max lifetime, stop/restart behavior, and signal handling
- background recording archive runner

The protocol is in `internal/agent/protocol.go`. Public commands live in
`internal/cli/agent`. The internal stop operation is named `lock` in protocol
terms, but the user-facing command is `nssh agent stop`; there is no public
password-manager lock/unlock workflow.

Import boundary tests require `internal/agent` to stay below CLI, UI, and SSH
packages.

## Connection Flow

Interactive connection starts in `internal/connect.ConnectHost`.

1. `internal/app.PreprocessArgs` maps `nssh HOST` to hidden `smart-connect`.
2. `connect.ResolveHostname` checks SSH config, exact matches, suggestions, and
   fuzzy selection. On misses, it returns host-not-found immediately.
3. On host-not-found, `internal/app.Run` spawns `nssh inv set <host>` for local
   inventory creation.
4. `connect.ResolveHostForConnect` loads config, finds the SSH host entry,
   resolves inventory group metadata, selects a username, and resolves any
   provider-backed credential.
5. `connect.newConnector` builds an `internal/ssh/connector.Connector` with the
   host alias, optional username, optional secret, host-key policy, and timeout
   config.
6. The connector runs OpenSSH in a PTY, detects prompts, injects credentials only
   when prompted, handles host-key prompts, relays stdio and signals, and emits
   timing markers when enabled.
7. On legacy SSH negotiation failure, `connect.handleCompatibilityFix` probes
   the target with `internal/ssh/connector.TestConnection`, parses fixes from
   `internal/ssh/compat`, persists them to the right include file, and retries.
8. Optional recording wraps the outer connection before connector execution.

SCP uses the same host and credential resolver through `internal/cli/cp`.

## SSH Connector

`internal/ssh/connector` owns PTY lifecycle, prompt detection, host-key handling,
password injection, timing, and stdio relay. It should not import higher-level
CLI, UI, recording, or agent packages.

Key files:

- `connector.go`: connector state and public setters.
- `lifecycle_unix.go`: OpenSSH process lifecycle.
- `relay_unix.go`: PTY relay and signal handling.
- `password_unix.go`: prompt-driven password writes.
- `patterns.go`: prompt patterns.
- `accept_once_unix.go` and `hostkey.go`: host-key prompt behavior.
- `tester.go`: compatibility probe helper.

`internal/ssh/sshconfig` parses and mutates SSH config, including include
discovery and compatibility fix persistence. It may depend on `internal/ssh/compat`
but not on the connector.

## Recording And Logs

Recording planning and metadata live in `internal/recording`. The package does
not perform interactive terminal work. It decides whether to record, computes
cast paths, lock directories, append behavior, title templates, idle-limit
behavior, index metadata, text export, and archive eligibility.

`internal/connect/recording.go` wraps the outer nssh invocation with asciinema
and guards the inner process with `NSSH_RECORDING_INNER=1`.

The `nssh log` commands live in `internal/cli/log`. Background archive
maintenance is hosted by `internal/agent` but archive policy lives in
`internal/recording`.

## Audit, UI, And Exit Codes

Audit logging lives in `internal/audit` and writes security-relevant events to
the state directory when enabled.

Terminal rendering and prompts live in `internal/ui`. User-facing command output
should go through this package.

Exit codes are centralized in `internal/exit`:

- 0: success
- 1: general error
- 2: connection error
- 3: auth error
- 4: host not found
- 126: command not executable
- 127: command not found
