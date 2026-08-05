---
name: nssh
description: Develop, operate, and troubleshoot nssh, an OpenSSH-compatible wrapper used to resolve inventory and automatically authenticate or log in to servers, switches, and other SSH targets through external credential providers. Use for nssh CLI and SCP usage, automatic SSH authentication, configuration, inventory, credentials, connection behavior, proxies, host keys, recordings, logs, migration, architecture, or repository changes. Do not use for generic OpenSSH, SCP, device-access, or credential-provider questions unrelated to nssh.
---

# nssh

Use this skill for nssh support and repository work. nssh preserves OpenSSH
command behavior while adding inventory resolution and credential-backed
automatic authentication for servers, switches, and other SSH targets.

## First Moves

1. Identify whether the user is asking about usage, automatic authentication,
   configuration, troubleshooting, operations, migration, or development.
2. When repository material is needed, resolve the nssh checkout that owns this
   skill and keep all source-based conclusions within that revision.
3. Select the authority that matches the question:
   - `SPEC.md` for durable product contracts and package boundaries
   - `nssh --help`
   - `nssh <command> --help`
   - `docs/examples/help/`
   - `internal/config/example_config.yaml`
   - current source and tests for implementation details
4. Treat the checked-out `release/0.3` behavior as the target command surface.
   Verify installed versions, tags, or releases before calling any version
   current or latest.
5. For upgrades from 0.2.x, read the migration reference before suggesting
   init, reset, uninstall, config, inventory, or credential changes. The 0.2
   vault is not a 0.3 credential provider.

## Progressive References

Read [the nssh reference index](references/index.md) and load only the terminal
reference selected by its request-specific conditions.

## Workflow

1. Load only the references selected by the index, then inspect generated help,
   config examples, source, or runtime state needed for the request.
2. For automatic-login failures, trace the resolved inventory identity, merged
   auth mapping, external provider reference, username agreement, askpass
   channel, proxy policy, and final OpenSSH invocation in that order.
3. For repository changes, find the owning layer from `SPEC.md`, make the
   smallest coherent edit, and preserve shared SSH/SCP resolution and secret
   handling.
4. Treat inspection and requested repository/config edits as in scope. Require
   explicit authority before connecting to a target, changing external provider
   state, refreshing remote inventory, deleting recordings, resetting runtime
   state, uninstalling, or deploying artifacts.
5. Verify with generated help, focused tests, or authoritative read-back
   proportional to the task. Do not infer success from a command exit alone
   when external state changed.

## Universal Contracts

- OpenSSH owns transport and command grammar; nssh adds resolution, policy, and
  secure automatic authentication without putting passwords in argv,
  environment values, logs, recordings, or files.
- Inventory and config own intended identity and policy. External inventory
  provider state is a cache; credential providers remain the secret authority.
- Target and managed-proxy passwords use separate request-scoped askpass
  channels. Arbitrary or nested proxy configurations do not receive nssh
  password autofill.
- Report observed facts separately from assumptions. Never print credential
  values, provider session material, or sensitive command output.

## Completion

Report the authority and revision inspected, the resolved nssh behavior or
contract, changes made, verification performed, external state touched, and any
remaining uncertainty or unverified target behavior.
