# nssh Config Authority Decisions

This log records architecture decisions for removing generated OpenSSH config as
a runtime dependency in the `release-0.3` worktree.

The companion YAML schema spec is approved for implementation in
`docs/nssh-config-yaml-mockup.md`.

## Decisions

### nssh Config Is The Runtime Source Of Truth

`nssh connect` must resolve connections from nssh config, included nssh config
files, and provider state. It must not read `~/.ssh/config` or generated
`~/.ssh/nssh.d/*` files for host lookup, auth policy, routing, compatibility
fixes, or connection defaults.

Rationale: splitting authority between nssh TOML and generated OpenSSH config
allowed credential resolution to be correct while OpenSSH auth policy remained
stale.

### No Generated SSH Config Dependency

Provider refresh should update provider state only. It should not write
provider-owned SSH config files for `nssh` runtime use.

Raw `ssh <host>` compatibility for provider-backed hosts is no longer a product
contract. Operators should use `nssh` to get nssh features such as credential
injection, host-key handling, compatibility remediation, audit, and recording.

### No Migration Compatibility Pass

The `release-0.3` branch is still in development and has a single active user.
The cutover should be clean rather than maintaining a temporary hybrid mode.

### Provider-Scoped Local Inventory Files

Local, manually managed hosts should live in provider-scoped nssh inventory
files, not in the root config. The root config should stay lean and use includes
for credential, inventory, and host data.

### Strict YAML Replaces TOML

The clean cutover should replace TOML with YAML as the canonical nssh
configuration format.

Rationale: the next config model is deeply hierarchical. TOML dotted table names
and inline maps make provider, group, host, auth, match, and SSH option data hard
to scan. YAML gives the required visual nesting while preserving comments.

YAML must be decoded with a strict schema. Unknown keys should be validation
errors, not silently ignored. This avoids free-form YAML drift while keeping the
file readable.

### OpenSSH Remains The Transport For Now

`nssh` should continue to execute OpenSSH, but only as a transport binary.
Connection behavior should be expressed as typed nssh config and resolved into a
typed argv passed to `exec.Command("ssh", args...)`.

Do not build shell command strings except for debug display.

### Verbosity Is First-Class Runtime State

Do not use raw argv for OpenSSH verbosity. Repeated `-v` flags should increase
diagnostic output in one predictable ladder:

```text
nssh -v <host>     -> nssh debug
nssh -vv <host>    -> nssh debug + ssh -v
nssh -vvv <host>   -> nssh debug + ssh -vv
nssh -vvvv <host>  -> nssh debug + ssh -vvv
```

`-v` remains nssh debugging. Additional `v` levels add OpenSSH transport
verbosity. OpenSSH verbosity should be capped at `-vvv`.

### Runtime Resolution Uses One Connection Model

Connection execution should be driven by a single resolved model containing host
identity, aliases, hostname, port, username, provider, group, auth mode,
credential target, proxy settings, compatibility fixes, host-key policy, and
timeouts.

This model should be transport-neutral enough that a future custom SSH client can
consume it without another config migration.

### Resolution Merge Order Is Fixed

Runtime resolution should apply config in this order:

```text
global ssh.defaults
-> provider defaults
-> discovered provider facts or local host identity
-> selected group defaults
-> provider.hosts.<host> per-host config
-> CLI flags
```

For local hosts, the `hosts.<name>` entry supplies identity fields such as
`hostname`, `port`, and `aliases`, but its config fields still override selected
group defaults later in the merge.

### Group Inheritance Is Singular

Every resolved host should have at most one selected group. Multiple inherited
groups are not supported for `release-0.3`; labels or search facets should use a
future `tags` field instead of multi-group inheritance.

For discovered providers, group matching should produce one group. If multiple
groups match implicitly, the first matching group in file order wins and the
resolver should emit a warning or debug-visible event. A provider-level
`hosts.<canonical>.group` value overrides implicit matching.

For the local provider, `hosts.<name>.group` is required unless a later schema
adds an explicit default group.

### Compatibility Fixes Are Per-Host Config

Legacy SSH compatibility fixes should be stored as provider-scoped per-host nssh
config, not in generated OpenSSH config and not as hidden runtime-only state.

Provider state may identify discovered hosts and current group placement, but
nssh config should own learned operator policy. Use one provider-level `hosts`
key in `inventory/<provider>.yaml`: for discovered providers, entries overlay
discovered hosts; for the local provider, entries are the inventory.

### SSH Config Import Is Allowed, Runtime Reads Are Not

`nssh` may provide an import command that reads existing OpenSSH config as source
material. Import can translate global defaults and Host blocks into nssh config
and preserve unsupported directives through an explicit escape hatch.

After import, runtime connection behavior must still come from nssh config.

For `release-0.3`, import these OpenSSH directives as typed nssh fields:

- `HostName`
- `User`
- `Port`
- `ProxyJump`
- `ProxyCommand`
- `IdentityAgent`
- `IdentityFile`
- `CertificateFile`
- `IdentitiesOnly`
- `ForwardAgent`
- `LocalForward`
- `RemoteForward`
- `SetEnv`
- `RemoteCommand`
- `ServerAliveInterval`
- `ServerAliveCountMax`
- `ConnectTimeout`
- `ControlMaster`
- `ControlPersist`
- `ControlPath`
- `Ciphers`
- `MACs`
- `KexAlgorithms`
- `HostKeyAlgorithms`
- `PubkeyAcceptedAlgorithms`

Preserve unsupported directives in `ssh.options` only when they are simple
one-to-one OpenSSH `-o Key=Value` options. Warn and do not import `Match`,
`Include`, shell-expanded conditionals, `CanonicalizeHostname`, and any
directive with ordering or conditional behavior nssh cannot preserve honestly.

No raw argv escape hatch should be included in `release-0.3`.

### Compatibility Fix Catalog Is Narrow

`release-0.3` should include four named compatibility fixes:

- `legacy-kex`
- `legacy-macs`
- `legacy-hostkey`
- `legacy-pubkey`

They should translate to these OpenSSH option additions:

```text
legacy-kex      -> KexAlgorithms +diffie-hellman-group14-sha1,+diffie-hellman-group1-sha1
legacy-macs     -> MACs +hmac-sha1,+hmac-sha1-96
legacy-hostkey  -> HostKeyAlgorithms +ssh-rsa
legacy-pubkey   -> PubkeyAcceptedAlgorithms +ssh-rsa
```

Do not include a broad `legacy` preset in `release-0.3`; it hides too much and
encourages sloppy per-host fixes.

### Preserve 1Password SSH Agent Integration

Using OpenSSH as transport keeps 1Password SSH agent integration viable.
Settings such as `IdentityAgent`, `IdentityFile`, and `IdentitiesOnly` should be
modeled explicitly in nssh config or imported from OpenSSH config.

If nssh later moves to a custom SSH client, the 1Password agent remains possible
because it speaks the SSH agent protocol over a Unix socket, but the complexity
is higher.

## Expected Losses

- Raw `ssh <provider-host>` support is no longer guaranteed.
- `ssh -G <host>` is no longer the truth source for nssh behavior.
- OpenSSH `Host` wildcard and `Match` behavior will not apply at runtime unless
  nssh models equivalent behavior explicitly.
- Arbitrary OpenSSH directives will not be silently inherited from
  `~/.ssh/config`.
- Existing TOML config is replaced rather than carried as a runtime format.

## Required Capabilities

- Fast host lookup across provider state and local nssh inventory.
- Fuzzy selection from nssh-managed hosts only.
- YAML includes for modular config files.
- Strict validation for unknown YAML fields.
- Explicit auth argv for password and key modes.
- Explicit support for common SSH options:
  - hostname
  - aliases
  - port
  - username
  - proxy jump
  - identity agent
  - identity file
  - identities only
  - certificate file
  - forward agent
  - local and remote forwarding
  - set environment
  - remote command
  - host-key policy
  - compatibility algorithms
  - connection and idle timeouts
- An explicit escape hatch for uncommon OpenSSH options.

## Resolved Schema Questions

- Global SSH defaults belong under `ssh.defaults`.
- Provider-level `hosts` is the single key for local host entries and
  discovered-provider per-host overlays.
- All providers use singular group inheritance.
- Discovered providers select one group by first matching group in file order,
  unless `hosts.<canonical>.group` overrides it.
- Local hosts require `hosts.<name>.group`.
- Runtime merge order is global defaults, provider defaults, host identity,
  selected group defaults, per-host config, then CLI flags.
- Compatibility fixes should be stored as user-visible nssh names such as
  `legacy-kex`, not as hand-written OpenSSH fragments.
- Compatibility fixes belong under provider-scoped `hosts` entries, not
  generated state.
- The first YAML cutover should include a minimal SSH config import command.
- SSH config import has a narrow first-version boundary: typed fields for common
  directives, `ssh.options` for safe one-to-one options, warnings for anything
  conditional or order-sensitive.
- `release-0.3` should not include raw argv config.
- Runtime verbosity is first-class: `-v` enables nssh debug, while `-vv` through
  `-vvvv` add OpenSSH `-v` through `-vvv`.
- `release-0.3` compatibility fixes are `legacy-kex`, `legacy-macs`,
  `legacy-hostkey`, and `legacy-pubkey`.
