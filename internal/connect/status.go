package connect

import (
	"errors"
	"fmt"

	"github.com/ntwrknrd/nssh/internal/exit"
	"github.com/ntwrknrd/nssh/internal/rex"
	"github.com/ntwrknrd/nssh/internal/ssh/connector"
)

// sessionStatus reports interactive session state to Rex as OSC 7501 program
// status on the root record. A nil sessionStatus is a no-op.
type sessionStatus struct {
	w    *rex.Writer
	name string
}

// newSessionStatus returns a reporter for an interactive session inside Rex,
// with the same mode exclusions as the terminal title, or nil otherwise.
func newSessionStatus(resolved *ResolvedHost, sshArgs []string) *sessionStatus {
	name := terminalTitle(resolved)
	if name == "" || !interactiveSession(sshArgs) || !titleTerminal() {
		return nil
	}
	w := rex.NewWriter(titleOutput)
	if w == nil {
		return nil
	}
	return &sessionStatus{w: w, name: name}
}

func (s *sessionStatus) report(state, kind, msg string) {
	if s == nil {
		return
	}
	s.w.Report(rex.Report{State: state, Kind: kind, Title: s.name, Msg: msg})
}

// connecting marks the session as working until input is relayed.
func (s *sessionStatus) connecting() {
	if s != nil {
		s.report(rex.StateWorking, "", "Connecting to "+s.name)
	}
}

// established marks the session idle once keyboard input reaches it.
func (s *sessionStatus) established() { s.report(rex.StateIdle, "", "") }

// hostKeyPrompt reports blocked while the operator answers a host-key prompt.
func (s *sessionStatus) hostKeyPrompt(next connector.HostKeyPromptFunc) connector.HostKeyPromptFunc {
	if s == nil || next == nil {
		return next
	}
	return func(prompt connector.HostKeyPrompt) connector.HostKeyAction {
		s.report(rex.StateBlocked, rex.KindQuestion, "Verify host key for "+s.name)
		action := next(prompt)
		s.connecting()
		return action
	}
}

// finished clears the record after a clean exit and leaves an error record,
// which survives the next shell prompt, after a failure.
func (s *sessionStatus) finished(err error) {
	if s == nil {
		return
	}
	if err == nil {
		s.w.Report(rex.Report{State: rex.StateClear})
		return
	}
	code := 1
	var exitErr *exit.ExitError
	if errors.As(err, &exitErr) {
		code = exitErr.Code
	}
	s.report(rex.StateError, "", fmt.Sprintf("%s: exit status %d", s.name, code))
}
