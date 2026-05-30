# nssh Architecture

This document is for contributors. For user-facing commands and examples, see
[USER_GUIDE.md](USER_GUIDE.md).

## Overview

`nssh` wraps OpenSSH tooling with smart host resolution, provider-backed
credentials, PTY-based password prompt handling, session recording, and
inventory output under SSH include files.

Primary packages:

- `cmd/nssh`: root command, SSH connect path, agent runtime entrypoint
- `internal/cli/inv`: inventory commands
- `internal/cli/resolve`: shared SSH/SCP host and credential resolution
- `internal/credential`: provider abstraction and provider implementations
- `internal/agent`: background runtime, provider-session broker, metadata cache,
  socket lifecycle, and peer checks
- `internal/ssh/connector`: PTY connector and password prompt handling
- `internal/recording`: recording plan, wrapper metadata, and archival policy

## Credential Model

Credentials are resolved through named provider instances configured under
`[credential.provider.<name>]`.

Supported providers:

- `pass`: default local provider, using `pass` or a configured compatible
  command.
- `1password`: reads and writes through `op`; `session = "agent"` lets the
  nssh agent own request-scoped provider sessions. The runtime agent auto-starts
  by default and can be disabled with `agent.auto_start = false`.
- `bitwarden`: reads through `bw`; authentication remains owned by
  Bitwarden CLI state.

Auth mappings live in inventory config:

- `inventory.host.<host>.auth` for host overrides
- `inventory.group.<group>.auth` for group defaults

Each mapping may specify `provider`, `ref`, `username`, and `username_ref`.
When `provider` is omitted, `credential.default_provider` is used.
Group-level `default_user` is SSH inventory policy; provider refresh renders it
as `User` in generated SSH config. Auth `username` is only for provider items
that must override the inventory login user.

Resolution order:

1. Host auth override provider and host credential
2. Inventory group auth provider and group credential
3. SSH config user/key authentication

The connect and SCP paths use the same resolver so provider selection cannot
drift between workflows.

## Secret Handling

Provider authentication is provider-owned. nssh does not store 1Password or
Bitwarden tokens, and does not keep a default cache of resolved passwords.
Secrets are request scoped: a provider returns one record, the connector injects
the password into the PTY when OpenSSH prompts, and the in-memory secret is
destroyed by the secret wrapper when no longer needed.

There is no automated migration from prior local encrypted credential files.
Users must recreate equivalent provider-backed records or link existing
provider items.

## Agent Runtime

The agent is a Unix-domain-socket daemon. It is not the credential store. Its
responsibilities are:

- provider-session requests for agent-owned providers
- non-secret metadata cache operations
- socket path management and stale socket cleanup
- peer credential verification
- idle timeout, max lifetime, signal handling, and stop/restart behavior
- hosting the background recording archive runner

Protocol operations include `hello`, `status`, `lock`, metadata cache
operations, and `provider_request`. `lock` is the internal stop operation used
by `nssh agent stop`; there is no public vault lock command.

## Connection Flow

1. Root preprocessing routes `nssh HOST` to `smart-connect`.
2. Host lookup checks SSH config and may refresh stale inventory providers once.
3. Username is selected from explicit `user@host`, SSH config, or defaults.
4. Inventory group is resolved from provider state or local SSH config comments.
5. `internal/cli/resolve` selects the provider from host or group binding.
6. The PTY connector runs OpenSSH and injects the resolved password only when
   prompted.
7. Optional recording wraps the outer connection and leaves connector behavior
   unchanged.

## Recording

Recording is planned outside the connector. The recording package selects cast
paths, lock directories, append behavior, title templates, idle limits, and
archive eligibility, and owns archive bundle policy. The agent hosts the
background archive runner so normal SSH sessions do not pay archive maintenance
cost.

## Troubleshooting Surfaces

- `nssh self status`: installed binary, config, providers, SSH include state,
  recording state, and agent status
- `nssh agent status`: runtime mode, lifetime, metadata cache count, and provider
  session count
- `nssh agent doctor`: socket and runtime diagnostics
- `nssh inv status`: inventory provider cache and output status
