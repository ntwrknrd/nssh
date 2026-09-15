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

The original root syntax still takes one destination:

```fish
nssh irn-border-sw1 'show env power'
nssh --target repl 'show version'
printf '%s\n' "[ 'irn-border-sw(1,2)' ] ( 'show env power' )" | nssh repl
```

A comma-separated root target is a single hostname. `--target repl` escapes the
new command name when connecting to a host literally named `repl`.

## Execution and output

The default is four simultaneous hosts; `--concurrency N` changes that limit.
Commands run in listed order for each host. Failure skips later commands on that
host while other hosts continue. REPL does not retry commands. Only one
submission runs at a time.

Progress updates while commands run. Complete retained output appears when each
command finishes, with host/command attribution and distinct stdout/stderr.
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

Use `:help` or `nssh repl --explain` for keys and syntax. Tab completes the current target token; up/down
recall history. Page Up/Page Down scroll output. Ctrl-C during a submission
cancels local SSH work and waits for cleanup before returning to the prompt.
Ctrl-C while idle, EOF, `:quit`, or `:exit` exits. Interactive command failures
stay visible; a normal quit returns zero. Cancellation cannot undo commands
already executed remotely or guarantee termination of every remote process.

Interactive history is `$XDG_STATE_HOME/nssh/repl_history`, normally
`~/.local/state/nssh/repl_history`, with mode 0600. It contains submitted text,
which may contain sensitive arguments; it does not store resolved credentials
or remote output. History retains at most 1,000 entries and 1 MiB, and concurrent
REPL instances coordinate updates. Close sessions before deleting this file to
clear history. Piped sessions do not write it.

Capture retains up to 8 MiB of stdout/stderr combined per command. The interactive
transcript retains at most 32 MiB. Truncation or eviction is reported while SSH
output continues draining and the command outcome is recorded. A submission is
limited to 2 MiB before and after suffix expansion, 1,000 unique targets,
100 commands, and 10,000 host/command pairs.

Incremental output streaming, selected-result diffs, a multi-select completion
picker, and a Rust frontend are deferred. See
[frontend-alternatives.md](frontend-alternatives.md) for the evaluated Rust design.
