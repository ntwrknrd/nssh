# REPL TUI Adapter Comparison

## Summary

`nssh repl` currently uses Bubble Tea for the interactive TUI. Bubble Tea is good
for Go-native application structure, but recent REPL work has exposed pain in
the exact areas this feature depends on most: terminal input parsing, mouse
scrolling, selection/copy behavior, and precise viewport rendering.

The right way to compare alternatives is not to rewrite the REPL core. Keep the
Go REPL backend stable, then build small frontend adapters that exercise the
same data flow with different TUI stacks.

## Current Constraints

Terminal TUIs have hard limits regardless of framework:

- Smooth scrolling is row-based, not pixel-based.
- Transparency only works where the app does not paint a background.
- Enabling mouse mode usually compromises terminal-native drag selection.
- Mouse input is escape-sequence parsing; better frameworks reduce leaks, but
  they do not remove the underlying protocol model.

## Recommended Architecture

Add a small JSON broker mode and keep REPL domain logic in Go:

- `internal/repl` remains the source of truth for parsing, target resolution,
  history, hostname suggestions, fanout, SSH capture, and result events.
- `nssh repl broker --json` reads newline-delimited JSON requests from stdin.
- The broker writes newline-delimited JSON events to stdout.
- Experimental frontends run as separate processes and talk to the broker.

This keeps SSH, credential, host resolution, and inventory behavior identical
while letting us compare terminal rendering and input handling independently.

## Broker Requests

Candidate request types:

- `suggest`: return hostname completions for a target prefix.
- `submit`: parse, resolve, and run a REPL command line.
- `cancel`: cancel the active fanout batch.
- `history`: load or append REPL command history.

## Broker Events

Candidate event types:

- `started`: a target worker started.
- `completed`: a target produced output, error text, and exit status.
- `status`: running, done, failed, and pending counts changed.
- `error`: parser, resolver, broker, or fanout-level failure.

## Adapter Candidates

### Ratatui

Ratatui is the best first comparison target. It is Rust-based and uses terminal
backends such as Crossterm, Termion, and Termwiz for raw mode, alternate screen,
mouse capture, terminal sizing, and styled output.

Pros:

- Strong Rust TUI ecosystem.
- Mature rendering and layout model.
- Multiple backend options if Crossterm is not ideal.
- Good fit for a separate frontend binary.

Cons:

- Requires a Rust experimental frontend.
- Ratatui is primarily rendering/layout; input behavior still depends on the
  backend.
- Not an in-process Go adapter.

### OpenTUI

OpenTUI is worth a spike, but it is higher uncertainty. Its core is written in
Zig with TypeScript bindings, currently Bun-focused, and the native core exposes
a C ABI. There is also an `opentui_rust` crate, but it is described as a
rendering engine rather than a full application framework.

Pros:

- Modern terminal renderer.
- Explicit focus on correctness, stability, and performance.
- Potentially useful if its renderer/input model handles terminal edge cases
  better than Bubble Tea.

Cons:

- Runtime and packaging story is less aligned with a Go CLI.
- TypeScript/Bun adapter would add a different toolchain.
- Rust OpenTUI may require more low-level event-loop work.

### Vaxis

Vaxis is the Go-native alternative worth keeping in mind. It targets modern
terminal capabilities and would be easier to integrate in-process than Ratatui
or OpenTUI. It is not the main comparison target here only because the question
was specifically about Ratatui and OpenTUI.

## Comparison Scope

Each adapter should implement only enough UI to test the pain points:

- Bottom prompt.
- Transcript viewport.
- Mouse wheel scrolling.
- Prompt editing.
- Hostname completion.
- Split diff rendering.
- Large transcript performance.
- Terminal escape leakage under rapid scroll.
- Selection and copy behavior.
- Color, background, and transparency behavior.

## Recommendation

Build the Ratatui adapter first. If Ratatui still has the same mouse and
selection problems, OpenTUI is unlikely to justify a bigger dependency and
runtime jump without a very specific win.

Keep the broker protocol small and disposable until the spike proves that a
non-Bubble-Tea frontend is materially better.
