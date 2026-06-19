# nssh YAML Config Spec

This is the approved config schema spec for the `release-0.3` YAML cutover. It
defines the target YAML shape, merge behavior, OpenSSH import boundary,
compatibility floor policy, and runtime verbosity behavior for implementation.

## Goals

- Make nssh YAML the only runtime config format.
- Keep the root config lean.
- Keep provider-scoped inventory in provider-scoped files.
- Keep per-host config explicit and easy to find.
- Preserve common OpenSSH behavior in type-aware `ssh.options`.
- Keep uncommon OpenSSH directives as explicit, safe pass-through options.

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
    options:
      IdentitiesOnly: true
      IdentityAgent: ~/Library/Group Containers/2BUA8C4S2C.com.1password/t/agent.sock
      IdentityFile:
        - ~/.ssh/ed25519-1Password-Personal.pub
      ServerAliveInterval: 240s
      ServerAliveCountMax: 2
      SetEnv:
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
      ssh:
        options:
          TCPKeepAlive: "yes"
      config:
        url_env: NETBOX_URL
        token_env: NETBOX_TOKEN

      groups:
        cbb:
          ssh:
            options:
              ControlMaster: auto
              ControlPersist: 12h
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
            compatibility:
              kex: diffie-hellman-group14-sha1
              mac: hmac-sha1
            options:
              Ciphers:
                - aes256-ctr
```

### NetBox Notes

- Provider refresh writes provider state only.
- Group auth affects runtime resolution immediately after config load.
- Provider-backed `hosts` entries are operator-authored overlays for discovered
  hosts.
- Provider-level `ssh` config applies to every host from that provider.
- Group-level `ssh` config applies after provider-level `ssh` config and before
  per-host overlays.
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
        core-sw1.lan.ntwrknrd.net:
          group: homelab
          aliases:
            - core-sw1
            - core
            - core-sw1.lan
          port: 22

        pla-mgmt-sw1.custcbb.local:
          group: custcbb
          aliases:
            - pla-mgmt-sw1
            - pla-mgmt
          port: 22
```

### Local Inventory Notes

- Local hosts live under the local provider `hosts` key, not under groups.
- A local group can define match rules and default auth.
- Local hosts select one group with `group`.
- Explicit local hosts should not require provider state.
- Local auto-add prompts once for `Host`; the accepted value becomes the
  canonical inventory key and dial target.
- Local auto-add does not prompt for `HostName`. Target overrides belong under
  `ssh.options.HostName`.
- When the accepted local `Host` is an FQDN, nssh automatically adds the short
  name as an alias. Extra aliases are explicit and additive through
  `nssh inv set HOST --alias ALIAS`.

## Provider Host Entry With Compatibility Policy

Provider-backed compatibility policy lives in provider-level `hosts`.

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
            compatibility:
              kex: diffie-hellman-group14-sha1
              mac: hmac-sha1
            options:
              Ciphers:
                - aes256-ctr
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
- Compatibility floors are operator policy and belong in nssh config.
- `ssh.options` is the type-aware OpenSSH options surface. Known keys are
  validated before rendering; unknown safe one-to-one keys can pass through.

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

Provider defaults and selected group defaults include `ssh:` config. The same
`compatibility` and `options` schema is valid under:

```text
ssh.defaults
inventory.providers.<provider>.ssh
inventory.providers.<provider>.groups.<group>.ssh
inventory.providers.<provider>.hosts.<host>.ssh
```

The SSH config merge must be deterministic:

- `options` maps merge by OpenSSH directive key, with higher-priority layers
  winning.
- Known `options` keys validate typed values before rendering.
- Unknown `options` keys may pass through only when they are safe one-to-one
  OpenSSH `-o Key=Value` directives.
- `compatibility` maps merge by category, with higher-priority layers winning.
  Values are legacy algorithm floors, not exact final selections.
- If `options` sets an OpenSSH directive controlled by a compatibility category,
  that explicit option disables nssh compatibility policy for that category.

After the merge, nssh renders the resolved SSH config to OpenSSH argv and
executes `ssh` with `-F none`. Runtime must not read generated SSH config or
`~/.ssh/config`.

## Runtime Argv Inspection

nssh should include a diagnostic command that prints the exact OpenSSH argv it
would execute for a resolved host after applying defaults, provider config,
group config, host config, and CLI flags.

This command replaces `ssh -G <host>` for nssh debugging. `ssh -G` cannot be the
truth source once nssh disables OpenSSH config with `-F none`.

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

An import from `~/.ssh/config` should translate global defaults into
type-aware `ssh.options`.

```yaml
ssh:
  defaults:
    options:
      IdentitiesOnly: true
      IdentityAgent: ~/Library/Group Containers/2BUA8C4S2C.com.1password/t/agent.sock
      IdentityFile:
        - ~/.ssh/ed25519-1Password-Personal.pub
      ControlMaster: auto
      ControlPersist: 12h
      ControlPath: ~/.ssh/sockets/%r@%h:%p
      ServerAliveInterval: 240s
      ServerAliveCountMax: 2
      Ciphers:
        - chacha20-poly1305@openssh.com
        - aes256-gcm@openssh.com
        - aes256-ctr
      SetEnv:
        TERM: xterm-256color
```

### Imported Defaults Notes

- These defaults are nssh-owned after import.
- `nssh connect` still uses `-F none`.
- Import should translate deterministic OpenSSH directives into `ssh.options`.
- Known `ssh.options` keys should validate type-aware values before rendering.
- Import should preserve unsupported directives in `ssh.options` only when they
  are simple one-to-one OpenSSH `-o Key=Value` options.
- Import should warn and skip `Match`, `Include`, shell-expanded conditionals,
  `CanonicalizeHostname`, and anything with ordering or conditional behavior
  nssh cannot preserve honestly.
- `release-0.3` should not include a raw argv escape hatch.

### Import Option Boundary

Import these OpenSSH host identity directives into nssh inventory or auth fields:

```text
HostName -> host key when concrete; otherwise ssh.options.HostName
User     -> auth username
Port     -> host port
```

Import these OpenSSH behavioral directives into `ssh.options` with type-aware
validation for `release-0.3`.

```text
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

Common OpenSSH directives should live under `ssh.options`. Known keys are still
validated by nssh before rendering.

```yaml
ssh:
  options:
    ProxyJump: bastion01
    ProxyCommand: ssh -W %h:%p bastion01
    IdentitiesOnly: true
    IdentityAgent: ~/Library/Group Containers/2BUA8C4S2C.com.1password/t/agent.sock
    IdentityFile:
      - ~/.ssh/ed25519-1Password-Personal.pub
    CertificateFile:
      - ~/.ssh/ed25519-1Password-Personal-cert.pub
    ForwardAgent: false
    LocalForward:
      - 127.0.0.1:15432 db.internal:5432
    RemoteForward:
      - 127.0.0.1:18080 localhost:8080
    SetEnv:
      TERM: xterm-256color
    RemoteCommand: show version
    Compression: "yes"
```

### SSH Option Coverage Notes

- `ssh.options` is the operator-facing OpenSSH option surface.
- Known keys in `ssh.options` should validate expected value types.
- Unknown keys in `ssh.options` can pass through when a one-to-one OpenSSH
  option is safe, and should warn otherwise.
- Host and group config can use the same `ssh` shape; merge order decides the
  final resolved value.

## Resolved Connection Sketch

For `nssh 701-sw37`, the runtime resolver should produce a single object with
the final decision.

```yaml
query: 701-sw37
canonical_name: 701-sw37r103c608.expedient.com
dial_target: 701-sw37r103c608.expedient.com
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
  compatibility:
    kex: diffie-hellman-group14-sha1
    mac: hmac-sha1
  options:
    IdentityAgent: ~/Library/Group Containers/2BUA8C4S2C.com.1password/t/agent.sock
```

## Projected Current Config

This section projects the current local config into the proposed YAML shape. It
is included to test the schema against real usage.

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
- Is type-aware `ssh.options` strict enough, or should there also be a raw argv
  list to preserve exact OpenSSH ordering?
- Should compatibility policy be user-visible names, exact OpenSSH option
  fragments, exact algorithms selected from server offers, or typed algorithm
  floors?

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
            compatibility:
              kex: diffie-hellman-group14-sha1
```

For the local provider, `hosts` entries are the inventory and each host selects
one group:

```yaml
inventory:
  providers:
    local:
      hosts:
        core-sw1.lan.ntwrknrd.net:
          group: homelab
          aliases:
            - core-sw1
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

### Use Type-Aware SSH Options

Use `ssh.options` as the operator-facing OpenSSH option surface. Known keys are
type-aware and validated by nssh; unknown keys may pass through only when they
are safe one-to-one OpenSSH `-o Key=Value` directives.

```yaml
ssh:
  options:
    ProxyJump: bastion01
    ForwardAgent: false
    LocalForward:
      - 127.0.0.1:15432 db.internal:5432
    Compression: "yes"
    ServerAliveInterval: 60s
```

Avoid raw argv as the primary escape hatch. It preserves ordering, but it also
lets config bypass validation and makes future non-OpenSSH transport harder.
Only add raw argv later if a real directive cannot be represented as
`ssh.options`.

### Add Provider And Group SSH Inheritance

Use the same `compatibility` and `options` schema at provider, group, and host
scope:

```yaml
inventory:
  providers:
    netbox-prod:
      ssh:
        options:
          IdentityAgent: ~/Library/Group Containers/2BUA8C4S2C.com.1password/t/agent.sock
      groups:
        cbb:
          ssh:
            options:
              ControlMaster: auto
              ControlPath: ~/.ssh/sockets/%r@%h:%p
              ControlPersist: 12h
      hosts:
        701-sw37r103c608.expedient.com:
          ssh:
            options:
              IdentitiesOnly: true
```

This is how nssh replaces broad OpenSSH wildcard config. YAML `include` handles
file composition. Provider and group selection handle most `Host *` and
wildcard-host policy use cases. Explicit host overlays handle exceptional hosts.
OpenSSH `Match` remains out of scope unless nssh grows a typed equivalent.

### Use Compatibility Algorithm Floors

Store compatibility policy as typed algorithm floors:

```yaml
ssh:
  compatibility:
    kex: diffie-hellman-group1-sha1
    mac: hmac-sha1
    host_key: ssh-rsa
    public_key: ssh-rsa
```

Each value is the weakest legacy algorithm this host is allowed to use for that
category. nssh owns a small ordered policy list per category, sorted from most
preferred to least preferred. At render time, nssh includes only algorithms at
or above the configured floor, filters them against local OpenSSH support, and
orders them by nssh preference.

Auto-remediation should parse `Their offer`, intersect it with the relevant
policy list and `ssh -Q` local support, choose the most preferred working
algorithm, and persist that algorithm as the compatibility floor. If the only
working option is a weak last entry such as `diffie-hellman-group1-sha1`, nssh
should still be able to propose and persist it without requiring manual YAML
editing.

Diagnostics must stay transparent: `nssh inv get`, `nssh self cfg`, and rendered
argv inspection should show the configured floor and the exact OpenSSH expansion
nssh will execute.

```text
kex: [diffie-hellman-group-exchange-sha256, diffie-hellman-group14-sha1, diffie-hellman-group-exchange-sha1, diffie-hellman-group1-sha1]
mac: [hmac-sha2-512, hmac-sha2-256, hmac-sha1, hmac-sha1-96, hmac-md5]
host_key: [rsa-sha2-512, rsa-sha2-256, ssh-rsa]
public_key: [rsa-sha2-512, rsa-sha2-256, ssh-rsa]
```

Do not include broad `legacy` presets in `release-0.3`.

### Include SSH Config Import In The Cutover

Include a minimal `nssh self import ssh-config` in the cutover. It reads
`~/.ssh/config`, expands includes recursively, prompts before importing each
included file, prompts before importing `Host *` defaults, and asks which local
group each imported file's concrete hosts should use.

Imported SSH defaults write directly to the root `config.yaml` under
`ssh.defaults`. Imported hosts always write to the `local` inventory provider in
a fixed included YAML file under `inventory/` so root config can stay lean and
`include: [inventory/*.yaml]` picks it up.

Prompts should show the full nssh YAML fragment that will be written and the
destination file/key path. They should not collapse hidden lines behind a
summary such as `... more`.

The importer should translate global defaults and selected Host blocks into
`ssh.options` using the option boundary above. It does not need to perfectly
translate every OpenSSH feature on day one, but it must report any unsupported
directives instead of silently dropping them.

## Approval Status

Current status: approved for implementation.

Checks completed:

- The spec covers root config, credentials, provider-scoped inventory, local
  inventory, per-host compatibility policy, imported SSH defaults for
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
- Imported `ssh.defaults` affects runtime argv.
- Provider-level and group-level `ssh:` config use the same
  `compatibility`/`options` schema as host-level `ssh:` config.
- nssh can show the final rendered OpenSSH argv for a resolved host.
- Type-aware `ssh.options` plus `ssh.compatibility` are sufficient for the first
  YAML cutover.
- No raw argv escape hatch is required for `release-0.3`.
- The `release-0.3` compatibility floor policy is narrow and explicit.
- The SSH config import scope is clear enough for `release-0.3`.
- The resolved connection sketch contains enough information to drive OpenSSH
  argv generation without reading generated SSH config or `~/.ssh/config`.

If any item fails during implementation, update this spec or the implementation
before release.
