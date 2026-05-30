# Configuration, Inventory, And Credentials

Audience: agents answering nssh setup, config, inventory, and credential
questions.

## Config Files

Source paths:

- `internal/config/paths.go`
- `internal/config/settings.go`
- `internal/config/include.go`
- `internal/config/inventory.go`
- `docs/examples/config/config.example.toml`

Default paths:

- Main config: `~/.config/nssh/config.toml`
- Managed SSH includes: `~/.ssh/nssh.d/`
- Runtime state: `~/.local/state/nssh/`
- Recordings: `~/.local/state/nssh/casts`
- Data/backups/benchmarks: `~/.local/share/nssh/`

Config is TOML. `include = [...]` can appear at the root or under tables.
Included files are merged first; the importing file wins.

Use `nssh self cfg` or the example config for field-level details instead of
repeating the whole schema.

## Inventory

Commands:

```bash
nssh inv list
nssh inv get <host>
nssh inv set <host> --hostname <addr> --user <user> -g <group>
nssh inv rm <host>
nssh inv import ./hosts.csv --group <group>
nssh inv get -g <group>
nssh inv set -g <group>
nssh inv rm -g <group>
nssh inv status
nssh inv doctor
```

Local inventory is the implicit `local` provider and writes
`~/.ssh/nssh.d/provider_local.conf`. Local host groups are stored in SSH config
comments.

External inventory providers write provider-owned include files named
`~/.ssh/nssh.d/provider_<name>.conf` and non-secret state under
`~/.local/state/nssh/inventory/providers/<name>.json`.

Current external providers:

- NetBox: configured with `type = "netbox"`, URL/token settings, and route
  match rules.
- Containerlab: configured with `type = "containerlab"` and a required
  `jump_host`.

Routes assign discovered objects to groups. `auth_mode = "password"` renders
password and keyboard-interactive SSH preferences. `auth_mode = "key"` disables
password auth. If omitted, nssh defaults to password when the group has auth,
otherwise key.

## Credential Providers

Source paths:

- `internal/credential/provider.go`
- `internal/credential/pass.go`
- `internal/credential/onepassword.go`
- `internal/credential/bitwarden.go`
- `internal/connect/resolve.go`

Supported providers:

- `pass`: local password-store compatible CLI, default provider name
  `pass-local`.
- `1password`: `op` CLI, with `session = "agent"` by default.
- `bitwarden`: `bw` CLI, with provider auth state owned externally.

Auth mappings belong to inventory:

```toml
[inventory.group.default]
auth = { provider = "pass-local", ref = "nssh/groups/default" }

[inventory.host.edge01]
auth = { provider = "op-network", ref = "nssh host edge01", username = "netops" }
```

Every set auth mapping needs `provider` and `ref`. `username` and
`username_ref` are optional and mutually exclusive.

Resolution order:

1. Host auth override.
2. Inventory group auth mapping.
3. SSH config and key auth without an nssh credential.

If a provider record returns a username that conflicts with the selected SSH
username, nssh skips that credential. This prevents injecting the wrong password
for a different account.

Legacy config tables `credential.host`, `credential.group`, root
`credential.type`, and `sync.sources` are invalid in release/0.3.
