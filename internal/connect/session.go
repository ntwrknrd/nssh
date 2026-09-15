package connect

import (
	"context"
	"github.com/ntwrknrd/nssh/internal/ssh/captured"
	"github.com/ntwrknrd/nssh/internal/ssh/session"
)

// RunTerminal keeps shared credential, proxy, and trust preparation alive for
// the foreground terminal's lifetime. started receives the locally opened PTY;
// authentication and remote prompts remain visible in the terminal output.
func RunTerminal(ctx context.Context, resolved *ResolvedHost, opts CaptureOptions, terminal session.Options, started func(*session.Session)) error {
	opts.SSHArgs = []string{"-o", "ControlMaster=no", "-o", "ControlPath=none", "-o", "ControlPersist=no", "-o", "ClearAllForwardings=yes", "-o", "SessionType=default", "-o", "RemoteCommand=none", "-o", "ForkAfterAuthentication=no", "-o", "EscapeChar=none", "-o", "PermitLocalCommand=no", "-o", "Tunnel=no"}
	opts.preparedShell = func(ctx context.Context, req captured.Request) (captured.Result, error) {
		shell, err := session.Start(ctx, req, terminal)
		if err != nil {
			return captured.Result{}, err
		}
		started(shell)
		err = shell.Wait()
		return captured.Result{}, err
	}
	_, err := RunRemoteCommandCapture(ctx, resolved, nil, opts)
	return err
}
