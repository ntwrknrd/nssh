# Multi-host REPL

`nssh repl` is the normal multi-host command in the 0.3 development series.
Go/Charm provides the interactive interface when stdin and stdout are terminals.
`--plain` or piped input uses line-oriented output. No feature flag is required.

## Grammar

Enter one submission per line:

```text
[ 'irn-border-sw1', 'irn-border-sw2', 'irn-agg-sw1', 'irn-agg-sw2' ] ( 'show env power' )
[ 'irn-border-sw(1,2)', 'irn-agg-sw(1,2)' ] ( 'show env power', 'show version' )
[ 'select:provider:netbox' ] ( 'show version' )
[ 'operator@edge1', '2001:db8::10' ] ( 'show version' )
```

Targets and commands must be single quoted and separated with commas. `\'`
escapes a quote; other backslashes are preserved. A trailing `prefix(1,2)` expands
suffixes. Empty values, trailing commas, ranges, and nested expansions are not
supported. Commands pass unchanged to the remote command mechanism; this is not
a persistent remote shell.

One `select:` expression may replace the target list. It uses `nssh inv list`
matching: case-insensitive regular expressions for plain terms, exact
`field:value` matches, and AND across terms. Fields are `host`, `hostname`, `id`,
`user`, `port`, `provider`, and `group`. Use inventory values for provider/group;
selectors cannot be mixed with explicit hosts.

Exact managed names and aliases retain inventory policy. Unmatched targets use
literal SSH resolution without fuzzy selection or creating inventory entries.
Equivalent aliases with the same configured username are deduplicated; different
usernames and separately configured identities remain distinct. Config and the
local inventory catalog reload for each submission; REPL does not refresh remote
inventory itself.

For one remote command across a small explicit set, root syntax also accepts a
bare comma list. It shares REPL's four-worker default and execution limits:

```fish
nssh 'irn-border-sw1, irn-border-sw2' 'show env power'
nssh --target repl 'show version'
printf '%s\n' "[ 'irn-border-sw(1,2)' ] ( 'show env power' )" | nssh repl
```

Root lists require a command. Use the REPL grammar for several commands, target
expansion, or selectors. `--target repl` escapes the command name when connecting
to a host literally named `repl`.

## Interactive command prompt

Run `nssh repl` to edit a submission directly. Press Enter to run it.

- Tab completes a unique host at the cursor or opens a searchable host picker.
  Tab on an empty prompt starts a submission and opens the picker.
- In the picker, type to filter, use Up/Down to move, and Space to select hosts.
  Selections persist across filters. Enter inserts the selected hosts into the
  command; it does not execute it. Esc closes the picker and restores the draft.
- Selected hosts are quoted and separated automatically. Existing command text
  and an explicit username remain intact. A new submission places the cursor
  in the command field after inserting hosts.
- Up/Down outside the picker recalls history. The submitted syntax is stored
  exactly as entered after trimming outer whitespace.
- The status row keeps a `:help` hint at the bottom right. Enter `:help` to open
  a scrollable help and command index overlay. Up/Down, Page Up/Page Down, or the
  mouse wheel scroll the pane; Esc or Enter closes it without changing output.

Older form history is converted to editable syntax when it preserves the same
hosts and commands. Entries that cannot be converted safely show an explanation
and leave the current draft intact.

## Execution and output

The default is four simultaneous hosts; `--concurrency N` changes that limit.
Each command runs across all hosts, with results printed in requested host
order, before the next command starts. A slow earlier host can delay later
hosts because the execution window also bounds buffered output. Failure skips
later commands on that host while other hosts continue. REPL does not retry commands. Only one
submission runs at a time.

Progress updates while commands run. Complete retained output appears in requested host order, with host/command attribution and distinct stdout/stderr.
Plain mode sends remote stdout to stdout and remote stderr/status to stderr,
without a prompt or UI styling. It processes lines sequentially and stops at the
first failed submission. Exit codes are zero for success, one for a failed
submission, and 130 for interruption; results show original per-command exits.

Remote stdin is EOF. Commands requiring input must be run through ordinary
`nssh HOST` instead. Authenticate credential providers before starting REPL;
REPL uses existing sessions and never starts provider sign-in or unlock prompts.
Interactive host-key decisions use one modal prompt at a time; the REPL
keeps ownership of terminal input. Plain mode reports trust decisions as errors; establish trust in an
ordinary interactive connection before retrying.

## Keys, history, and limits

The Go TUI has a command prompt with host completion and
persistent running/done/failed/pending/canceled/skipped counts. Each result
starts with its status, device, and command: `OK:  [user@device] ('show env power')`.
The full device/command status line stays pinned above its visible output.
Select the status line alone or drag into the output to copy them together.
A pinned heading is copied once, without including output above the visible area.
At 100 columns or wider, adjacent devices for the same command appear side by
side only when both headings and every output line fit, including line-number
space. Otherwise, devices stack at full width. Lines wrap only when they exceed
the available full width. Trailing table padding is removed for display; real
blank lines and indentation remain. Stderr, failures,
and truncation remain visible.

Use `:help` or `nssh repl --explain` for keys and syntax:

- Tab completes a unique hostname or opens the picker. Type to filter, Space selects
  hosts, Up/Down moves, Enter inserts selected hosts, and Escape closes the picker.
- Outside the picker, Up/Down recalls history. Page Up/Page Down and the mouse
  wheel scroll output.
- Ctrl-L switches between automatic split layout and stacked full-width results.
- Ctrl-G highlights differing displayed lines in paired panes. This is a line
  comparison; it does not infer semantic differences in device output.
- Drag selects displayed output lines within one device. Ctrl-Y sends up to
  64 KiB to the terminal clipboard using OSC 52, if the terminal supports it.
  Right-click also copies and clears the selection after a successful clipboard
  write. Selection excludes line numbers and adjacent devices. Resize or new output
  clears selection. Native terminal selection depends on the terminal's mouse
  override shortcut.

Remote terminal-control sequences are removed before TUI rendering.
 Ctrl-C during a submission
cancels local SSH work and waits for cleanup before returning to the prompt.
Ctrl-C while idle, EOF, `:quit`, or `:exit` exits. Interactive command failures
stay visible; a normal quit returns zero. Cancellation cannot undo commands
already executed remotely or guarantee termination of every remote process.

Interactive history is `$XDG_STATE_HOME/nssh/repl_history`, normally
`~/.local/state/nssh/repl_history`, with mode 0600. It contains submitted hosts and command text,
which may contain sensitive arguments; it does not store resolved credentials
or remote output. History retains at most 1,000 entries and 1 MiB, and concurrent
REPL instances coordinate updates. Close sessions before deleting this file to
clear history. Piped sessions do not write it.

Capture retains up to 8 MiB of stdout/stderr combined per command. The interactive
transcript retains at most 32 MiB and 4,096 display blocks. Truncation or eviction is reported while SSH
output continues draining and the command outcome is recorded. A submission is
limited to 2 MiB before and after suffix expansion, 1,000 unique targets,
100 commands, and 10,000 host/command pairs.

Incremental output streaming, selected-result diffs, a multi-select completion
picker, and a Rust frontend are deferred. See
[frontend-alternatives.md](frontend-alternatives.md) for the evaluated Rust design.
