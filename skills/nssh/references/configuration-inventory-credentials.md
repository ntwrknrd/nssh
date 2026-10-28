# Configuration, Inventory, And Credentials

Audience: agents answering nssh setup, config, inventory, and credential
questions.

## Config Files

Default paths:

- Main config: `~/.config/nssh/config.yaml`
- Inventory includes: `~/.config/nssh/inventory/*.yaml`
- Runtime state: `~/.local/state/nssh/`
- Recordings: `~/.local/state/nssh/casts`
- Data: `~/.local/share/nssh/` (SOPS credentials, backups, and benchmarks)
- Benchmarks: `~/.local/share/nssh/benchmarks/`

Config is YAML. `include: [...]` can appear at the root or under sections.
Included files are merged first; the importing file wins. Unmatched include
globs are allowed; missing explicitly named files remain errors.

Use `nssh self cfg` or the first-run config template for field-level details
instead of repeating the whole schema. Bare `nssh self init` is first-run only;
add provider files later with repeatable `--cred` and `--inv` flags.

`nssh self import ssh-config` is a reviewed, one-way import from
`~/.ssh/config` and its includes. It can populate root SSH defaults and local
inventory hosts, but it does not import 0.2 vault credentials or convert TOML.

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

nssh writes a provider's hosts and groups to the included file that declares
that provider's `type`, even when the root config also has a block for the
provider. After a write, it removes the root config's now-redundant copy of
that host, group, or provider so the root cannot shadow the included file.

Host lookup names rank in this order: full host names, explicit aliases, and
then derived short names (the first DNS label). A derived short name that
several hosts share identifies none of them; use the full name or give one host
an explicit alias. When hosts share a full name or explicit alias, the
lexically first host wins. `nssh inv set` skips a derived
short alias that already identifies another host and rejects an explicit
`--alias` collision.

A host under `inventory.providers.<provider>.hosts` overlays the provider
object with that name; `nssh inv` commands and connections show it as one
host. A local inventory host shadows a provider object with the same name, so
local settings win without a refresh. To keep a device provider-managed while
changing its settings, use a provider host override instead of a local host.
`nssh inv status <provider>` lists shadowed objects and overrides that match
no provider object, and `nssh inv get` shows what a local host shadows.

External inventory providers write non-secret state under
`~/.local/state/nssh/inventory/providers/<name>.json`. Provider groups,
auth mappings, SSH options, and host overrides live in provider-scoped YAML
inventory config.

An inventory host may use `ProxyJump` in its resolved SSH options. When that
single proxy target resolves to another nssh inventory host, nssh builds a
managed proxy and resolves the proxy host's auth independently. Nested proxies,
comma-separated jumps, and explicit `ProxyCommand` values stay OpenSSH-native.

Current external providers:

- NetBox: configured with `type: netbox`, URL/token settings, and
  provider-owned group match rules.
- Containerlab: configured with `type: containerlab` and a required
  `jump_host`.

Provider-owned group selectors assign discovered objects to canonical groups.
A provider group's top-level `domain_suffix` qualifies short object names:
when it lists exactly one suffix, an object assigned to that group whose name
has no domain and no separate address becomes `<name><suffix>`, with the short
name kept as a lookup name. Selection still uses the object's original name, so
match such groups on fields other than `match.domain_suffix`.
When resolved inventory auth mode is `password`, nssh renders OpenSSH options
that force password-style authentication:
`PreferredAuthentications=keyboard-interactive,password` and
`PubkeyAuthentication=no`. This is applied during connection after SSH defaults,
group options, and host options are merged. Key auth sets
`PreferredAuthentications=publickey` and `PubkeyAuthentication=yes`, so it does
not fall back to login passwords. Explicit command-line SSH options retain
precedence over inventory policy.

nssh bypasses SSH config files with `-F none`. Configure `IdentityFile`,
`IdentityAgent`, and `IdentitiesOnly` in the host's `ssh.options`. Interactive
key setup offers to read the target's effective SSH identity settings and
preview them before importing. Reading those settings evaluates SSH config,
including any `Match exec` directives.

Local host creation stores auth in the owning provider inventory file, ensures
that file is included, and verifies discovery after reloading. It does not
create a root host auth override. Saving after a failed connection test requires
an explicit choice and reports the host as unverified.
For containerlab, use `state: [running]` to select any running node; add
`kind: [ceos, vjunos]` only when a group should be limited to specific
node kinds.

## Credential Providers

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

SOPS providers can set `age_key_command` to a nonsecret lookup command, such as
an OS Keychain lookup. SOPS executes the command and consumes the returned age
identity internally; do not put the identity itself in the command or YAML.
`age_key_command` and `age_key_file` are mutually exclusive. The command may
also be placed under the provider's `config` table; the flat field takes
precedence.

```yaml
credential:
  provider:
    sops-personal:
      type: sops-age
      file: ~/.local/share/nssh/personal.sops.json
      age_key_command: /usr/bin/security find-generic-password -s com.example.sops.age -a alice -w
```

The setting overrides `SOPS_AGE_KEY_CMD` only in that provider's SOPS process,
clearing inherited `SOPS_AGE_KEY` and `SOPS_AGE_KEY_FILE` there. Other providers
and the parent shell are unchanged. Without this setting, existing SOPS
identity/environment behavior is preserved. SOPS still owns its default
identity-file behavior. Commands that fail retain the usual bounded lookup,
safe error and manual/unattended fallback policy; private command output is
never included in nssh diagnostics. This does not unlock Keychain or provision
an identity. A command-backed provider needs an existing encrypted file and
externally managed identity; setup must not generate a plaintext key file.

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

Inventory auth is merged field by field in this order, with each later layer
overriding only the fields it sets:

1. Root `inventory.auth` defaults.
2. `inventory.providers.<provider>.auth` defaults.
3. The provider-owned group auth mapping.
4. A root `inventory.hosts.<host>.auth` override.
5. A provider-scoped `inventory.providers.<provider>.hosts.<host>.auth`
   override.

`inventory.auth.fallback` defaults to `manual`; set it to `fail` to disable
native manual password fallback for terminal SSH. Each host's `auth.fallback`
overrides that default through the same inheritance, including managed proxy
hosts independently. `auth.lookup_timeout` bounds credential lookup and defaults
to `10s`; a positive duration overrides it and zero inherits. Remote commands,
REPL/host lists, SCP, non-terminal SSH and `BatchMode=yes` always fail without
manual prompting. Automatic lookups use existing provider sessions rather than
opening provider sign-in/unlock prompts.

Either host layer can set `auth_disabled: true` to suppress credential
resolution. A resolved `mode: key` also clears inherited password-provider
fields. With no resolved password mapping, OpenSSH key or agent authentication
remains available.

If a provider record returns a username that conflicts with the selected SSH
username, nssh skips that credential. This prevents injecting the wrong password
for a different account.

Legacy config tables `credential.host`, `credential.group`, root
`credential.type`, and `sync.sources` are invalid in release/0.3.
