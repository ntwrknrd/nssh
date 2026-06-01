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
nssh inv set <host> --hostname <addr> --user <user> -g local/<group>
nssh inv rm <host>
nssh inv import ./hosts.csv --group local/<group>
nssh inv set -g local/<group>
nssh inv rm -g local/<group>
nssh inv status
nssh inv refresh
nssh inv refresh local
```

Local inventory is the implicit `local` provider and writes
`~/.ssh/nssh.d/provider_local.conf`. Local host groups are stored in SSH config
comments as canonical IDs such as `local/custcbb`.

External inventory providers write provider-owned include files named
`~/.ssh/nssh.d/provider_<name>.conf` and non-secret state under
`~/.local/state/nssh/inventory/providers/<name>.json`.

Current external providers:

- NetBox: configured with `type = "netbox"`, URL/token settings, and
  provider-owned group match rules.
- Containerlab: configured with `type = "containerlab"` and a required
  `jump_host`.

Provider-owned group selectors assign discovered objects to canonical groups.
nssh defaults to password SSH preferences when the provider group has password
auth, otherwise key.
For containerlab, use `state = ["running"]` to select any running node; add
`kind = ["ceos", "vjunos"]` only when a group should be limited to specific
node kinds.

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
[inventory.provider.local]
type = "local"

[inventory.provider.local.group.default]
auth = { credential_provider = "pass-local", password_ref = "nssh/groups/default" }

[inventory.host.edge01]
auth = { credential_provider = "op-network", password_ref = "nssh host edge01", username = "netops" }
```

Every set auth mapping needs `credential_provider` and either `password_ref` or
`username_ref`. `username` and `username_ref` are optional and mutually
exclusive.

For faster password-auth connections, prefer a literal `username` plus a direct
password field reference such as `op://Network/edge01/password`. Use
`username_ref` when treating the SSH username as sensitive; it remains
supported, but it costs an extra external provider call, so time to first
prompt is slower. Item-base refs can also add provider calls. Each `op`,
`pass`, or `bw` call adds connection time.

Resolution order:

1. Host auth override.
2. Inventory provider group auth mapping.
3. SSH config and key auth without an nssh credential.

If a provider record returns a username that conflicts with the selected SSH
username, nssh skips that credential. This prevents injecting the wrong password
for a different account.

Legacy config tables `credential.host`, `credential.group`, root
`credential.type`, and `sync.sources` are invalid in release/0.3.
