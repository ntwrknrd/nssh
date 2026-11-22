# Rex terminal integration for nssh

Status: in progress. Slices 1 (OSC 7501 status) and 2 (pane labels) are
implemented for testing; later slices are proposed.

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

- Open one or several inventory hosts as interactive sessions from the root
  command, placed by simple, configurable rules: one nssh session, at most two
  panes per tab by default, with the app switched to the new tab.
- Focus an existing live connection instead of duplicating it.
- Report connection and multi-host progress through Rex program status.
- Offer a Rex command-palette picker for connecting and running commands.
- Degrade silently, with no behavior change, outside Rex.

## Non-goals

- New top-level commands or subcommands.
- Linking against or reimplementing the Rex server protocol. Its transport is
  not documented; the `rex` CLI with `--json` is the stable surface.
- Making Rex a dependency of any existing behavior.
- Changing behavior outside Rex, or for single-host runs with a command.
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

Three forms name several hosts. All of them are resolved before Rex is
involved.

| Form | Use | Example | Status |
| --- | --- | --- | --- |
| Comma list | Explicit hosts | `nssh 'acm-core1, acm-core2'` | Exists at the root today |
| Suffix expansion | Quick one-liner | `nssh 'acm-core(1,2)'`, `nssh 'acm-core(1-4)'` | Exists in REPL; ranges are new |
| Picker multi-select | Many hosts, such as every core | `nssh core`, then select several matches | New picker behavior |

Decisions:

- Suffix expansion reuses the REPL `prefix(...)` grammar instead of inventing
  a second one. Ranges such as `(1-4)` are added to both REPL and root
  resolution.
- The target must be quoted. Unquoted, fish runs `(1,2)` as a command
  substitution and zsh treats it as a glob qualifier, so neither shell passes
  the text to nssh. This matches REPL targets today.
- A trailing-number shorthand such as `acm-core1-2` is rejected. It collides
  with real names that contain hyphens and digits.
- Plural guessing (`acm-cores` meaning every `acm-core*`) is rejected. Picker
  multi-select covers that case.
- Named host sets are deferred; expansion and multi-select cover the known
  cases.

Picker multi-select:

- Smart lookup with several fuzzy matches already offers selection. Inside
  Rex, the picker allows multi-select; selecting several hosts takes the
  multi-host open path. Outside Rex, selection stays single.
- The picker already has a multi-select mode, `FuzzySelectMulti` in
  `internal/ui/fzf.go`. With fzf it toggles with Tab; the huh fallback toggles
  with Space.
- Keep Tab in fzf. Space is part of fzf's query syntax, where it separates
  search terms, so binding it to selection would break searches such as
  `pit2 core`.

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

### 5. Single host inside Rex

An interactive `nssh HOST` inside Rex opens in the `nssh` session like a
multi-host open: one new tab, labeled with the host, which the app switches
to. If the host already has a live pane there, nssh switches to that pane
instead of connecting again. The invoking pane returns to its prompt.

nssh connects in the current pane, as it does today, when any of these hold:

- A command is given, as in `nssh HOST show version`.
- Standard input or output is not a terminal, as in scripts and agents.
- The app cannot be switched to the new tab because Remote Control is off.
  Opening a tab the operator cannot see would look like a hang, so nssh checks
  before launching and falls back to the current pane.
- `rex.single_host: here` is configured.
- The process is the launched pane itself. Launches set `NSSH_REX_PANE=1` so a
  pane never opens another pane.

### 6. Workspace placement and reuse

Placement follows a few fixed rules with configurable defaults. It never
closes or moves panes, and it never adds panes to an existing tab.

```yaml
rex:
  session: nssh          # a session label, "current", or "site"
  panes_per_tab: 2       # 1, 2, or 4
  split: auto            # auto, side-by-side, or stacked
  single_host: tab       # tab, or here for the current pane
```

**Session.** By default every open goes to one session labeled
`nssh`, found case-insensitively or created. `current` uses the invoking
session. `site` uses the hosts' shared site, such as `PIT2`, and falls back to
the `nssh` session when hosts span sites or have none. `site` needs new
non-secret metadata: NetBox discovery reads the device site for filtering but
does not cache it, so provider state must cache it and local inventory needs an
optional `site` field. That work is needed only for this mode.

**Tabs.** Hosts are taken in the requested order and grouped into tabs of
`panes_per_tab`:

| Hosts | Default result (`panes_per_tab: 2`) |
| --- | --- |
| 2 | One tab, split in two |
| 3 | One split tab and one single tab |
| 4 | Two split tabs |
| 5 or more | Split tabs, with a single tab for an odd host |

`panes_per_tab: 4` gives an even 2x2 grid for the rare case that wants one.
`1` gives one tab per host. No other layouts are offered.

**Split direction.** `auto` places two panes side by side when the invoking
window is at least 160 columns wide, and stacks them otherwise.

**Tab names.** A tab is named after its hosts, using the expansion grammar
when they share a prefix: `acm-core(1,2)` for a split tab, `acm-core3` for a
single tab, and `edge1 + core2` when names share no prefix.

**Reuse.** Before placing hosts, nssh looks in the target session for panes
labeled with each host whose process is a live nssh. Those hosts are not
opened again. If every host already has a live pane, nssh focuses the first
one. Otherwise only the remaining hosts are placed in new tabs.

Launches do not pass `rex run --keep-open`, so Rex closes a pane when its
session ends. Dead panes never accumulate, and nssh has nothing to clean up.

**Focus.** nssh focuses the first new tab and asks the app to show it with
`session.select`. Without Remote Control, multi-host opens still place their
panes and print where they opened.

### 7. Palette picker

A Lua file in `contrib/rex/nssh.lua` defines Rex actions that call nssh. It
installs with one documented `require` line in the Rex config; nssh gains no
installer command.

| Action | Behavior |
| --- | --- |
| `nssh_connect` | Pick one or more hosts, then open them as in multi-host open |
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

### 8. Batch follow-ups

Root host lists keep attributed output in one pane. When Rex is present, the
summary can offer to open interactive sessions to failed hosts, using the
same placement and reuse rules. This is an explicit prompt, never automatic.

## Feasibility with Rex 0.1.0

| Need | Available today | Gap or caveat |
| --- | --- | --- |
| Create labeled sessions, tabs, and layouts | Yes | None found |
| Find sessions and panes by label | Yes, through `session.list` and `session.view` | Labels are operator-editable; a renamed pane is not reused |
| Check whether a pane is live | Yes, through the terminal `process` method | Confirm it exposes the command line for matching nssh |
| Choose split direction | Yes, through the terminal `size` method | The target session's window may differ from the invoking pane's window |
| Show the target session in the app | Yes, through `client.perform` with `session.select` | The app accepts CLI actions only with Remote Control enabled; single-host opens fall back to the current pane, multi-host opens print where the panes opened |
| Close panes when sessions end | Yes, the `rex run` default | `--keep-open` would keep them |
| Per-host status | Yes, through OSC 7501 | Display is Rex-defined and may change |
| Palette multi-select | Yes, through `rex.ui.pick` | 2,000-item limit |

Before implementation, verify the `process` and `size` outputs, the
`session.select` behavior, and a way to check Remote Control before launching,
against the installed Rex version.

## Package boundaries

- A new `internal/rex` package holds detection, the CLI adapter, placement,
  reuse, and OSC 7501 encoding. It has no dependency on `internal/ssh`.
- Target expansion lives in the shared resolution path, so REPL, root lists,
  and multi-host open resolve identically.
- Site caching, if `session: site` is built, lives in inventory provider state.
- `internal/connect` calls the status and label hooks alongside the existing
  title handling.
- CLI presentation owns the multi-host open decision, the multi-select picker,
  and follow-up prompts.
- REPL core emits progress events; presentation maps them to status reports.

## SPEC changes

- Replace the statement that the Go/Charm interactive frontend is maintained:
  it is frozen, with fixes only.
- Inside Rex, an interactive single-host `nssh HOST` opens in the `nssh`
  session and switches to it, with the current-pane fallbacks above.
- Amend "Root lists require a command": inside Rex, a multi-host target
  without a command opens interactive sessions; outside Rex the requirement
  stands.
- Add root suffix expansion, with ranges, to the destination grammar, with
  exact host or alias precedence.
- Allow multi-select in smart lookup inside Rex.
- Add the Rex integration contract: optional, detected, silent fallback,
  argv-only launches, no new credential or trust paths, and placement that
  never closes or moves panes.

## Delivery order

1. OSC 7501 status for interactive sessions and root host lists.
2. Pane labels.
3. Opening in the `nssh` session for single hosts and comma lists: split
   tabs, reuse of live panes, app focus, current-pane fallbacks.
4. Suffix expansion with ranges at the root.
5. Multi-select in smart lookup.
6. `inv list --json`, then the palette picker.
7. Batch follow-ups.
8. `session: site`, with site caching, if wanted after use.

Slices 1 and 2 are testable without Rex by asserting emitted bytes and adapter
calls. Slice 3 needs a fake `rex` executable that records calls and serves
canned `session.view` output.

## Risks

- Rex is new and closed source; its CLI and APIs may change. Keep the adapter
  thin, version-probe with `server.version`, and keep every feature optional.
- Reuse by label can misfire if an operator reuses a label for unrelated
  work. Matching also checks that the pane's process is nssh.
- Rex calls act on the local Rex server only. nssh on a remote host does not
  see the operator's Rex unless the operator uses `rex -S HOST` deliberately.
- Status reports carry only sanitized names and states, never remote output.

## Deferred

- Broadcast input to several panes with `rex send`. Dangerous on network
  devices; if built, it needs an explicit, per-use confirmation.
- Using `rex events` to reconnect dropped sessions or raise notifications.
- Running nssh from a bastion's Rex server through `rex -S HOST`.
- Named host sets in inventory YAML, such as `nssh acm-cores`.
