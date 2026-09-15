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

## Guided interactive prompt

Run `nssh repl` to pick inventory hosts and enter commands without learning the
quoted submission syntax:

1. Type to filter the inventory. Use Up/Down to move and Space to select hosts.
   Selection follows the order you pick hosts and survives filter changes.
   Ctrl-A selects all matches; Ctrl-X clears the selection.
2. Press Enter to move to commands. If no hosts were selected, Enter selects
   the highlighted host first. Type one command per line; Enter adds another.
3. Press F5 to run. Tab switches between hosts and commands. The selected hosts
   and command text remain available after the run for editing or repetition.

Ctrl-P/Ctrl-N recalls previous submissions. F2 switches to the original syntax
editor for literal hosts, patterns, selectors, or pasted submission syntax;
F2 returns to the guided draft. Plain mode continues to use the original syntax.
Guided inventory selections are literal, and command quotes and backslashes are
passed through without adding submission quoting.

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

The Go TUI has a guided inventory and command editor, a syntax editor, and
persistent running/done/failed/pending/canceled/skipped counts. Results stay
under their command heading. At 100 columns or wider, adjacent devices for the
same command appear side by side; narrow terminals stack them. Long output lines
wrap within each pane. Trailing table padding is removed for display; real blank
lines and indentation remain. Paired source rows stay aligned when either side
wraps, and continuation rows do not receive new line numbers. Stderr, failures,
and truncation remain visible.

Use `:help` or `nssh repl --explain` for keys and syntax:

- In the syntax editor, Tab completes a unique hostname or opens the picker. Space selects hosts,
  Up/Down moves, Enter inserts selected hosts, and Escape closes the picker.
- In the syntax editor outside the picker, Up/Down recalls history. Page Up/Page Down and the mouse
  wheel scroll output.
- Ctrl-L switches between automatic split layout and stacked full-width results.
- Ctrl-G highlights differing displayed lines in paired panes. This is a line
  comparison; it does not infer semantic differences in device output.
- Drag selects displayed output lines within one device. Ctrl-Y sends up to
  64 KiB to the terminal clipboard using OSC 52, if the terminal supports it.
  Selection excludes line numbers and adjacent devices. Resize or new output
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
