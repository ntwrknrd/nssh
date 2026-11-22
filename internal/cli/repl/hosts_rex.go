package repl

import (
	"fmt"
	"io"

	core "github.com/ntwrknrd/nssh/internal/repl"
	"github.com/ntwrknrd/nssh/internal/rex"
)

// rexHostStatus reports root host-list progress to Rex: a root record with
// overall progress and one child record per host under hosts/. The executor
// serializes callbacks. A nil rexHostStatus is a no-op.
type rexHostStatus struct {
	w        *rex.Writer
	label    string
	total    int
	finished int
	failed   int
}

func newRexHostStatus(out io.Writer, terminal bool, label string) *rexHostStatus {
	if !terminal {
		return nil
	}
	w := rex.NewWriter(out)
	if w == nil {
		return nil
	}
	return &rexHostStatus{w: w, label: displayLabel(label)}
}

func (s *rexHostStatus) start(targets []core.ResolvedTarget) {
	if s == nil {
		return
	}
	s.total = len(targets)
	// Drop host records left by an earlier run in this terminal.
	s.w.Report(rex.Report{State: rex.StateClear, ID: "hosts"})
	s.root()
}

func (s *rexHostStatus) event(event core.Event) {
	if s == nil {
		return
	}
	name := hostListName(event.Target)
	child := rex.Report{ID: "hosts/" + rex.Segment(name), Title: name}
	switch event.State {
	case core.Running:
		child.State = rex.StateWorking
	case core.Completed:
		child.State = rex.StateDone
		s.finished++
	case core.Failed, core.Canceled, core.Skipped:
		child.State = rex.StateError
		child.Msg = string(event.State)
		s.finished++
		s.failed++
	default:
		return
	}
	s.w.Report(child)
	if child.State != rex.StateWorking {
		s.root()
	}
}

func (s *rexHostStatus) root() {
	r := rex.Report{State: rex.StateWorking, Title: s.label}
	if s.total > 0 {
		r.Progress = s.finished * 100 / s.total
		r.Msg = fmt.Sprintf("%d of %d hosts", s.finished, s.total)
	}
	s.w.Report(r)
}

// finish leaves a root record that survives the next shell prompt.
func (s *rexHostStatus) finish() {
	if s == nil || s.total == 0 {
		return
	}
	r := rex.Report{State: rex.StateDone, Title: s.label, Msg: fmt.Sprintf("%d hosts succeeded", s.total)}
	if s.failed > 0 || s.finished < s.total {
		r.State = rex.StateError
		r.Msg = fmt.Sprintf("%d of %d hosts failed", s.total-(s.finished-s.failed), s.total)
	}
	s.w.Report(r)
}
