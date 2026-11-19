# Rex terminal integration for nssh

Status: proposed design. Nothing here is implemented.

## Summary

When nssh runs inside the Rex terminal, it can use Rex for presentation that
would otherwise require an nssh TUI: sessions, tabs, split panes, a
command-palette inventory picker, and per-host status on tabs and in the
session picker. Rex is an optional presentation backend. The batch CLI and
plain REPL remain the baseline and work unchanged without Rex.

The Charm interactive frontend is frozen: it stays shipped and supported for
fixes, with no new features.

The integration adds no new commands. Multi-host interactive work goes through
the existing root invocation, and Rex placement is decided automatically.

## Goals

- Open several inventory hosts as interactive sessions from the root command,
  placed in a Rex session, tab, and pane layout chosen for them.
- Reuse an existing session, tab, or pane for the same work instead of
  duplicating it.
- Report connection and multi-host progress through Rex program status.
- Offer a Rex command-palette picker for connecting and running commands.
- Degrade silently, with no behavior change, outside Rex.

## Non-goals

- New top-level commands or subcommands.
- Linking against or reimplementing the Rex server protocol. Its transport is
  not documented; the `rex` CLI with `--json` is the stable surface.
- Making Rex a dependency of any existing behavior.
- Changing single-host behavior. `nssh HOST` still connects in the current
  pane.
- Broadcasting keystrokes to several panes. See [Deferred](#deferred).
- Replacing attributed batch output with one pane per host by default.

## Rex surfaces used

| Surface | Use in nssh |
| --- | --- |
| Environment: `REX_SESSION`, `REX_BLOCK`, `REX_SERVER`, `REX_BIN_DIR` | Detection and targeting |
| `session.list`, `session.view` | Find existing sessions, tabs, and panes by label |
| `session.create`, `session.new_window`, `session.new_split` | Create sessions, tabs, and layouts |
| `com.superlogical.terminal.process`, `com.superlogical.terminal.size` | Check whether a pane is live; size layouts |
| `session.set_label`, `session.set_window_label`, `session.set_block_label` | Name sessions, tabs, and panes |
| `client.perform` with the app's `session.select` action | Show the target session and tab in the app |
| OSC 7501 program status | Connection state and per-host progress |
| Lua actions with `rex.ui.pick` and `rex.ui.input` | Palette picker and prompts |

References: Rex documentation pages for the CLI, custom UI, actions, Lua
scripts, Lua events, program status, and server API under
`https://www.superlogical.com/rex/docs/`, plus `rex api describe` output from
Rex 0.1.0.

## Detection

nssh treats Rex as present when all of the following hold:

1. `REX_SESSION` and `REX_BLOCK` are set.
2. A `rex` executable resolves, preferring `$REX_BIN_DIR/rex`.
3. For features that call the CLI, a `rex whoami --json` probe succeeds within
   a short timeout. The result is cached for the process.

OSC 7501 status needs only the first condition, since terminals ignore unknown
OSC sequences.

Operators can disable integration with a YAML setting and the `NSSH_REX=0`
environment variable, which keeps the root option grammar unchanged. A failed
probe or Rex CLI error never fails the nssh operation; it falls back to the
non-Rex path and logs at verbose level.

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

### 2. Pane label

During an interactive session, nssh sets the block label to the sanitized
inventory name with `session.set_block_label` and restores the previous label
on exit. Labels persist where the terminal title may be overwritten by the
remote shell, make panes targetable for `rex send` and `rex capture`, and are
how [workspace reuse](#5-workspace-placement-and-reuse) finds panes.

### 3. Multi-host targets at the root

Three existing or small extensions to target resolution can name several
hosts. All of them are resolved before Rex is involved.

| Form | Example | Status |
| --- | --- | --- |
| Comma list | `nssh 'acm-core1, acm-core2'` | Exists at the root today |
| Suffix expansion | `nssh 'acm-core(1,2)'`, `nssh 'acm-core(1-4)'` | Exists in REPL; ranges are new |
| Named host set | `nssh acm-cores` | New YAML concept |
| Multi-select from smart lookup | `nssh acm-core`, then select several matches | New picker behavior |

Notes:

- `prefix(...)` reuses the REPL grammar instead of inventing a second one.
  Ranges such as `(1-4)` are added to both REPL and root resolution.
  Parentheses need quoting in fish and zsh, the same as REPL targets today.
- A trailing-number shorthand such as `acm-core1-2` is rejected as a range
  syntax. It collides with real names that contain hyphens and digits, and an
  unmatched literal host would silently change meaning.
- Plural guessing (`acm-cores` meaning every `acm-core*`) is rejected. It is
  unpredictable and has no inventory authority.
- A named host set is an inventory-owned entry that resolves to several hosts
  by explicit list or by an `nssh inv list` selector. It also supplies the tab
  name, and optionally the session name and layout. An exact host or alias
  match takes precedence over a set with the same name.

  ```yaml
  inventory:
    sets:
      acm-cores:
        hosts: [acm-core1, acm-core2]
        # Or: select: "host:acm-core group:corp"
  ```

- Smart lookup with several fuzzy matches already offers selection. Inside
  Rex, the picker allows multi-select; selecting several hosts takes the
  multi-host open path. Outside Rex, selection stays single.

### 4. Multi-host open without a command

Today a root host list requires a command. This design extends that rule:

- With a command, a multi-host target runs as a root host list, with
  attributed output in the current pane. This is unchanged inside Rex.
- Without a command inside Rex, nssh opens one interactive session per host
  and returns once every pane is launched.
- Without a command outside Rex, nssh keeps today's error and adds a hint to
  pass a command or use REPL.

Each pane runs its own single-target nssh:

```text
nssh --target HOST
```

launched by Rex with `--shell=none` and an argv list. Properties:

- Credential, host-key, proxy, and fallback behavior stays in the existing
  single-target path. No new trust code is added.
- Inventory names are never interpreted by a shell. Secrets never appear in
  argv because each pane resolves its own credentials.
- The runtime agent lets one provider unlock serve every pane.
- The launcher passes `--target` with the resolved inventory name, so each
  pane skips fuzzy selection.

### 5. Workspace placement and reuse

nssh derives a desired workspace (session, tab, and panes) and reconciles Rex
toward it. It never closes or moves panes it did not create in this
invocation.

**Session name.** In order of preference:

1. The host set's configured session.
2. The shared site of every host, such as `PIT2`.
3. The current session, when hosts span several sites or have no site.

Site needs new non-secret metadata: NetBox discovery already reads the device
site for filtering but does not cache it. Cache the site name in provider
state, and allow an optional `site` on local inventory groups and hosts.

**Tab name.** In order of preference:

1. The host set name, such as `acm-cores`.
2. The expansion as written, such as `acm-core(1,2)`.
3. The hosts' longest common name prefix, such as `acm-core`.
4. The first host plus a count, such as `acm-core1 +1`.

**Reconciliation.**

1. Find the session by label, case-insensitively, or create it.
2. Find the tab in that session by label, or create it with the full layout.
3. In an existing tab, match panes by label to hosts. Keep a pane whose
   process is a live nssh. Add a split for each host without a live pane.
   Leave other panes alone, including exited ones, unless the operator
   enabled replacing exited panes.
4. Focus the tab and ask the app to show it with `session.select`.

Running the same command twice therefore focuses the existing tab instead of
opening duplicates, and reconnects only the hosts whose sessions dropped.

**Layout.** The layout is computed, not configured per host count:

- Measure the window from the invoking pane's terminal size.
- Fit panes to a minimum usable size, defaulting to 80 by 24 cells.
- Two hosts split side by side when the width fits, otherwise stacked.
- Three or more form a near-square grid.
- Hosts beyond one window's capacity overflow into additional tabs named
  `<tab> 2`, `<tab> 3`, and so on.
- A host set may override the layout. A global minimum pane size is the only
  other setting.

### 6. Palette picker

A Lua file in `contrib/rex/nssh.lua` defines Rex actions that call nssh. It
installs with one documented `require` line in the Rex config; nssh gains no
installer command.

| Action | Behavior |
| --- | --- |
| `nssh_connect` | Pick one or more hosts or sets, then open them as in multi-host open |
| `nssh_run` | Pick hosts, prompt for a command, run it as a root host list in a new pane |

The picker calls `rex.exec{"nssh", "inv", "list", "--json"}` and passes the
result to `rex.ui.pick` with `multiple = true`, `icon = "server"`, group and
site as `subtitle`, and the address as `detail`. Actions accept arguments so
they also run non-interactively through `rex do`.

Prerequisites and constraints:

- nssh needs a stable `--json` output for `inv list`. It does not exist today.
  This is a flag on an existing command.
- `rex.ui.pick` accepts at most 2,000 items. Larger inventories use a
  two-stage pick: site or group first, then hosts.

### 7. Batch follow-ups

Root host lists keep attributed output in one pane. When Rex is present, the
summary can offer to open interactive sessions to failed hosts, using the
same placement and reuse rules. This is an explicit prompt, never automatic.

## Feasibility with Rex 0.1.0

| Need | Available today | Gap or caveat |
| --- | --- | --- |
| Create labeled sessions, tabs, and layouts | Yes | None found |
| Find sessions, tabs, and panes by label | Yes, through `session.list` and `session.view` | Labels are operator-editable; a renamed pane is not reused |
| Check whether a pane is live | Yes, through the terminal `process` method | Confirm it exposes the command line for matching nssh |
| Measure window size | Yes, through the terminal `size` method | The new window may differ from the invoking pane's window |
| Show the target session in the app | Yes, through `client.perform` with `session.select` | The app accepts CLI actions only with Remote Control enabled; otherwise print where the panes opened |
| Per-host status | Yes, through OSC 7501 | Display is Rex-defined and may change |
| Palette multi-select | Yes, through `rex.ui.pick` | 2,000-item limit |

Before implementation, verify the `process` and `size` outputs and the
`session.select` behavior against the installed Rex version.

## Package boundaries

- A new `internal/rex` package holds detection, the CLI adapter, workspace
  reconciliation, and OSC 7501 encoding. It has no dependency on
  `internal/ssh`.
- Target expansion and host sets live in inventory and the shared resolution
  path, so REPL, root lists, and multi-host open resolve identically.
- Site caching lives in inventory provider state.
- `internal/connect` calls the status and label hooks alongside the existing
  title handling.
- CLI presentation owns the multi-host open decision, the multi-select picker,
  and follow-up prompts.
- REPL core emits progress events; presentation maps them to status reports.

## SPEC changes

- Replace the statement that the Go/Charm interactive frontend is maintained:
  it is frozen, with fixes only.
- Amend "Root lists require a command": inside Rex, a multi-host target
  without a command opens interactive sessions; outside Rex the requirement
  stands.
- Add host sets and root suffix expansion to the destination grammar, with
  exact host or alias precedence.
- Add the Rex integration contract: optional, detected, silent fallback,
  argv-only launches, no new credential or trust paths, and reconciliation
  that never closes panes it did not create.

## Delivery order

1. OSC 7501 status for interactive sessions and root host lists.
2. Pane labels.
3. Multi-host open for comma lists, creating a new tab in the current session.
4. Workspace reuse and computed layout.
5. Suffix expansion with ranges at the root, and host sets.
6. Site caching and session placement by site.
7. Multi-select in smart lookup.
8. `inv list --json`, then the palette picker.
9. Batch follow-ups.

Slices 1 and 2 are testable without Rex by asserting emitted bytes and adapter
calls. Slices 3 and 4 need a fake `rex` executable that records calls and
serves canned `session.view` output.

## Risks

- Rex is new and closed source; its CLI and APIs may change. Keep the adapter
  thin, version-probe with `server.version`, and keep every feature optional.
- Reconciliation by label can misfire if an operator reuses a label for
  unrelated work. Matching also checks that the pane's process is nssh.
- Rex calls act on the local Rex server only. nssh on a remote host does not
  see the operator's Rex unless the operator uses `rex -S HOST` deliberately.
- Status reports carry only sanitized names and states, never remote output.

## Deferred

- Broadcast input to several panes with `rex send`. Dangerous on network
  devices; if built, it needs an explicit, per-use confirmation.
- Using `rex events` to reconnect dropped sessions or raise notifications.
- Running nssh from a bastion's Rex server through `rex -S HOST`.

## Open questions

- Should host sets live under `inventory.sets` or reuse inventory groups with
  an opt-in flag?
- Should exited panes in a reused tab be replaced by default?
- Is 80 by 24 the right minimum pane size for network device sessions?
