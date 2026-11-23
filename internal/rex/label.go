package rex

import (
	"context"
	"log/slog"
)

// maxLabelBytes keeps labels within Rex's 128-character limit.
const maxLabelBytes = 128

// LabelPane labels the invoking pane after name for the life of a session
// and returns a function that restores the previous label. Labeling runs in
// the background so it never delays the connection; the restore waits for it.
// When the previous label cannot be read, the pane is left unchanged, and
// the restore leaves a label the operator changed during the session alone.
// Failures are logged at debug level and never fail the session.
func LabelPane(name string) func() {
	label := clean(name, maxLabelBytes)
	c := NewClient()
	if c == nil || c.block == "" || label == "" {
		return func() {}
	}
	type state struct {
		previous string
		set      bool
	}
	done := make(chan state, 1)
	go func() {
		ctx := context.Background()
		previous, err := c.BlockLabel(ctx, c.block)
		if err != nil {
			slog.Debug("rex pane label skipped", "err", err)
			done <- state{}
			return
		}
		if err := c.SetBlockLabel(ctx, c.block, label); err != nil {
			slog.Debug("rex pane label failed", "err", err)
			done <- state{}
			return
		}
		done <- state{previous: previous, set: true}
	}()
	return func() {
		s := <-done
		if !s.set {
			return
		}
		ctx := context.Background()
		if current, err := c.BlockLabel(ctx, c.block); err == nil && current != label {
			return
		}
		if err := c.SetBlockLabel(ctx, c.block, s.previous); err != nil {
			slog.Debug("rex pane label restore failed", "err", err)
		}
	}
}
