---
name: nssh
description: Use when working with nssh concepts or answering questions about nssh usage, configuration, inventory, credential providers, connection behavior, logs, troubleshooting, operations, or architecture.
---

# nssh

Use this skill for nssh support and repository work. Keep answers grounded in
the repository-root product specification, generated help, example config, and
current source.

## First Moves

1. Identify whether the user is asking about usage, configuration,
   troubleshooting, operations, or architecture.
2. Select the authority that matches the question:
   - `SPEC.md` for durable product contracts and package boundaries
   - `nssh --help`
   - `nssh <command> --help`
   - `docs/examples/help/`
   - `internal/config/example_config.yaml`
   - current source and tests for implementation details
3. Describe only the current release/0.3 command surface.
4. For upgrades from the latest stable release (0.2.x), read the migration
   reference before suggesting init, reset, uninstall, config, inventory, or
   credential changes. The 0.2 vault is not a 0.3 credential provider.

## Progressive References

Read [the nssh reference index](references/index.md) and load only the terminal
reference selected by its request-specific conditions.

## Common Commands

```bash
nssh self init
nssh self init --cred 1password
nssh self init --inv local
nssh self status
nssh inv list
nssh inv get <host>
nssh inv set <host> --hostname <addr> --user <user> -g <group>
nssh inv status
nssh <host>
nssh --select
nssh --target <host>
nssh cp <host>:/remote/path ./local/path
nssh agent status
nssh agent stop
nssh agent reset
nssh log list
nssh log play
nssh log export
nssh log delete --select <pattern> --dry-run
nssh -v <command>
NSSH_DEBUG=1 nssh <host>
```
