---
name: nssh
description: Use when working with nssh concepts or answering questions about nssh usage, configuration, inventory, credential providers, connection behavior, logs, troubleshooting, operations, or architecture.
---

# nssh

Use this skill for nssh support and architecture context. Keep answers grounded
in the repo, command help, example config, architecture reference, and current
source.

## First Moves

1. Identify whether the user is asking about usage, configuration,
   troubleshooting, operations, or architecture.
2. Prefer generated help and source over remembered behavior:
   - `nssh --help`
   - `nssh <command> --help`
   - `docs/examples/help/`
   - `docs/examples/config/config.example.yaml`
   - `references/architecture.md`
3. Do not describe removed command surfaces as current. In release/0.3, the old
   local vault plus `host`, `ctx`, `sync`, `lock`, and `unlock` workflows are
   gone.

## Progressive References

Read only what matches the user request:

- [architecture.md](references/architecture.md) for architecture, package
  boundaries, command flow, storage model, credential model, agent runtime, SSH
  connector behavior, recordings, and removed surfaces.
- [configuration-inventory-credentials.md](references/configuration-inventory-credentials.md)
  for config files, includes, inventory groups, local inventory, NetBox,
  containerlab, credential providers, and auth mappings.
- [connections-logs-troubleshooting.md](references/connections-logs-troubleshooting.md)
  for connect/SCP behavior, fuzzy matching, provider refresh on lookup miss,
  agent runtime, host keys, legacy SSH fixes, recordings, logs, benchmarks, and
  diagnostics.

## Common Commands

```bash
nssh self init
nssh self status
nssh inv list
nssh inv get <host>
nssh inv set <host> --hostname <addr> --user <user> -g <group>
nssh inv status
nssh inv doctor
nssh <host>
nssh connect <host>
nssh cp <host>:/remote/path ./local/path
nssh agent status
nssh agent doctor
nssh log list
nssh -v <command>
NSSH_DEBUG=1 nssh <host>
```
