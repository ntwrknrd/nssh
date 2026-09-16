# Terminal interface and multi-host commands

Run `nssh --tui`. It starts in **batch** mode. Open controls with Ctrl-P and enter `:interactive` to choose
devices and open live SSH panes, or resume connected tabs.
The old `repl` subcommand is removed: `nssh repl` addresses a host named repl.

## Batch requests

One bar contains the complete request:

```text
[ 'irn-border-sw1', 'irn-border-sw2' ] ( 'show env power' )
[ 'irn-border-sw(1,2)' ] ( 'show env power', 'show version' )
[ 'select:provider:netbox' ] ( 'show version' )
[ 'operator@edge1', '2001:db8::10' ] ( 'show version' )
```

Enter runs a filled request. Up/Down recalls the whole request,
including its devices, so either part can be edited before running again.
Identical requests move to the newest history position. History uses the existing
private XDG state file `nssh/repl_history`, bounded to 1000 entries or 1 MiB.
Older form entries are converted only when their meaning can be preserved.

Typing a hostname into an empty bar starts the quoted request template.
Deletion stays inside quoted values and preserves delimiters. Shift-Tab moves
between device and command fields; Alt-Enter inserts another quoted value.
If the command field is empty, Enter moves into it instead of running.

Tab opens the device picker. Type to filter, use Space to select and Up/Down to
move, then Enter to insert the selection. Selections persist across filters.
Esc restores the draft. Reopening an exact device starts a fresh filter;
Tab within the picker clears selection. The picker fills the request bar.

Values use single quotes and commas. `\'` escapes a quote; other backslashes
remain unchanged. Trailing `prefix(1,2)` expands suffixes. Empty values, ranges,
trailing commas, and nested expansions are rejected.

A single `select:` expression can replace the device list. It uses `nssh inv list`
matching: regular expressions for plain terms, exact `field:value` matches, and
AND across terms. Fields include host, hostname, id, user, port, provider, and
group. Selectors cannot mix with explicit devices. Exact inventory names and
aliases retain inventory policy; unmatched targets use literal SSH resolution.
Equivalent resolved identities are deduplicated. Each request reloads local
config and inventory without refreshing remote inventory.

### Execution and output

Each command runs across the hosts before the next command starts. Results
appear in requested host order. Four workers run by default; `--concurrency N`
changes that limit. A slow earlier host can delay later results because the
execution window bounds buffered output. Failure skips later commands on that
host. Commands are never retried; one request runs at a time.

Batch executes each command separately with remote stdin at EOF. It preserves
stdout, stderr, and exit status. Use interactive mode when commands need input
or must retain shell state.

Results start with `OK:  [user@device] ('command')`. The status heading stays
pinned above its visible output and can be selected with the output. Results
appear beside each other only when both fit without wrapping; otherwise they
stack. The `:stacked` overlay command toggles stacked results, and Ctrl-G compares displayed lines in
paired results. Comparison does not infer semantic differences.

## Interactive sessions

Open controls with Ctrl-P and enter `:interactive`, select devices, accept the picker, then press Enter to open
the sessions. One live terminal pane appears per device. The group stays fixed
until it closes. EOS, Junos, and Linux retain their native shells and prompts:
nssh does not replace the shell, disable pagination, parse prompts, or run
initialization or configuration commands.

Keyboard input goes directly to the targets listed in **Sending to ALL** or
**Sending to**. Space, Enter, and q work at pagers; Tab and arrows use remote
completion and history. Ctrl-C and Ctrl-D go to the selected terminals.
Each terminal keeps its working directory, variables, CLI hierarchy, and output.

Wait for each device's prompt before sending input. Click a pane top border to target
only that device. Output selection does not change targets. A disconnected
session pauses broadcast. Reconnection requires an explicit control command,
and input is never replayed.

### Local controls and tabs

Press Ctrl-P to open the local control overlay. While it is open, typing stays
local. Esc or Ctrl-P returns to direct input. These commands run in the overlay:

- `:new` selects devices and opens a new session tab.
- `:tab N` switches tabs; clicking a tab also switches it. Alt-Left and Alt-Right
  cycle tabs without opening controls, wrapping at either end.
- `:close` disconnects the current tab's devices.
- `:reconnect` reconnects disconnected panes in the current tab. `:reconnect N`
  reconnects only pane N. Live sessions stay open; old output remains visible.
  Each reconnection starts a fresh SSH session. Wait for prompts, then use
  `:all` to resume paused broadcast.
- `:scroll-lock` toggles linked scrolling for the current tab. It starts enabled.
- `:target N` focuses one device; `:all` restores broadcast to this tab.
- `:next` and `:prev` change pages when more than four panes are open.
- `:clear` clears this tab's scrollback; the current screen stays visible.
- `:copy` copies selected output.
- `:batch` returns to batch while keeping session tabs connected.
- `:help` opens the full command index.
- `:quit` closes every session and exits.

Only the active tab receives keyboard input. Background tabs keep receiving
output. From batch, use Ctrl-P then `:interactive` to resume the tabs. Remote shells own interactive
history; nssh saves only complete batch requests in its history file.

The mouse wheel scrolls all panes in the tab together by default, with each pane
stopping at its own scrollback limit. Disable scroll lock to scroll only the
pane under the pointer. Drag selects the device header and output lines from one pane. The header
keeps its identity when the session disconnects; connection state appears in the
footer. Direct input does not provide a tracked command label.
Right-click copies and clears selection after a successful write. Clipboard
copies use OSC 52 and are limited to 64 KiB. New output or resize clears selection.

Interactive mode supports eight tabs with 1-16 devices each, up to four panes per page, and 1000
scrollback rows per pane. Terminal dimensions follow pane size. Rendered terminal
controls remain inside each virtual pane; remote clipboard requests do not reach
the outer terminal. Terminal emulation is provided by the pinned Charm VT package;
full application and platform coverage still requires representative testing.

## Shared controls and plain input

Ctrl-P opens local controls in either mode; `:help` opens the help index from
there. Esc or Enter closes help. Ctrl-K and Ctrl-L clear scrollback in either
mode. The overlay command `:wipe` also erases saved batch history.
Batch output supports the same drag and clipboard controls as interactive panes,
including selectable status headings. In batch, Ctrl-C cancels active work and
waits for local cleanup, or exits when idle. Closing or canceling local SSH cannot
undo effects of commands already sent to devices.

Authenticate credential providers before starting the TUI. Shared connection
preparation owns inventory resolution, credentials, proxies, host-key policy, and
audit context. Host-key approval uses one modal prompt at a time. Foreground
interactive terminals neither reuse nor leave behind a shared SSH master.

`--plain` or piped input uses batch grammar without history writes. Stdout remains
stdout; stderr and status go to stderr. Processing stops on failure (exit 1);
interruption exits 130. Plain mode cannot prompt for host-key approval.

```fish
nssh 'irn-border-sw1, irn-border-sw2' 'show env power'
printf '%s\n' "[ 'irn-border-sw(1,2)' ] ( 'show env power' )" | nssh --tui
nssh --tui --explain
```

Root multi-host commands remain linear and use the batch scheduler. Ordinary
single-host SSH is unchanged.
