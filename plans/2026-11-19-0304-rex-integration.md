# Rex terminal integration for nssh

Status: proposed design. Nothing here is implemented.

## Summary

When nssh runs inside the Rex terminal, it can use Rex for presentation that
would otherwise require an nssh TUI: tabs, split panes, a command-palette
inventory picker, and per-host status on tabs and in the session picker.
Rex is an optional presentation backend. The batch CLI and plain REPL remain
the baseline and work unchanged without Rex.

Further investment in the Charm interactive frontend stops. SPEC.md must be
updated to say so; see [SPEC changes](#spec-changes).

## Goals

- Open one or many inventory hosts as interactive sessions in Rex tabs or panes.
- Report connection and multi-host progress through Rex program status.
- Offer a Rex command-palette picker for connecting and running commands.
- Degrade silently, with no behavior change, outside Rex.

## Non-goals

- Linking against or reimplementing the Rex server protocol. Its transport is
  not documented; the `rex` CLI with `--json` is the stable surface.
- Making Rex a dependency of any existing command.
- Broadcasting keystrokes to several panes. See [Deferred](#deferred).
- Replacing attributed batch output with one pane per host by default.

## Rex surfaces used

| Surface | Use in nssh |
| --- | --- |
| Environment: `REX_SESSION`, `REX_BLOCK`, `REX_SERVER`, `REX_BIN_DIR` | Detection and targeting |
| `rex run`, `rex split`, `session.new_window` layouts | Open hosts in tabs, splits, or a grid |
| `session.set_block_label` | Stable pane label set to the inventory name |
| OSC 7501 program status | Connection state and per-host progress |
| Lua actions with `rex.ui.pick` and `rex.ui.input` | Palette picker and prompts |
| `rex capture`, `rex events` | Optional follow-ups; not required for the first slices |

References: Rex documentation pages for the CLI, custom UI, actions, Lua
scripts, Lua events, program status, and server API under
`https://www.superlogical.com/rex/docs/`.

## Detection

nssh treats Rex as present when all of the following hold:

1. `REX_SESSION` and `REX_BLOCK` are set.
2. A `rex` executable resolves, preferring `$REX_BIN_DIR/rex`.
3. For features that call the CLI, a `rex whoami --json` probe succeeds within
   a short timeout. The result is cached for the process.

OSC 7501 status needs only the first condition, since terminals ignore unknown
OSC sequences.

Operators can disable integration with a YAML setting and a `--no-rex` flag.
A failed probe or Rex CLI error never fails the nssh operation; it falls back
to the non-Rex path and logs at verbose level.

## Design

### 1. Program status (OSC 7501)

nssh writes OSC 7501 reports to the controlling terminal. No `rex` CLI call
is involved.

Interactive sessions report on the root record with `app=nssh`:

| Event | Report |
| --- | --- |
| Connecting | `state=working`, `msg` naming the target |
| Manual password or host-key approval | `state=blocked`, `kind=auth` or `kind=question` |
| Session established | `state=idle` or `clear` |
| Session ends with nonzero status | `state=error`, `msg` with the exit status |

Root host lists and REPL submissions report a root record with `progress` and
one child record per host, `id=hosts/<segment>`, ending in `done` or `error`.
The final root report is `done` when every host succeeded and `error`
otherwise, so the result survives the next shell prompt.

Constraints:

- Status writes share the title write path in `internal/connect/title.go` and
  are never interleaved with PTY passthrough bytes.
- `msg` and `title` use sanitized names, then base64, and respect the protocol
  limits (2048 decoded bytes for `msg`, 192 for `title`).
- Host names that do not fit the `id` segment grammar `[A-Za-z0-9_.+-]{1,32}`
  map to a stable short hash.
- Apply the same mode exclusions as titles: no reports for `-N`, `-W`, `-f`,
  `-G`, `-Q`, `-s`, `-T`, SCP, or non-terminal stdout.

### 2. Block label

During an interactive session, nssh sets the block label to the sanitized
inventory name with `session.set_block_label` and restores the previous label
on exit. Labels persist where the terminal title may be overwritten by the
remote shell. Labels also make blocks targetable by name for `rex send` and
`rex capture`.

### 3. Fan-out open

A new command opens interactive sessions for several hosts:

```text
nssh open HOSTS [--layout tabs|grid|split] [--window LABEL]
```

`HOSTS` uses the same comma list and selector rules as root host lists.
Without Rex, the command fails with a clear message rather than opening
sessions serially.

Each block runs its own single-target nssh:

```text
rex run --shell=none --label NAME -- nssh --target NAME
```

Properties:

- Credential, host-key, proxy, and fallback behavior stays in the existing
  single-target path. No new trust code is added.
- Commands use argv with `--shell=none`, so inventory names are never
  interpreted by a shell. Secrets never appear in argv because each block
  resolves its own credentials.
- `grid` builds one `session.new_window` call with a nested split layout.
  Grid size is capped by the existing host-list target cap; the cap for
  visible panes per window is an open question.
- The runtime agent lets one provider unlock serve every block.

### 4. Palette picker

A Lua file defines Rex actions that call nssh:

| Action | Behavior |
| --- | --- |
| `nssh_connect` | Pick one or more hosts, then open them as in fan-out open |
| `nssh_run` | Pick hosts, prompt for a command, run it as a root host list in a new block |

The picker calls `rex.exec{"nssh", "inv", "list", "--json"}` and passes the
result to `rex.ui.pick` with `multiple = true`, `icon = "server"`, the group
as `subtitle`, and the address as `detail`. Actions accept arguments so they
also run non-interactively through `rex do`.

Prerequisites and constraints:

- nssh needs a stable `inv list --json` output. It does not exist today.
- `rex.ui.pick` accepts at most 2,000 items. Larger inventories use a
  two-stage pick: group first, then hosts.
- Distribution: `nssh rex install` writes the Lua file into the Rex config
  and prints the `require` line to add, or the file ships in `contrib/rex/`
  for manual installation. Lua stays outside the Go module either way.

### 5. Batch follow-ups

Root host lists keep attributed output in one pane. When Rex is present, the
summary can offer follow-up actions, starting with opening interactive
sessions to failed hosts in splits. These are explicit flags or prompts, never
automatic.

## Package boundaries

- A new `internal/rex` package holds detection, the CLI adapter, and OSC 7501
  encoding. It has no dependency on `internal/ssh`.
- `internal/connect` calls the status and label hooks alongside the existing
  title handling.
- CLI presentation owns `nssh open`, follow-up prompts, and `nssh rex install`.
- REPL core emits progress events; presentation maps them to status reports.

## SPEC changes

- Replace the statement that the Go/Charm interactive frontend is maintained
  with its new status, either frozen or retired.
- Add the Rex integration contract: optional, detected, silent fallback,
  argv-only launches, and no new credential or trust paths.

## Delivery order

1. OSC 7501 status for interactive sessions and root host lists.
2. Block labels.
3. `nssh open` with `tabs` and `grid` layouts.
4. `inv list --json`, then the palette picker and its installer.
5. Batch follow-ups.

Slices 1 and 2 are testable without Rex by asserting emitted bytes and adapter
calls. Slice 3 needs a fake `rex` executable in tests.

## Risks

- Rex is new and closed source; its CLI and APIs may change. Keep the adapter
  thin, version-probe with `server.version`, and keep every feature optional.
- Rex calls act on the local Rex server only. nssh on a remote host does not
  see the operator's Rex unless the operator uses `rex -S HOST` deliberately.
- Status reports from a misbehaving nssh could mislabel tabs. Reports carry
  only sanitized names and states, never remote output.

## Deferred

- Broadcast input to several panes with `rex send`. Dangerous on network
  devices; if built, it needs an explicit, per-use confirmation.
- Using `rex events` to reconnect dropped sessions or raise notifications.
- Running nssh from a bastion's Rex server through `rex -S HOST`.

## Open questions

- Is the Charm frontend frozen or retired?
- Is `nssh open` a new command or a root flag on host lists without a command?
- What is the pane cap per window for `grid` before overflowing to more windows?
- Should the Lua file ship through `nssh rex install` or only in `contrib/rex/`?
