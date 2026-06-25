# Configuration, Inventory, And Credentials

Audience: agents answering nssh setup, config, inventory, and credential
questions.

## Config Files

Source paths:

- `internal/config/paths.go`
- `internal/config/settings.go`
- `internal/config/include.go`
- `internal/config/inventory.go`
- `internal/config/example_config.yaml`

Default paths:

- Main config: `~/.config/nssh/config.yaml`
- Inventory includes: `~/.config/nssh/inventory/*.yaml`
- Runtime state: `~/.local/state/nssh/`
- Recordings: `~/.local/state/nssh/casts`
- Data/benchmarks: `~/.local/share/nssh/`

Config is YAML. `include: [...]` can appear at the root or under sections.
Included files are merged first; the importing file wins.

Use `nssh self cfg` or the first-run config template for field-level details
instead of repeating the whole schema. Bare `nssh self init` is first-run only;
add provider files later with repeatable `--cred` and `--inv` flags.

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

Local inventory is the implicit `local` provider and writes YAML, usually
`~/.config/nssh/inventory/local.yaml`. Local host groups are stored under each
host as a singular group key such as `custcbb`, with the canonical group ID
remaining `local/custcbb`.
The host `group` key is optional. Ungrouped local hosts are valid and inherit
inventory/provider defaults plus host-level auth, SSH, and highlight settings,
but they do not inherit any group settings.

External inventory providers write non-secret state under
`~/.local/state/nssh/inventory/providers/<name>.json`. Provider groups,
auth mappings, SSH options, and host overrides live in provider-scoped YAML
inventory config.

Current external providers:

- NetBox: configured with `type: netbox`, URL/token settings, and
  provider-owned group match rules.
- Containerlab: configured with `type: containerlab` and a required
  `jump_host`.

Provider-owned group selectors assign discovered objects to canonical groups.
When resolved inventory auth mode is `password`, nssh renders OpenSSH options
that force password-style authentication:
`PreferredAuthentications=keyboard-interactive,password` and
`PubkeyAuthentication=no`. This is applied during connection after SSH defaults,
group options, and host options are merged. Key auth does not add these options.
For containerlab, use `state: [running]` to select any running node; add
`kind: [ceos, vjunos]` only when a group should be limited to specific
node kinds.

## Credential Providers

Source paths:

- `internal/credential/provider.go`
- `internal/credential/sops_age.go`
- `internal/credential/sopsdoc/doc.go`
- `internal/credential/onepassword.go`
- `internal/credential/bitwarden.go`
- `internal/connect/resolve.go`

Supported providers:

- `sops-age`: `sops` CLI with age recipients. The default provider instance
  name is `sops`, and the default file is
  `~/.local/share/nssh/credentials.sops.yaml`.
- `1password`: `op` CLI. Lookups run directly in the foreground unless
  `keepalive: true` requires the runtime agent. If a user-driven lookup finds
  the account signed out, nssh runs `op signin` once and retries the credential
  command.
- `bitwarden`: `bw` CLI, with lazy unlock. `BW_SESSION` is request-scoped unless
  `warm_session: true` requires the runtime agent to retain it in memory.

1Password keepalive is explicit per provider and disabled by default:

```yaml
credential:
  provider:
    op-expedient:
      type: 1password
      vault: Expedient
      keepalive: false
      keepalive_interval: 5m
      keepalive_timeout: 10s
```

Bitwarden warm sessions are also explicit and disabled by default:

```yaml
credential:
  provider:
    bw-work:
      type: bitwarden
      warm_session: false
```

Do not enable either option unless the operator wants the agent to start and
retain that provider access state in memory.

1Password keepalive uses only `op whoami`; it does not run `op signin`.

Auth mappings belong to inventory:

```yaml
inventory:
  providers:
    local:
      type: local
      groups:
        default:
          auth:
            credential_provider: sops
            password_ref: groups.local.default.password
      hosts:
        edge01:
          group: default
          auth:
            credential_provider: op-network
            password_ref: op://Network/edge01/password
            username: netops
```

Password-backed auth mappings need `credential_provider` plus `password_ref` or
`username_ref`. `username` and `username_ref` are optional and mutually
exclusive; key-mode mappings may set only `mode` and `username`.

For SOPS+age, use scalar key paths such as `groups.local.default.password`,
`hosts.edge01.password`, or `expedient.password`. For 1Password, prefer a
literal `username` plus a direct password field reference such as
`op://Network/edge01/password`. Use `username_ref` when treating the SSH username
as sensitive; it remains supported, but it may add provider lookup time.

Resolution order:

1. Host auth override.
2. Inventory provider group auth mapping.
3. Key auth or SSH agent auth without an nssh credential.

If a provider record returns a username that conflicts with the selected SSH
username, nssh skips that credential. This prevents injecting the wrong password
for a different account.

Legacy config tables `credential.host`, `credential.group`, root
`credential.type`, and `sync.sources` are invalid in release/0.3.
