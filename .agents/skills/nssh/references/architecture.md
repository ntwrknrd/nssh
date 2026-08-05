# nssh Architecture

Audience: agents who need nssh architecture, package boundaries, and design
context. Source is authoritative. If this file conflicts with current code, fix
this file after reading the code.

## Product Contract

nssh wraps OpenSSH for operators with many SSH targets. It must preserve normal
OpenSSH behavior while adding:

- smart host lookup and fuzzy selection
- local and external inventory resolved from nssh YAML config and provider state
- provider-backed SSH credentials from SOPS+age, 1Password, or Bitwarden
- request-scoped password injection through isolated askpass channels
- optional asciinema session recording and log management
- typed legacy SSH compatibility fixes under nssh host SSH config
- status, diagnostics, benchmarking, install, reset, and uninstall commands

nssh must not maintain a local credential vault or write decrypted credential
material to disk. The runtime agent may hold provider runtime material in memory
until it stops.

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
- `nssh agent`: background runtime status, stop, and reset.
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

`nssh self init` writes the commented first-run template embedded from
`internal/config/example_config.yaml`. Bare init is first-run only; use
`nssh self init --cred <provider>` and `nssh self init --inv <provider>` to add
provider include files later.

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

The public first-run template is `internal/config/example_config.yaml`. Keep
field-level examples there, not in narrative docs.

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
Local host groups are optional inheritance buckets. A local host without
`group` is still valid; it uses provider/default and host-level settings, and
skips group auth, SSH, and highlight inheritance.
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
Resolved password auth forces OpenSSH password-style auth during connection:
`PreferredAuthentications=keyboard-interactive,password` and
`PubkeyAuthentication=no`. This happens after SSH defaults, group options, and
host options are merged. Key auth does not add those options.

## Credentials

Credential storage is external. nssh stores only auth mappings that tell it
which provider and item reference apply to a host or provider-owned group.

Primary packages:

- `internal/credential`: provider registry and provider implementations.
- `internal/config/inventory.go`: credential and inventory auth config types.
- `internal/connect/resolve.go`: connect-time credential selection.

Supported credential providers:

- `sops-age`: `credential.sopsAgeProvider`, using `sops`; default provider
  instance name is `sops`.
- `1password`: `credential.onePasswordProvider`, using `op`; lookups run
  directly in the foreground unless `keepalive: true` requires the runtime
  agent. A signed-out user-driven lookup may run `op signin` once and retry the
  same credential command.
- `bitwarden`: `credential.bitwardenProvider`, using `bw`; lazy unlock uses a
  request-scoped `BW_SESSION` unless `warm_session: true` requires the runtime
  agent to retain it in memory.

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

Password delivery is prompt-driven through askpass. For a cold, managed
password connection, nssh may begin provider resolution concurrently with
OpenSSH setup to reduce latency. It skips prefetch and askpass setup when an
existing multiplexed control session can satisfy the connection. Unmanaged
proxy transports also disable target password prefetch and autofill.

## Agent Runtime

`internal/agent` is a Unix-domain-socket runtime daemon. It is not a password
manager and must not persist decrypted credentials.

Responsibilities:

- retained-access provider requests for 1Password and Bitwarden
- in-memory Bitwarden `BW_SESSION` handling only for providers with
  `warm_session: true`
- 1Password keepalive only for providers with `keepalive: true`, armed after a
  successful credential lookup and suspended after a failed refresh
- socket path management and stale socket cleanup
- peer credential verification
- idle timeout, max lifetime, stop/reset behavior, and signal handling

Provider execution lives in `internal/credential/providerexec`; foreground
providers choose direct transport unless retained access requires agent
transport. The protocol is in `internal/agent/protocol.go`. Public commands live
in `internal/cli/agent`. The internal stop operation is named `lock` in protocol
terms, but the user-facing commands are `nssh agent stop` and `nssh agent reset`;
there is no public password-manager lock/unlock workflow or manual start command.

Import boundary tests require `internal/agent` to stay below CLI, UI, and SSH
packages.

## Connection Flow

Root SSH-style execution is routed through `internal/connect.ConnectRequest`.

1. `internal/app.PreprocessArgs` maps root SSH-style commands to hidden
   `smart-connect`.
2. `smart-connect` preserves the OpenSSH grammar split: tokens before `HOST`
   are SSH args, tokens after `HOST` are a remote command.
3. `connect.ResolveHostname` checks the nssh host catalog, exact matches,
   suggestions, and fuzzy selection. On misses, it returns host-not-found
   immediately.
4. On host-not-found, `internal/app.Run` spawns `nssh inv set <host>` for local
   inventory creation.
5. `connect.ResolveHostForConnect` loads config, finds the nssh host catalog
   entry from YAML config and provider state, resolves inventory group metadata,
   selects a username, and resolves any provider-backed credential.
6. Interactive sessions build an `internal/ssh/connector.Connector` with the
   host alias, optional username, rendered SSH policy, host-key policy, timeout
   config, and any askpass environment prepared by the connection layer.
7. Interactive password sessions start request-scoped target and, when needed,
   managed-proxy askpass servers. OpenSSH still runs in a PTY for terminal I/O,
   signals, timing, and fallback prompt handling, but password bytes travel
   through the separately installed `nssh-askpass` helper and authenticated
   local sockets.
8. Remote-command sessions run OpenSSH without a local PTY through the captured
   runner. stdout and stderr are captured separately; stdout may be highlighted
   after the command completes.
9. Password-backed remote commands use the same askpass design without a local
   PTY. The server supports the bounded prompt sequence needed by OpenSSH while
   keeping passwords out of argv, environment values, and temporary files.
10. A single-hop `ProxyJump` that resolves to another nssh inventory host is
   rendered as a managed `ProxyCommand`. Target and proxy credentials use
   separate askpass variables and sockets. Arbitrary or nested OpenSSH proxy
   configurations remain unmanaged and do not receive nssh password autofill.
11. New or changed host keys are scanned before password-backed connection
   setup. Accept-once uses a temporary pinned `known_hosts`; accept-always adds
   a new key or explicitly replaces the changed entry after user confirmation.
12. On legacy SSH negotiation failure, nssh can persist compatibility floors
   under the owning provider YAML host `ssh.compatibility` field.
13. Optional recording wraps the outer interactive connection before connector
    execution.

SCP uses the same host and credential resolver through `internal/cli/cp`.

## SSH Connector

`internal/ssh/connector` owns PTY lifecycle, fallback prompt detection, host-key
handling, timing, and stdio relay. `internal/ssh/askpass` owns the authenticated
password channel used by normal 0.3 connections. Neither package should import
higher-level CLI, UI, recording, or agent packages.

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

The `nssh log` commands live in `internal/cli/log`. Archive maintenance is an
explicit `nssh log archive` command; automation belongs in cron, launchd,
systemd timers, or another operator-owned scheduler. Archive policy lives in
`internal/recording`, not `internal/agent`.

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
