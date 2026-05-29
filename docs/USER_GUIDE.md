# nssh User Guide

`nssh` wraps SSH with inventory management, credential injection, session
recording, and a small set of installation tools.

## First Setup

Initialize local state and shell integration:

```bash
nssh self init
nssh self status
```

New installs use:

- `~/.config/nssh/config.toml` for nssh configuration
- `~/.local/share/nssh/credentials.age` for the default age-backed credential provider
- `~/.local/state/nssh/` for runtime state
- `~/.ssh/nssh.d/` for nssh-managed SSH config include files

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

Use `-s`/`--select` to filter inventory rows. Plain terms search visible row
values; `field:value` terms are exact matches. Multiple terms are combined with
AND.

```bash
nssh inv list -s cbb
nssh inv list -s group:cbb
nssh inv list -s 'group:cbb user:admin'
nssh inv list -s provider:netbox-prod
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
Use `nssh inv status` to inspect cache age, route ownership, and output files;
use `nssh inv status --refresh` to refresh external-provider caches.

External provider setup is not managed by CLI CRUD. Configure providers in
`config.toml`:

```toml
[inventory]
default_group = "lab"

# Empty table is enough to declare a local group.
[inventory.group.lab]

[inventory.provider.netbox-prod]
type = "netbox"

[inventory.provider.netbox-prod.config]
base_url = "https://netbox.example.com"
token_env = "NETBOX_TOKEN"

[[inventory.provider.netbox-prod.route]]
group = "lab"
```

## Credentials

Credentials are managed with `nssh cred`. Credential records attach only to a
host or a group.

```bash
nssh cred status
nssh cred set switch1 --username admin
nssh cred link switch1 --ref "Existing 1Password Item"
nssh cred get switch1
nssh cred rm switch1

nssh cred set -g lab --username admin
nssh cred link -g lab --ref "Network Shared Admin"
nssh cred get -g lab
nssh cred rm -g lab
```

Credential resolution order during connect:

1. Host credential override
2. Group credential fallback
3. SSH config defaults and key authentication

`nssh cred get <host>` shows the effective credential source for that host. If
no host override exists, it reports the inventory group and any group
fallback credential that would be used.

The active credential backend is selected globally:

```toml
[credential]
type = "age"
```

For 1Password:

```toml
[credential]
type = "1password"

[credential.config]
account = ""
vault = "Network"
```

1Password items are deterministic:

- `nssh host <host>`
- `nssh group <group>`

Use `cred link` when an existing 1Password item should be the credential source
instead of a deterministic nssh-owned item. `--ref` may be an item name/ID or an
`op://` secret reference. If `--ref` is an `op://.../password` reference, nssh
uses the sibling `op://.../username` reference unless `--username` or
`--username-ref` is provided.

```bash
nssh cred link -g lab --ref "Network Shared Admin"
nssh cred link switch1 --ref "op://Network/Switch 1/password"
nssh cred link switch1 --ref "op://Network/Switch 1/password" --username netops
```

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

## Vault Session

Unlock credentials for the current session:

```bash
nssh unlock
nssh lock
```

Headless unlock is available when needed:

```bash
printf '%s\n' "$PASSPHRASE" | nssh unlock --stdin
```

## Session Logs

If recording is enabled, manage recordings with `nssh log`:

```bash
nssh log list
nssh log play <id>
nssh log export <id>
nssh log delete <id>
```
