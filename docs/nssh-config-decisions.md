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

Rationale: splitting authority between nssh config and generated OpenSSH config
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

### Strict YAML Is Canonical

The clean cutover should use YAML as the canonical nssh configuration format.

Rationale: the next config model is deeply hierarchical. Flat dotted table names
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
identity, aliases, port, username, provider, group, auth mode,
credential target, proxy settings, compatibility policy, host-key policy, and
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
`port`, and `aliases`, but its config fields still override selected
group defaults later in the merge.

Provider defaults and selected group defaults should include `ssh:` config, not
only `auth:` config. This gives nssh a native replacement for broad OpenSSH
`Host` wildcard policy without preserving OpenSSH's pattern precedence rules.

The runtime SSH merge must be deterministic:

- `ssh.options` maps merge by OpenSSH directive key, with the higher-priority
  layer winning.
- `ssh.options` is type-aware for known OpenSSH directives: booleans, durations,
  string lists, forwarding strings, and maps such as `SetEnv` should validate
  before rendering.
- Unknown `ssh.options` keys may pass through only when they can be rendered as
  safe one-to-one OpenSSH `-o Key=Value` directives.
- `ssh.compatibility` maps merge by category, with higher-priority layers
  winning. Values are legacy algorithm floors, not exact final selections.
- If `ssh.options` sets an OpenSSH directive controlled by a compatibility
  category, that explicit option disables nssh compatibility policy for that
  category.

The resolved SSH config should then be rendered to `exec.Command("ssh", args...)`
argv with `-F none`. nssh should never construct a shell command string for
normal SSH execution.

### Runtime Argv Must Be Inspectable

Add a diagnostic command that prints the exact OpenSSH argv nssh would execute
for a host after config resolution. The command should redact nothing by default
because argv contains policy, not passwords; if a future option may carry secret
material, that specific value should be redacted.

This command is the replacement for using `ssh -G <host>` as a truth source.
`ssh -G` is still useful for raw OpenSSH debugging, but it cannot explain nssh
behavior once nssh runs OpenSSH with `-F none`.

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

### Compatibility Policy Is Per-Host Config

Legacy SSH compatibility policy should be stored as provider-scoped per-host nssh
config, not in generated OpenSSH config and not as hidden runtime-only state.

Provider state may identify discovered hosts and current group placement, but
nssh config should own learned operator policy. Use one provider-level `hosts`
key in `inventory/<provider>.yaml`: for discovered providers, entries overlay
discovered hosts; for the local provider, entries are the inventory.

### SSH Config Import Is Allowed, Runtime Reads Are Not

`nssh` may provide an import command that reads existing OpenSSH config as source
material. Import can translate global defaults and Host blocks into nssh config
and preserve safe OpenSSH directives in `ssh.options`.

After import, runtime connection behavior must still come from nssh config.

The user-facing import command should stay simple: no source, provider, output,
or dry-run flags. It reads `~/.ssh/config`, expands includes recursively, prompts
before importing each included file, prompts before importing `Host *` defaults,
and asks which local group each imported file's concrete hosts should use.
Imported SSH defaults write directly to the root `config.yaml` under
`ssh.defaults`. Imported hosts always write to the `local` inventory provider in
a fixed YAML file under `inventory/`.

Import prompts should preview the full nssh YAML fragment that will be written
and identify the destination file/key path. They should not hide config behind
line-count summaries.

For `release-0.3`, import OpenSSH host identity directives into nssh inventory
or auth fields:

- `HostName` -> host key when concrete; otherwise `ssh.options.HostName`
- `User` -> auth `username`
- `Port` -> host `port`

Import OpenSSH behavioral directives into the type-aware `ssh.options` map, not
as many parallel typed fields under `ssh`.

Known option keys should validate their value shape before rendering:

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
shell-expanded conditionals, `CanonicalizeHostname`, and any directive with
ordering or conditional behavior nssh cannot preserve honestly.

No raw argv escape hatch should be included in `release-0.3`.

### Compatibility Uses Algorithm Floors

`release-0.3` should replace named compatibility lists with typed
compatibility floors:

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
orders them by nssh preference. OpenSSH then selects the first client-preferred
algorithm that the server also offers.

This keeps config honest without requiring operators to hand-maintain long
algorithm lists. It also avoids blindly copying the server's `Their offer` list,
which may include weak algorithms or reflect an old server's preference order.

Initial ordered policy lists:

```text
kex: [diffie-hellman-group-exchange-sha256, diffie-hellman-group14-sha1, diffie-hellman-group-exchange-sha1, diffie-hellman-group1-sha1]
mac: [hmac-sha2-512, hmac-sha2-256, hmac-sha1, hmac-sha1-96, hmac-md5]
host_key: [rsa-sha2-512, rsa-sha2-256, ssh-rsa]
public_key: [rsa-sha2-512, rsa-sha2-256, ssh-rsa]
```

Auto-remediation should parse `Their offer`, intersect it with the relevant
policy list and `ssh -Q` local support, choose the most preferred working
algorithm, and persist that algorithm as the compatibility floor. If the only
working option is a weak last entry such as `diffie-hellman-group1-sha1`, nssh
should still be able to propose and persist it without requiring manual YAML
editing.

Diagnostics must show both the configured floor and the effective OpenSSH argv
expansion so compatibility policy is visible.

### Preserve 1Password SSH Agent Integration

Using OpenSSH as transport keeps 1Password SSH agent integration viable.
Settings such as `IdentityAgent`, `IdentityFile`, and `IdentitiesOnly` should be
stored as validated keys under `ssh.options`.

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
- Existing pre-YAML config is replaced rather than carried as a runtime format.

## Required Capabilities

- Fast host lookup across provider state and local nssh inventory.
- Fuzzy selection from nssh-managed hosts only.
- YAML includes for modular config files.
- Strict validation for unknown YAML fields.
- Explicit auth argv for password and key modes.
- Explicit support for nssh host identity and auth fields:
  - aliases
  - `ssh.options.HostName` override when the connection target differs from the inventory key
  - port
  - username
- Explicit support for common OpenSSH behavior under type-aware `ssh.options`:
  - proxy jump
  - identity agent
  - identity file
  - identities only
  - certificate file
  - forward agent
  - local and remote forwarding
  - set environment
  - remote command
  - compatibility algorithms
- Explicit support for nssh SSH runtime policy:
  - host-key policy
  - connection, password, and idle timeouts
- Safe pass-through for uncommon OpenSSH options.
- Provider-level and group-level `ssh:` inheritance.
- A command that shows the final rendered OpenSSH argv for a resolved nssh host.

## Resolved Schema Questions

- Global SSH defaults belong under `ssh.defaults`.
- Provider-level `hosts` is the single key for local host entries and
  discovered-provider per-host overlays.
- Local auto-add treats the prompted `Host` value as the canonical inventory
  key and dial target.
- If a local auto-added host is an FQDN, nssh automatically adds its short name
  as an alias. Additional aliases are explicit and additive through
  `nssh inv set HOST --alias ALIAS`.
- Inventory `hostname` is not supported. When a target override is truly
  needed, configure OpenSSH `HostName` under `ssh.options`.
- All providers use singular group inheritance.
- Discovered providers select one group by first matching group in file order,
  unless `hosts.<canonical>.group` overrides it.
- Local hosts require `hosts.<name>.group`.
- Runtime merge order is global defaults, provider defaults, host identity,
  selected group defaults, per-host config, then CLI flags.
- Provider-level and group-level `ssh:` config use the same
  `compatibility`/`options` schema as host-level `ssh:` config.
- Imported `ssh.defaults` must affect runtime argv; storing the defaults without
  merging them into resolved host config is not acceptable.
- nssh handles OpenSSH `Include`, broad wildcard, and `Match` use cases through
  nssh-native mechanisms: YAML includes, provider/group selection, and explicit
  host overlays.
- Compatibility policy should be stored as typed `ssh.compatibility` algorithm
  floors, not as named `legacy-*` presets or hand-written OpenSSH fragments.
- Compatibility diagnostics and argv inspection must show the configured floor,
  selected policy expansion, and final OpenSSH options.
- Compatibility policy belongs under provider-scoped `hosts` entries, not
  generated state.
- The first YAML cutover should include a minimal SSH config import command.
- SSH config import has a narrow first-version boundary: type-aware
  `ssh.options` for common directives, safe one-to-one pass-through for unknown
  directives, and warnings for anything conditional or order-sensitive.
- `release-0.3` should not include raw argv config.
- Runtime verbosity is first-class: `-v` enables nssh debug, while `-vv` through
  `-vvvv` add OpenSSH `-v` through `-vvv`.
- `release-0.3` compatibility categories are `kex`, `mac`, `host_key`, and
  `public_key`.
