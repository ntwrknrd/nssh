# nssh User Guide

`nssh` wraps SSH with inventory management, provider-backed credential
injection, session recording, and installation tools.

## First Setup

Initialize local state and shell integration:

```bash
nssh self init
nssh self status
```

New installs use:

- `~/.config/nssh/config.toml` for nssh configuration
- `~/.local/state/nssh/` for runtime state and recordings
- `~/.ssh/nssh.d/` for nssh-managed SSH config include files
- Pass, 1Password, or Bitwarden for credential storage

`~/.ssh/config` should include the managed inventory directory:

```sshconfig
Include ~/.ssh/nssh.d/*
```

## Inventory

Inventory is managed with `nssh inv`. SSH config remains the canonical backing
store.

```bash
nssh inv list
nssh inv get switch1
nssh inv set switch1 --hostname 10.0.0.10 --user admin -g lab
nssh inv rm switch1
nssh inv import ./hosts.csv --group lab
nssh inv doctor
nssh inv status
```

Groups are inventory placement buckets:

```bash
nssh inv list -g
nssh inv get -g lab
nssh inv set -g lab
nssh inv rm -g lab
```

Local-provider hosts are written to `~/.ssh/nssh.d/provider_local.conf`.
External-provider hosts are generated into provider-owned files, such as
`~/.ssh/nssh.d/provider_netbox-prod.conf`.

Set the SSH login user on the inventory group when every host in that group
uses the same account. Provider refresh writes this into generated SSH config.

```toml
[inventory.group.custcbb]
default_user = "netops"
```

## Credentials

Credentials are stored and inspected in the selected password manager. `nssh`
stores only inventory auth mappings: which provider item should be used for a
host or group.

```bash
nssh inv set switch1 --credential-provider op-network --credential-ref "Existing 1Password Item"
nssh inv set switch1 --credential-clear
nssh inv get switch1
nssh inv get -g lab
```

Credential resolution order during connect:

1. Host auth override
2. Inventory group auth mapping
3. SSH config defaults and key authentication

Provider routes also control the SSH auth directives written to generated SSH
config. Set `auth_mode = "password"` for routed network gear that should prefer
password or keyboard-interactive auth. Set `auth_mode = "key"` for routed
servers that should use SSH keys. When omitted, routes default to password auth
if the destination group has an auth mapping, otherwise key auth.

`nssh inv get <host>` shows the effective auth mapping for that host. It never
prints the target password.

Pass is the default provider:

```toml
[credential]
default_provider = "pass-local"

[credential.provider.pass-local]
type = "pass"

[credential.provider.pass-local.config]
command = "pass"
prefix = "nssh"

[inventory.group.default.auth]
provider = "pass-local"
ref = "nssh/groups/default"
```

The auth mapping points at the password record. The SSH username belongs to
inventory group config unless the provider item must override it.

1Password and Bitwarden can be added as named providers:

```toml
[credential.provider.op-network]
type = "1password"

[credential.provider.op-network.config]
vault = "Network"
session = "agent"

[credential.provider.bw-team]
type = "bitwarden"

[credential.provider.bw-team.config]
session = "external"
```

Provider authentication stays with the provider. Pass uses GPG/pass, 1Password
uses `op`, and Bitwarden uses `bw`; nssh does not store provider tokens.

There is no automated migration from prior local encrypted credential files.
Create the equivalent records in the password manager, then map inventory
groups or host overrides to those provider item refs.

## Connecting

Once a host exists in SSH config, connect with the host name:

```bash
nssh switch1
nssh admin@switch1
nssh connect switch1 -p 2222
```

If lookup misses, `nssh` refreshes eligible stale inventory providers once,
retries lookup, and only then offers to create a local inventory host with
`nssh inv set`.

`nssh cp` uses the same host and credential resolution path as SSH:

```bash
nssh cp switch1:/tmp/file ./file
nssh cp ./file switch1:/tmp/file
```

## Agent

`nssh agent` manages the background runtime used for provider sessions and
metadata cache.

```bash
nssh agent status
nssh agent stop
nssh agent restart
nssh agent doctor
```

There is no primary unlock/lock workflow. Provider authentication happens when
the provider CLI requires it. For `session = "agent"` providers, nssh starts
the runtime agent on the first provider request by default. Set
`agent.auto_start = false` to require manual startup with `nssh agent restart`.

## Session Logs

If recording is enabled, manage recordings with `nssh log`:

```bash
nssh log list
nssh log play <id>
nssh log export <id>
nssh log delete <id>
```
