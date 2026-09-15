package connect

import (
	"context"
	"github.com/ntwrknrd/nssh/internal/config"

	"github.com/ntwrknrd/nssh/internal/ssh/captured"
	"github.com/ntwrknrd/nssh/internal/ssh/session"
)

// OpenSession uses the same credential and host-key preparation as captured
// commands. Its foreground shell owns the transport; no shared master is
// reused or left behind when this TUI exits.
func OpenSession(lifetime, startup context.Context, resolved *ResolvedHost, opts CaptureOptions, shellOpts session.Options) (*PersistentSession, error) {
	opts.SSHArgs = []string{"-o", "ControlMaster=no", "-o", "ControlPath=none", "-o", "ControlPersist=no", "-o", "ClearAllForwardings=yes", "-o", "SessionType=default", "-o", "RemoteCommand=none", "-o", "ForkAfterAuthentication=no", "-o", "EscapeChar=none", "-o", "PermitLocalCommand=no", "-o", "Tunnel=no"}
	var shell *session.Session
	opts.preparedShell = func(ctx context.Context, req captured.Request) (captured.Result, error) {
		var err error
		shell, err = session.Start(lifetime, ctx, req, shellOpts)
		return captured.Result{}, err
	}
	_, err := RunRemoteCommandCapture(startup, resolved, nil, opts)
	if err != nil {
		return nil, err
	}
	return &PersistentSession{Session: shell, config: resolved.Config, host: resolved.Hostname}, nil
}

// PersistentSession retains non-secret audit context around the SSH shell.
type PersistentSession struct {
	*session.Session
	config *config.Config
	host   string
}

func (s *PersistentSession) Execute(ctx context.Context, command string) session.Result {
	audit := newConnectAudit(s.config)
	if audit != nil {
		defer audit.Close()
		audit.Info("ssh_session_command_start", "host", s.host, "command", command)
	}
	result := s.Session.Execute(ctx, command)
	if audit != nil {
		audit.Info("ssh_session_command_end", "host", s.host, "status_known", result.StatusKnown, "exit_code", result.ExitCode, "error", result.Err)
	}
	return result
}
