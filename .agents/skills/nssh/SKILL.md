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
3. Describe only the current release/0.3 command surface.

## Progressive References

Read only what matches the user request:

- [architecture.md](references/architecture.md) for architecture, package
  boundaries, command flow, storage model, credential model, agent runtime, SSH
  connector behavior, and recordings.
- [configuration-inventory-credentials.md](references/configuration-inventory-credentials.md)
  for config files, includes, inventory groups, local inventory, NetBox,
  containerlab, credential providers, and auth mappings.
- [connections-logs-troubleshooting.md](references/connections-logs-troubleshooting.md)
  for connect/SCP behavior, fuzzy matching, provider refresh on lookup miss,
  agent runtime, host keys, legacy SSH fixes, recordings, logs, benchmarks, and
  diagnostics.

## Recording Guidance

- Do not set a fixed window size on live `asciinema rec` sessions. Although it
  makes published recordings more stable and readable, it also changes the live
  PTY size seen by shells and full-screen/line-editing programs. That can leave
  stale terminal paint, merged history lines, and confusing redraw behavior.
- Keep live SSH sessions on the operator's real terminal size. Apply readable
  dimensions only during export, using `logging.export.gif.window_size` and
  `logging.export.gif.font_size` for GIF output through `agg`.
- Do not reintroduce `logging.session.window_size`,
  `RecordingPlan.WindowSize`, or `asciinema rec --window-size` for interactive
  connections.

## Root SSH Contract

Root `nssh` follows OpenSSH command grammar:

```text
nssh [flags] [ssh-options] HOST [command]
```

- Bare `nssh` prints help.
- Parse SSH options only before `HOST`; tokens after `HOST` are remote command.
- `nssh HOST` uses smart host resolution.
- `nssh HOST command...` runs `command...` remotely.
- `--select` opens the smart target picker.
- `--target HOST` bypasses subcommand parsing and fuzzy resolution; exact
  managed targets still use inventory metadata, unmanaged targets use SSH
  defaults.
- nssh-specific root flags are long-only so they do not collide with OpenSSH
  short flags.
- Interactive sessions add default remote `-tt` unless the user passes `-t`,
  `-tt`, or `-T`; remote command mode does not add default `-tt`.

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
nssh --select
nssh --target <host>
nssh cp <host>:/remote/path ./local/path
nssh agent status
nssh agent doctor
nssh log list
nssh -v <command>
NSSH_DEBUG=1 nssh <host>
```
