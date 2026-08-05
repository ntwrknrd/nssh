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
   - `internal/config/example_config.yaml`
   - `references/architecture.md`
3. Describe only the current release/0.3 command surface.
4. For upgrades from the latest stable release (0.2.x), read the migration
   reference before suggesting init, reset, uninstall, config, inventory, or
   credential changes. The 0.2 vault is not a 0.3 credential provider.

## Progressive References

Read [the nssh reference index](references/index.md) and load only the terminal
reference selected by its request-specific conditions.

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
- The connection-specific `--select` and `--target` flags are long-only;
  `-v`, `-V`, and `-h` remain nssh root flags.
- Interactive sessions add default remote `-tt` unless the user passes `-t`,
  `-tt`, or `-T`; remote command mode does not add default `-tt`.
- Password-backed sessions use the separately installed `nssh-askpass` helper
  and request-scoped local sockets. Inventory-resolved single-hop proxies can
  resolve and inject target and proxy credentials independently.

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
