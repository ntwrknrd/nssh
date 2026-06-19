# nssh Architecture

Audience: agents who need nssh architecture, package boundaries, and design
context. Source is authoritative. If this file conflicts with current code, fix
this file after reading the code.

## Product Contract

nssh wraps OpenSSH for operators with many SSH targets. It must preserve normal
OpenSSH behavior while adding:

- smart host lookup and fuzzy selection
- local and external inventory resolved from nssh YAML config and provider state
- provider-backed SSH credentials from Pass, 1Password, or Bitwarden
- request-scoped password injection through a PTY
- optional asciinema session recording and log management
- typed legacy SSH compatibility fixes under nssh host SSH config
- status, diagnostics, benchmarking, install, reset, and uninstall commands

nssh must not own external password-manager authentication, store long-lived
passwords, or maintain a local credential vault.

## Command Surface

The root command is built in `internal/app.NewRootCmd`.

Public command groups:

- `nssh [ssh-options] host [command...]`: SSH-compatible smart connect path.
  `internal/app.PreprocessArgs` rewrites root SSH-style invocations to hidden
  `smart-connect`.
- `nssh --select`: opens smart target selection.
- `nssh --target host [command...]`: literal target escape hatch for names that
  collide with public nssh commands or should bypass fuzzy resolution.
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

- Config: `$XDG_CONFIG_HOME/nssh/config.yaml`, defaulting to
  `~/.config/nssh/config.yaml`.
- Data: `$XDG_DATA_HOME/nssh`, defaulting to `~/.local/share/nssh`.
- State: `$XDG_STATE_HOME/nssh`, defaulting to `~/.local/state/nssh`.
- Recordings: `config.Paths.RecordingsDir`, defaulting to
  `~/.local/state/nssh/casts`.
- Backups: `config.Paths.BackupDir`, defaulting to
  `~/.local/share/nssh/backups`. This remains for legacy local inventory helper
  paths, not for current config writes.

`nssh self init` uses `docs/examples/config/config.example.yaml` through
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

YAML include handling is implemented by `internal/config/include.go`. Included
files merge before the importing file overrides them. Use this for modular
credential and inventory config instead of adding new root config files.

The public example config is
`docs/examples/config/config.example.yaml`. Keep field-level examples there, not
in narrative docs.

## Inventory

Inventory is the source of host placement and OpenSSH argv inputs.

Primary packages:

- `internal/cli/inv`: command implementation.
- `internal/inventory`: provider state, group matching, reconciliation, and
  local host metadata.
- `internal/inventory/providers`: provider implementations.

There is an implicit local provider named `local`. Local hosts live under
`inventory.providers.local.hosts`, usually in
`~/.config/nssh/inventory/local.yaml`.
External provider discovered state is non-secret JSON; operator-owned groups,
auth mappings, SSH options, and host overrides live in provider-scoped YAML
config.

Provider state is non-secret JSON under
`$XDG_STATE_HOME/nssh/inventory/providers/`. It records provider objects, group
membership, and last refresh time.

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
Auth mode defaults to password when the target group has password auth,
otherwise key.

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

Auth mappings live under provider-scoped YAML:

- `inventory.providers.<provider>.hosts.<host>.auth`
- `inventory.providers.<provider>.groups.<group>.auth`

Password-backed auth mappings must include `credential_provider` plus
`password_ref` or `username_ref`. `username` and `username_ref` are optional and
mutually exclusive; key-mode mappings may set only `mode` and `username`. Do not
use legacy
`credential.host`, `credential.group`, or root `credential.type`; validation
rejects those forms.

Credential selection order in `internal/config.ResolveInventoryAuth` and
`internal/connect.resolveInventoryCredential`:

1. Host auth override.
2. Inventory provider group auth mapping.
3. No nssh credential; key auth or the SSH agent handles auth.

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

1. `internal/app.PreprocessArgs` maps root SSH-style commands to hidden
   `smart-connect`.
2. `connect.ResolveHostname` checks the nssh host catalog, exact matches,
   suggestions, and fuzzy selection. On misses, it returns host-not-found
   immediately.
3. On host-not-found, `internal/app.Run` spawns `nssh inv set <host>` for local
   inventory creation.
4. `connect.ResolveHostForConnect` loads config, finds the nssh host catalog
   entry from YAML config and provider state, resolves inventory group metadata,
   selects a username, and resolves any provider-backed credential.
5. `connect.newConnector` builds an `internal/ssh/connector.Connector` with the
   host alias, optional username, optional secret, host-key policy, and timeout
   config.
6. The connector runs OpenSSH in a PTY, detects prompts, injects credentials only
   when prompted, handles host-key prompts, relays stdio and signals, and emits
   timing markers when enabled.
7. On legacy SSH negotiation failure, nssh can persist compatibility floors
   under the owning provider YAML host `ssh.compatibility` field.
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

`internal/ssh/sshconfig` parses OpenSSH config for import and tests. Runtime
connections should not depend on it.

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
