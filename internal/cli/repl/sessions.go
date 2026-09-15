package repl

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/ntwrknrd/nssh/internal/connect"
	core "github.com/ntwrknrd/nssh/internal/repl"
	"github.com/ntwrknrd/nssh/internal/ssh/connector"
	"github.com/ntwrknrd/nssh/internal/ssh/session"
)

type sessionWaitingMsg struct{ host, text string }
type sessionReplyMsg struct{ err error }

func (o *terminalOwner) runSession(ctx context.Context, target core.ResolvedTarget, request targetRequest, command []string) core.Result {
	o.sessionMu.Lock()
	shell := o.sessions[target.Identity]
	if shell == nil && len(o.sessions)+o.opening >= 128 {
		o.sessionMu.Unlock()
		return core.Result{Err: errors.New("session limit reached; use :disconnect to close retained sessions"), ExitCode: 1}
	}
	if shell == nil {
		o.opening++
	}
	profile, width, height := o.profile, o.width, o.height
	o.sessionMu.Unlock()
	if shell == nil {
		reserved := true
		defer func() {
			if reserved {
				o.sessionMu.Lock()
				o.opening--
				o.sessionMu.Unlock()
			}
		}()
		resolved, err := connect.ResolveLiteralHostFromCatalog(ctx, request.host, request.user, request.cfg, request.cat)
		if err != nil {
			return core.Result{Err: err, ExitCode: 1}
		}
		identity := target.Identity
		opts := connect.CaptureOptions{MaxOutputBytes: core.MaxCommandOutput, HostKeyPrompt: func(p connector.HostKeyPrompt) connector.HostKeyAction { return o.prompt(ctx, p) }}
		shell, err = connect.OpenSession(o.ctx, ctx, resolved, opts, session.Options{Profile: profile, Width: max(80, width), Height: max(24, height), Limit: core.MaxCommandOutput, Waiting: func(text string) { o.program.Send(sessionWaitingMsg{identity, text}) }})
		if err != nil {
			return core.Result{Err: err, ExitCode: 1}
		}
		o.sessionMu.Lock()
		if o.sessions == nil {
			o.sessions = map[string]*connect.PersistentSession{}
		}
		o.sessions[target.Identity] = shell
		o.opening--
		reserved = false
		o.sessionMu.Unlock()
	}
	o.sessionMu.Lock()
	if o.busy == nil {
		o.busy = map[string]*connect.PersistentSession{}
	}
	o.busy[target.Identity] = shell
	o.sessionMu.Unlock()
	defer func() { o.sessionMu.Lock(); delete(o.busy, target.Identity); o.sessionMu.Unlock() }()
	result := shell.Execute(ctx, strings.Join(command, " "))
	err := result.Err
	if err == nil && result.StatusKnown && result.ExitCode != 0 {
		err = fmt.Errorf("remote shell exit status %d", result.ExitCode)
	}
	return core.Result{Stdout: result.Output, ExitCode: result.ExitCode, Err: err, Truncated: result.Truncated, Interactive: true, StatusKnown: result.StatusKnown, Prompt: result.Prompt}
}
func (o *terminalOwner) reply(text string) error {
	o.sessionMu.Lock()
	if !o.configMode || len(o.busy) != 1 {
		o.sessionMu.Unlock()
		return errors.New("replies require configuration mode with exactly one running device")
	}
	var shell *connect.PersistentSession
	for _, s := range o.busy {
		shell = s
	}
	o.sessionMu.Unlock()
	return shell.Reply(text)
}
func (o *terminalOwner) closeSessions() {
	o.sessionMu.Lock()
	sessions := o.sessions
	o.sessions = map[string]*connect.PersistentSession{}
	o.sessionMu.Unlock()
	for _, s := range sessions {
		s.Close()
	}
}
func (o *terminalOwner) sessionSummary() string {
	o.sessionMu.Lock()
	defer o.sessionMu.Unlock()
	var lines []string
	for name, s := range o.sessions {
		state := "open"
		if !s.Alive() {
			state = "closed"
		}
		lines = append(lines, fmt.Sprintf("[%s] %s: %s", name, state, s.Prompt()))
	}
	sort.Strings(lines)
	if len(lines) == 0 {
		return "No open device sessions"
	}
	return strings.Join(lines, "\n")
}
func (o *terminalOwner) resizeSessions(width, height int) {
	o.sessionMu.Lock()
	defer o.sessionMu.Unlock()
	o.width, o.height = width, height
	for _, s := range o.sessions {
		s.Resize(width, height)
	}
}
