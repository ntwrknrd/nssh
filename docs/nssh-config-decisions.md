# ADR: nssh Config Authority

Status: Accepted
Date: 2026-06-16
Applies to: `release/0.3`

## Context

`nssh` previously split runtime authority between nssh config/provider state and
generated OpenSSH config. That allowed nssh credential resolution to be current
while host lookup, routing, auth policy, compatibility fixes, or OpenSSH defaults
could still come from stale generated files.

The maintained schema example is `docs/examples/config/config.example.yaml`.

## Decision

`nssh` runtime behavior is owned by nssh YAML config, included YAML files, and
provider state. Runtime connect and SCP flows must not read `~/.ssh/config` or
generated `~/.ssh/nssh.d/*` files for managed host lookup, auth policy, routing,
compatibility fixes, or connection defaults.

The canonical config file is `~/.config/nssh/config.yaml`. Includes are YAML and
strictly decoded; unknown fields are validation errors.

Inventory policy is provider-scoped:

- local hosts live under `inventory.providers.local.hosts`
- discovered-provider overrides live under
  `inventory.providers.<provider>.hosts`
- provider refresh writes non-secret discovered state only
- operator-owned groups, auth mappings, SSH options, and host overrides stay in
  YAML config

OpenSSH remains the transport. nssh resolves one connection model and renders it
to `exec.Command("ssh", args...)` with `-F none`; normal execution must not build
shell command strings or use raw argv config.

SSH policy merges deterministically:

1. global `ssh.defaults`
2. discovered provider facts or local host identity
3. selected provider group defaults
4. provider host override
5. CLI SSH options

Legacy SSH compatibility is stored as per-host `ssh.compatibility` algorithm
floors: `kex`, `mac`, `host_key`, and `public_key`. Rendering expands those
floors into OpenSSH algorithm options using nssh policy and local OpenSSH
support.

OpenSSH config import is allowed as a one-way migration source. Imported
behavior is translated into nssh YAML; runtime behavior still comes from nssh
config and provider state.

## Consequences

- Raw `ssh <provider-host>` compatibility is not a product contract.
- Debug output showing the rendered OpenSSH argv is the truth source for nssh
  SSH policy; `ssh -G` only explains raw OpenSSH behavior.
- OpenSSH `Host` wildcard and `Match` behavior do not affect nssh runtime unless
  nssh models equivalent behavior explicitly.
- Historical implementation plans under `docs/superpowers/plans/` are not live
  product documentation.
