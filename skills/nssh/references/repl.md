# Terminal interface and multi-host commands

Run `nssh --tui`. It starts in **batch** mode. Enter `:interactive` to choose
devices and open live SSH panes; `:batch` closes that group and returns to batch.
The old `repl` subcommand is removed: `nssh repl` addresses a host named repl.

## Batch requests

One bar contains the complete request:

```text
[ 'irn-border-sw1', 'irn-border-sw2' ] ( 'show env power' )
[ 'irn-border-sw(1,2)' ] ( 'show env power', 'show version' )
[ 'select:provider:netbox' ] ( 'show version' )
[ 'operator@edge1', '2001:db8::10' ] ( 'show version' )
```

Enter runs a filled request. Up/Down or Ctrl-P/N recalls the whole request,
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
stack. Ctrl-L toggles stacked results, and Ctrl-G compares displayed lines in
paired results. Comparison does not infer semantic differences.

## Interactive sessions

Enter `:interactive`, select devices, accept the picker, then press Enter to open
the sessions. One live terminal pane appears per device. The group stays fixed
until it closes. EOS, Junos, and Linux retain their native shells and prompts:
nssh does not replace the shell, disable pagination, parse prompts, or run
initialization or configuration commands.

The shared command bar shows **Sending to ALL** followed by its target devices.
Enter sends the bar's text and a carriage return to those terminals. Each terminal
keeps its own working directory, variables, CLI hierarchy, and output. Commands
need not complete before the next input can be sent.

Wait for each device's actual prompt before sending input. To handle a
confirmation or different state, click its pane header or enter `:target N`. This targets
only that device. Enter `:all` to restore broadcast. nssh cannot determine whether
different prompts are safe to answer together. A disconnected session pauses
broadcast, never reconnects, and never replays input. Sending to all still requires
every targeted session to be open; focus a remaining pane or reopen the group.

Interactive history contains command-bar submissions for the current group.
Up/Down recalls them without changing the devices. This history stays in memory,
is separate from saved batch history, and disappears when the group closes.
Avoid putting secrets in the command bar; use direct keyboard input for password
prompts so replies are not added to local history.

### Direct keyboard input

Enter `:keys` or press Ctrl-] to send keys directly to the selected terminals.
This supports remote line editing, confirmations, pagination, and interactive
programs. Ctrl-] returns to the local command bar. Remote input remains as it was;
switching input modes does not clear a partially typed remote line.

Tab from the local bar sends the current draft followed by Tab and enters direct
input for remote completion. Subsequent typing and Enter act on the remote line.
Direct input uses each remote shell's own history. It does not populate the local
command-bar history.

Ctrl-C and Ctrl-D are sent to the selected terminals in interactive mode. Use
`:quit` from the local command bar to exit the TUI. A remote command starting with
`:` can use direct input or a leading space in the command bar.

### Pane controls and limits

- `:target N` or clicking a pane header focuses one device; `:all` selects all.
- `:next` and `:prev` change pages when more than four panes are open.
- PgUp/PgDn scroll the targeted panes; the mouse wheel scrolls its pane.
- Drag selects output lines from one pane. Ctrl-Y copies; right-click copies and
  clears the selection after a successful write. Clipboard copies use OSC 52
  and are limited to 64 KiB. New output or resize clears selection.
- `:clear` or Ctrl-K clears scrollback while keeping history. In interactive
  mode, the current terminal screen remains visible.
- `:wipe` also clears history for the current mode. Interactive wipe does not
  erase batch history or the remote shell's own history.
- `:batch` or `:disconnect` closes the group and returns to batch.
- `:quit` or `:exit` closes the TUI and its local SSH terminals.

Interactive mode supports 1-16 devices, up to four panes per page, and 1000
scrollback rows per pane. Terminal dimensions follow pane size. Rendered terminal
controls remain inside each virtual pane; remote clipboard requests do not reach
the outer terminal. Terminal emulation is provided by the pinned Charm VT package;
full application and platform coverage still requires representative testing.

## Shared controls and plain input

The bottom-right `:help` hint opens a scrollable overlay. Esc or Enter closes it.
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
