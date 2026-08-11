# nssh Specification

## Purpose

nssh is an OpenSSH front end for operators with many SSH targets. It preserves
normal SSH behavior while adding inventory lookup, external credential
providers, secure password delivery, managed single-hop proxies, optional
session recording, and operational diagnostics.

The source and generated command help define implementation details. This file
defines the durable product and package boundaries that implementations must
preserve.

## Public Contract

- Root invocation follows `nssh [ssh-options] HOST [command]`.
- SSH options occur before `HOST`; tokens after `HOST` form the remote command.
- Smart lookup resolves managed inventory and may offer selection or local host
  creation. Literal targeting bypasses fuzzy selection without discarding known
  inventory metadata.
- Interactive connections preserve terminal semantics. Remote commands preserve
  distinct stdout, stderr, and remote exit status.
- SCP uses the same host, SSH policy, proxy, credential, and host-key resolution
  as SSH connections.
- Generated help is the command and flag authority.

## Configuration And Inventory

- Operator configuration is YAML under the XDG config tree. Included files are
  merged before the including file overrides them.
- Inventory is the authority for managed host identity, destination, group
  membership, SSH policy, highlighting policy, and authentication mapping.
- Local inventory is operator-owned YAML. External provider state is a
  non-secret refreshable cache; operator policy remains in YAML.
- Configuration validation fails closed on unsupported legacy schema.
- The example configuration is the field-level schema guide. Narrative
  documentation must not duplicate the full schema.

## Credentials And Secrets

- nssh does not maintain a credential vault. Secrets remain in supported
  external providers.
- Configuration stores provider names and item references, never resolved
  passwords.
- Resolved passwords use protected secret values and are exposed as bytes only
  for the bounded operation that consumes them.
- Passwords must not appear in argv, environment values, logs, recordings,
  temporary files, or persisted runtime state.
- Password delivery uses authenticated request-scoped askpass channels. Target
  and managed-proxy credentials have separate channels.
- Username conflicts between inventory intent and provider data must not inject
  a credential for the wrong account.

## Connection And Trust

- OpenSSH owns transport, negotiation, and authentication behavior.
- Interactive SSH uses a PTY for terminal I/O, signals, and resize behavior;
  askpass is the normal password transport.
- A single inventory-resolved proxy may be managed by nssh. Arbitrary, nested,
  or multi-hop proxy configurations remain OpenSSH-owned and do not receive
  nssh password autofill.
- Target host keys are verified before target credentials are used. A managed
  proxy credential may be required to reach the target for that verification.
- Accept-once trust is temporary. Persistent trust or changed-key replacement
  requires explicit operator approval.
- Compatibility adjustments are bounded to recognized negotiation failures and
  persist only as typed host SSH policy after approval.

## Runtime Boundaries

- Configuration owns parsing, validation, includes, paths, and inheritance.
- Inventory owns provider discovery, cached state, grouping, and reconciliation.
- Credential providers own external secret retrieval.
- The connection layer owns shared SSH and SCP resolution and orchestration.
- The SSH layer owns OpenSSH process, PTY, askpass, host-key, and stdio mechanics
  without depending on higher-level CLI behavior.
- The runtime agent brokers only provider access that must be retained. It is
  not a password manager, recording scheduler, or general job daemon, and it
  must not persist decrypted credentials.
- Recording owns recording plans, metadata, exports, and archive eligibility;
  interactive terminal work remains in the connection and SSH layers.

## Recording And Rendering

- Recording is opt-in and wraps the outer interactive connection without
  recursively recording its inner process.
- Live sessions inherit the operator's real terminal size. Fixed dimensions
  apply only to exports.
- Interactive PTY bytes pass through without syntax highlighting or rendering
  delays.
- Highlighting is allowed only where nssh owns complete output, currently
  remote-command stdout or future managed renderers.
- Existing ANSI and unsafe control data pass through unchanged.

## State And Lifecycle

- Config, data, and state follow XDG paths and remain distinct.
- External inventory caches, audit logs, and recordings are state. Credential
  documents and benchmark artifacts are data. Operator YAML is configuration.
- Reset and uninstall operations expose their destructive scope, support a dry
  run, and require explicit confirmation or preservation flags.
- Migration must preserve rollback material until representative inventory,
  authentication, proxy, SCP, and recording workflows are verified.

## Verification

Changes must preserve package boundaries, secret-handling invariants, generated
help snapshots, configuration validation, and shared SSH/SCP resolution.
Behavioral detail belongs in tests and source rather than being expanded here.
