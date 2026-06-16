# nssh YAML Config Spec

This is the approved config schema spec for the `release-0.3` YAML cutover. It
defines the target YAML shape, merge behavior, OpenSSH import boundary,
compatibility fix catalog, and runtime verbosity behavior for implementation.

## Goals

- Make nssh YAML the only runtime config format.
- Keep the root config lean.
- Keep provider-scoped inventory in provider-scoped files.
- Keep per-host config explicit and easy to find.
- Preserve common OpenSSH behavior as typed nssh config.
- Keep uncommon OpenSSH directives behind an explicit escape hatch.

## File Layout

```text
~/.config/nssh/
  config.yaml
  credentials/
    op-expedient.yaml
    pass-local.yaml
  inventory/
    local.yaml
    netbox-prod.yaml
    nre-netlab01.yaml
```

## Root Config

`config.yaml` should stay small and describe global behavior plus includes.

```yaml
include: [credentials/*.yaml, inventory/*.yaml]

agent:
  auto_start: true
  idle_timeout: 4h
  activity_increment: 30m
  max_lifetime: 24h

ssh:
  connection:
    timeout: 30s
    password_timeout: 10s
    idle_timeout: 0s

  security:
    host_key_policy: pin
    accept_once_mode: pin
    compat_persist_probes: false

  defaults:
    identities_only: true
    identity_agent:
      path: ~/Library/Group Containers/2BUA8C4S2C.com.1password/t/agent.sock
    identity_files:
      - ~/.ssh/ed25519-1Password-Personal.pub
    server_alive_interval: 240s
    server_alive_count_max: 2
    set_env:
      TERM: xterm-256color

logging:
  audit:
    enabled: true
    max_size: 10MB

  session:
    enabled: true
    append_mode: true
    dir: ~/.local/state/nssh/casts
    auto_export_txt: true
    title_format: "nssh:{host}"
    idle_time_limit: 0
    idle_time_limit_mode: play
    exclude_hosts:
      - lab-*
    include_hosts: []
    archive:
      enabled: false
      dir: ~/.local/state/nssh/archives
      jitter: 30m
      max_bundles: 12
      max_run_bytes: 0
      min_age: 720h
```

### Root Config Notes

- `ssh.defaults` is for imported global SSH defaults and nssh-wide defaults.
- These defaults must be applied by nssh at argv render time.
- OpenSSH should still be invoked with `-F none`; `~/.ssh/config` is not read at
  runtime.

## Credentials

`credentials/op-expedient.yaml`

```yaml
credentials:
  op-expedient:
    type: 1password
    session: agent
    account: ""
    vault: Expedient
```

`credentials/pass-local.yaml`

```yaml
credentials:
  pass-local:
    type: pass
    session: external
    command: pass
    prefix: nssh
```

### Credential Notes

- Provider names stay top-level map keys under `credentials`.
- Backend-specific settings are not hidden under a generic `config` key.
- Strict validation decides which fields are valid for each provider type.

## NetBox Inventory

`inventory/netbox-prod.yaml`

```yaml
inventory:
  providers:
    netbox-prod:
      type: netbox
      config:
        url_env: NETBOX_URL
        token_env: NETBOX_TOKEN

      groups:
        cbb:
          match:
            domain_suffix:
              - .expedient.com
            manufacturer:
              - Arista
              - Juniper
            tenant:
              - Expedient
            status:
              - active
          auth:
            mode: password
            username: chris.jones
            credential_provider: op-expedient
            password_ref: op://Expedient/bdmuxl2pscoecl7gsdt5geodzu/password

        custcbb:
          match:
            domain_suffix:
              - .custcbb.local
            manufacturer:
              - Arista
              - Juniper
            tenant:
              - Expedient
            status:
              - active
          auth:
            mode: password
            username: chris.jones
            credential_provider: op-expedient
            password_ref: op://Expedient/bdmuxl2pscoecl7gsdt5geodzu/password

      hosts:
        701-sw37r103c608.expedient.com:
          group: cbb
          aliases:
            - 701-sw37
            - 701-sw37r103c608
          ssh:
            compat:
              - legacy-kex
              - legacy-macs
            options:
              Ciphers: aes256-ctr
```

### NetBox Notes

- Provider refresh writes provider state only.
- Group auth affects runtime resolution immediately after config load.
- Provider-backed `hosts` entries are operator-authored overlays for discovered
  hosts.
- Discovered provider hosts select one group. Explicit `hosts.<name>.group`
  wins over implicit matching.
- No generated SSH config file is produced or consumed by `nssh connect`.

## Local Inventory

`inventory/local.yaml`

```yaml
inventory:
  providers:
    local:
      type: local

      groups:
        homelab:
          match:
            domain_suffix:
              - .lan.ntwrknrd.net
          auth:
            mode: key
            username: cj

        custcbb:
          match:
            domain_suffix:
              - .custcbb.local
          auth:
            mode: password
            username: chris.jones
            credential_provider: op-expedient
            password_ref: op://Expedient/bdmuxl2pscoecl7gsdt5geodzu/password

      hosts:
        core-sw1:
          group: homelab
          hostname: core-sw1.lan.ntwrknrd.net
          aliases:
            - core
            - core-sw1.lan
          port: 22

        pla-mgmt-sw1:
          group: custcbb
          hostname: pla-mgmt-sw1.custcbb.local
          aliases:
            - pla-mgmt
          port: 22
```

### Local Inventory Notes

- Local hosts live under the local provider `hosts` key, not under groups.
- A local group can define match rules and default auth.
- Local hosts select one group with `group`.
- Explicit local hosts should not require provider state.

## Provider Host Entry With Compatibility Fixes

Provider-backed compatibility fixes live in provider-level `hosts`.

```yaml
inventory:
  providers:
    netbox-prod:
      groups:
        cbb:
          auth:
            mode: password
            username: chris.jones
            credential_provider: op-expedient
            password_ref: op://Expedient/bdmuxl2pscoecl7gsdt5geodzu/password

      hosts:
        701-sw37r103c608.expedient.com:
          group: cbb
          aliases:
            - 701-sw37
            - 701-sw37r103c608
          ssh:
            compat:
              - legacy-kex
              - legacy-macs
            options:
              Ciphers: aes256-ctr
```

### Host Entry Notes

- Provider-backed `hosts` entries are keyed by canonical hostname.
- For discovered providers, `hosts` entries do not create inventory; they only
  overlay discovered provider facts.
- For the local provider, `hosts` entries are the inventory.
- `group` is singular for all providers.
- For discovered providers, implicit group matching must choose one group. If
  multiple groups match, file order decides and the resolver should emit a
  warning or debug-visible event.
- For local providers, `hosts.<name>.group` is required.
- Compatibility fixes are operator policy and belong in nssh config.
- `ssh.options` is an explicit escape hatch for OpenSSH options nssh has not
  modeled as typed fields.

## Resolution Merge Order

Runtime resolution should apply config in a fixed order.

```text
global ssh.defaults
-> provider defaults
-> discovered provider facts or local host identity
-> selected group defaults
-> provider.hosts.<host> per-host config
-> CLI flags
```

For local hosts, `hosts.<name>` supplies identity fields early, but its config
fields still override selected group defaults later in the merge.

## Runtime Verbosity

Verbosity should be first-class CLI state, not raw OpenSSH argv config.

```text
nssh -v <host>     -> nssh debug
nssh -vv <host>    -> nssh debug + ssh -v
nssh -vvv <host>   -> nssh debug + ssh -vv
nssh -vvvv <host>  -> nssh debug + ssh -vvv
```

`-v` remains nssh debugging. Additional `v` levels add OpenSSH transport
verbosity, capped at `ssh -vvv`.

## Imported SSH Defaults Example

An import from `~/.ssh/config` should translate common global defaults into
typed nssh fields.

```yaml
ssh:
  defaults:
    identities_only: true
    identity_agent:
      path: ~/Library/Group Containers/2BUA8C4S2C.com.1password/t/agent.sock
    identity_files:
      - ~/.ssh/ed25519-1Password-Personal.pub
    control_master: auto
    control_persist: 12h
    control_path: ~/.ssh/sockets/%r@%h:%p
    server_alive_interval: 240s
    server_alive_count_max: 2
    ciphers:
      - chacha20-poly1305@openssh.com
      - aes256-gcm@openssh.com
      - aes256-ctr
    set_env:
      TERM: xterm-256color
```

### Imported Defaults Notes

- These defaults are nssh-owned after import.
- `nssh connect` still uses `-F none`.
- Import should translate common deterministic OpenSSH directives into typed
  nssh fields.
- Import should preserve unsupported directives in `ssh.options` only when they
  are simple one-to-one OpenSSH `-o Key=Value` options.
- Import should warn and skip `Match`, `Include`, shell-expanded conditionals,
  `CanonicalizeHostname`, and anything with ordering or conditional behavior
  nssh cannot preserve honestly.
- `release-0.3` should not include a raw argv escape hatch.

### Import Typed Field Boundary

Import these OpenSSH directives as typed fields for `release-0.3`.

```text
HostName
User
Port
ProxyJump
ProxyCommand
IdentityAgent
IdentityFile
CertificateFile
IdentitiesOnly
ForwardAgent
LocalForward
RemoteForward
SetEnv
RemoteCommand
ServerAliveInterval
ServerAliveCountMax
ConnectTimeout
ControlMaster
ControlPersist
ControlPath
Ciphers
MACs
KexAlgorithms
HostKeyAlgorithms
PubkeyAcceptedAlgorithms
```

## SSH Option Coverage Example

Common OpenSSH directives should become typed nssh fields when they affect
connection behavior directly.

```yaml
ssh:
  proxy_jump: bastion01
  proxy_command: ssh -W %h:%p bastion01
  identities_only: true
  identity_agent:
    path: ~/Library/Group Containers/2BUA8C4S2C.com.1password/t/agent.sock
  identity_files:
    - ~/.ssh/ed25519-1Password-Personal.pub
  certificate_files:
    - ~/.ssh/ed25519-1Password-Personal-cert.pub
  forward_agent: false
  local_forwards:
    - bind: 127.0.0.1:15432
      target: db.internal:5432
  remote_forwards:
    - bind: 127.0.0.1:18080
      target: localhost:8080
  set_env:
    TERM: xterm-256color
  remote_command: show version
  options:
    Compression: "yes"
```

### SSH Option Coverage Notes

- Typed fields should cover the common directives operators expect from
  OpenSSH config.
- `ssh.options` is only for directives nssh does not model yet.
- Import should preserve unsupported directives in `ssh.options` when a
  one-to-one OpenSSH option is safe, and warn otherwise.
- Host and group config can use the same `ssh` shape; merge order decides the
  final resolved value.

## Resolved Connection Sketch

For `nssh 701-sw37`, the runtime resolver should produce a single object with
the final decision.

```yaml
query: 701-sw37
canonical_name: 701-sw37r103c608.expedient.com
hostname: 701-sw37r103c608.expedient.com
aliases: [701-sw37, 701-sw37r103c608]
provider: netbox-prod
group: netbox-prod/cbb
port: 22
username: chris.jones
auth:
  mode: password
  credential_provider: op-expedient
  password_ref: op://Expedient/bdmuxl2pscoecl7gsdt5geodzu/password
ssh:
  compat:
    - legacy-kex
    - legacy-macs
  identity_agent:
    path: ~/Library/Group Containers/2BUA8C4S2C.com.1password/t/agent.sock
```

## Projected Current Config

This section projects the current local TOML config into the proposed YAML
shape. It is included to test the schema against real usage.

### Current Root Projection

```yaml
include: [credentials/*.yaml, inventory/*.yaml]

agent:
  idle_timeout: 4h
  activity_increment: 30m
  max_lifetime: 8h

logging:
  session:
    enabled: true
    window_size: 145x30
    auto_export_txt: true

ssh:
  security:
    host_key_policy: tofu
```

### Current Credential Projection

```yaml
credentials:
  op-expedient:
    type: 1password
    session: agent
    vault: Expedient

  op-private:
    type: 1password
    session: agent
    vault: Private
```

### Current Local Inventory Projection

```yaml
inventory:
  providers:
    local:
      type: local

      groups:
        custcbb:
          match:
            domain_suffix:
              - .custcbb.local
          auth:
            mode: password
            username: chris.jones
            credential_provider: op-expedient
            password_ref: op://Expedient/bdmuxl2pscoecl7gsdt5geodzu/password

        homelab:
          match:
            domain_suffix:
              - .lan.ntwrknrd.net

      hosts:
        nre-netlab01:
          group: homelab
          auth:
            mode: key
            username: nre

        rpi-a:
          group: homelab
          auth:
            mode: key
            username: cj

        rpi-b:
          group: homelab
          auth:
            mode: key
            username: cj

        rpi-c:
          group: homelab
          auth:
            mode: key
            username: cj
```

### Current Containerlab Projection

```yaml
inventory:
  providers:
    nre-netlab01:
      type: containerlab
      config:
        jump_host: nre-netlab01
        sudo: false
        strict_host_key_checking: false

      groups:
        ceos:
          match:
            kind: [ceos, arista_ceos]
            state: [running]
          auth:
            mode: password
            credential_provider: op-expedient
            username_ref: op://Expedient/nssh-containerlab-ceos/username
            password_ref: op://Expedient/nssh-containerlab-ceos/password

        juniper-crpd:
          match:
            kind: [juniper_crpd]
            state: [running]
          auth:
            mode: password
            credential_provider: op-expedient
            username_ref: op://Expedient/nssh-containerlab-juniper-crpd/username
            password_ref: op://Expedient/nssh-containerlab-juniper-crpd/password

        vjunos:
          match:
            kind: [vjunos, juniper_vjunosrouter]
            state: [running]
          auth:
            mode: password
            credential_provider: op-expedient
            username_ref: op://Expedient/nssh-containerlab-vjunos/username
            password_ref: op://Expedient/nssh-containerlab-vjunos/password

        linux:
          match:
            kind: [linux]
            state: [running]
          auth:
            mode: password
            credential_provider: op-expedient
            username_ref: op://Expedient/nssh-containerlab-linux/username
            password_ref: op://Expedient/nssh-containerlab-linux/password
```

## Review Questions

- Is `ssh.defaults` the right home for imported global SSH behavior?
- Should `hosts` be the single provider-level key for both explicit local hosts
  and discovered-provider per-host overlays?
- Should all providers use singular group inheritance?
- Is `ssh.options` as a map strict enough, or should the escape hatch be a raw
  argv list to preserve exact OpenSSH ordering?
- Should compatibility fixes be user-visible names like `legacy-kex`, or exact
  OpenSSH option fragments?

## Recommended Answers

These are the approved schema decisions for implementation.

### Keep Global SSH Defaults Under `ssh.defaults`

Use `ssh.defaults` for nssh-wide OpenSSH behavior imported from global
`~/.ssh/config` settings. It is short, obvious, and keeps connection policy near
`ssh.connection` and `ssh.security`.

Do not create an `openssh` root namespace unless nssh grows multiple transports
with materially different config. For now OpenSSH is the transport, but nssh is
the product.

### Use One Provider-Level `hosts` Key

Use `inventory.providers.<provider>.hosts` for both local inventory and
discovered-provider per-host overlays.

For discovered providers such as NetBox, `hosts` entries are overlays and do not
create inventory:

```yaml
inventory:
  providers:
    netbox-prod:
      hosts:
        701-sw37r103c608.expedient.com:
          ssh:
            compat:
              - legacy-kex
```

For the local provider, `hosts` entries are the inventory and each host selects
one group:

```yaml
inventory:
  providers:
    local:
      hosts:
        core-sw1:
          group: homelab
          hostname: core-sw1.lan.ntwrknrd.net
```

This avoids having both `hosts` and `host_overrides` for the same conceptual
thing: operator-authored per-host config. Provider type decides whether that
config creates inventory or overlays discovered inventory.

### Use Singular Group Inheritance

Every resolved host should inherit at most one group.

For discovered providers, implicit matching chooses one group. If multiple
groups match, the first matching group in file order wins and the resolver
should emit a warning or debug-visible event. A provider-level
`hosts.<canonical>.group` value overrides implicit matching.

For local providers, `hosts.<name>.group` is required. If host labels are needed
later, add `tags` rather than multi-group inheritance.

### Use Typed SSH Fields Plus A Map Escape Hatch

Prefer typed fields for supported SSH behavior and a strict `ssh.options` map
for uncommon OpenSSH options.

```yaml
ssh:
  proxy_jump: bastion01
  forward_agent: false
  local_forwards:
    - bind: 127.0.0.1:15432
      target: db.internal:5432
  options:
    Compression: "yes"
    ServerAliveInterval: "60"
```

Avoid raw argv as the primary escape hatch. It preserves ordering, but it also
lets config bypass validation and makes future non-OpenSSH transport harder.
Only add raw argv later if a real directive cannot be represented as typed
fields or `ssh.options`.

### Use Named Compatibility Fixes

Store compatibility fixes as user-visible nssh names:

```yaml
ssh:
  compat:
    - legacy-kex
    - legacy-macs
    - legacy-hostkey
    - legacy-pubkey
```

nssh should translate those names into OpenSSH flags at runtime. Exact option
fragments are implementation detail and are easier to get wrong by hand.

```text
legacy-kex      -> KexAlgorithms +diffie-hellman-group14-sha1,+diffie-hellman-group1-sha1
legacy-macs     -> MACs +hmac-sha1,+hmac-sha1-96
legacy-hostkey  -> HostKeyAlgorithms +ssh-rsa
legacy-pubkey   -> PubkeyAcceptedAlgorithms +ssh-rsa
```

Do not include a broad `legacy` preset in `release-0.3`.

### Include SSH Config Import In The Cutover

Include a minimal `nssh self import ssh-config` in the cutover. It should import
global defaults and selected Host blocks into YAML using the typed-field
boundary above, then stop. It does not need to perfectly translate every OpenSSH
feature on day one, but it must report any unsupported directives instead of
silently dropping them.

## Approval Status

Current status: approved for implementation.

Checks completed:

- The spec covers root config, credentials, provider-scoped inventory, local
  inventory, per-host compatibility fixes, imported SSH defaults for
  1Password agent integration, and a resolved connection sketch.
- All fenced YAML snippets parse as YAML.
- Markdown validation passes with `/Users/cj/.local/bin/validate-markdown`.
- The schema choices are implementation commitments for `release-0.3`.

Known review pressure points:

- One `hosts` key is easier to read but the provider type changes whether
  entries create inventory or overlay discovered inventory.
- Singular group inheritance is simple, but overlapping discovered-provider
  group matches must be visible through warnings or debug output.
- `ssh.options` as a map may not preserve every OpenSSH ordering edge case.
- The import command intentionally skips conditional and order-sensitive
  OpenSSH behavior.

## Implementation Acceptance Checklist

Implementation should satisfy these statements:

- Root config is lean enough to stay readable as the install grows.
- Provider-scoped inventory files make NetBox, local, and Containerlab ownership
  obvious.
- All providers use singular group inheritance.
- Local inventory uses provider-level `hosts` with a required `group` field.
- Credential provider references are explicit without making every host verbose.
- Provider-level `hosts` is readable for both local inventory and
  discovered-provider per-host overlays.
- Runtime merge order is explicit and deterministic.
- Runtime verbosity is explicit and does not require raw argv.
- `ssh.defaults` is the right place for imported global OpenSSH behavior.
- Typed SSH fields plus `ssh.options` are sufficient for the first YAML cutover.
- No raw argv escape hatch is required for `release-0.3`.
- The `release-0.3` compatibility fix catalog is narrow and explicit.
- The SSH config import scope is clear enough for `release-0.3`.
- The resolved connection sketch contains enough information to drive OpenSSH
  argv generation without reading generated SSH config or `~/.ssh/config`.

If any item fails during implementation, update this spec or the implementation
before release.
