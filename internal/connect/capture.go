package connect

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"time"

	"github.com/ntwrknrd/nssh/internal/exit"
	"github.com/ntwrknrd/nssh/internal/ssh/captured"
	"github.com/ntwrknrd/nssh/internal/ssh/connector"
)

// CaptureOptions keeps REPL I/O and trust decisions request scoped. The terminal
// owner supplies host-key decisions. A nil prompt rejects trust decisions
// instead of reading ambient stdin.
type CaptureOptions struct {
	Options
	MaxOutputBytes int
	HostKeyPrompt  connector.HostKeyPromptFunc
}

// RunRemoteCommandCapture shares normal SSH preparation without printing results.
// The resolved host and its credentials belong to this call and must not be reused.
func RunRemoteCommandCapture(ctx context.Context, resolved *ResolvedHost, command []string, opts CaptureOptions) (captured.Result, error) {
	if resolved == nil {
		return captured.Result{}, fmt.Errorf("resolved host is required")
	}
	if opts.MaxOutputBytes <= 0 {
		opts.MaxOutputBytes = 8 << 20
	}
	opts.capture = &opts
	audit := newConnectAudit(resolved.Config)
	if audit != nil {
		defer func() { _ = audit.Close() }()
		audit.Info("ssh_remote_command_start", "host", resolved.Hostname, "command", command)
	}
	result, err := captureResolvedRemoteCommand(ctx, resolved, nil, command, resolved.Config, opts.Options)
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil && result.ExitCode == 0 {
		result.ExitCode = 1
		var exitErr *exit.ExitError
		if errors.As(err, &exitErr) {
			result.ExitCode = exitErr.Code
		}
	}
	result.Err = err
	if audit != nil {
		audit.Info("ssh_remote_command_end", "host", resolved.Hostname, "exit_code", result.ExitCode, "error", err)
	}
	return result, err
}

// Setup is short-lived and serialized to avoid competing persistent master
// creation. Existing masters are never closed by an individual REPL command.
var commandMuxGate = make(chan struct{}, 1)

func startCommandMux(ctx context.Context, req connector.MuxStartRequest, scoped bool) error {
	if !scoped {
		return startMuxSessionFunc(ctx, req)
	}
	select {
	case commandMuxGate <- struct{}{}:
		defer func() { <-commandMuxGate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if hot, _ := checkMuxSessionFunc(ctx, connector.MuxCheckRequest{Hostname: req.Hostname, Username: req.Username, Port: req.Port, SSHOptions: req.SSHOptions, SSHVerbosity: req.SSHVerbosity, SSHArgs: req.SSHArgs, Timeout: req.Timeout}); hot {
		return nil
	}
	return startMuxSessionFunc(ctx, req)
}

type captureDiagnostics struct {
	mu        sync.Mutex
	limit     int
	data      []byte
	truncated bool
}

func (b *captureDiagnostics) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	keep := min(n, max(0, b.limit-len(b.data)))
	b.data = append(b.data, p[:keep]...)
	b.truncated = b.truncated || keep < n
	return n, nil
}
func (b *captureDiagnostics) result() captured.Result {
	return captured.Result{Stderr: b.data, Truncated: b.truncated}
}

// collectPreparationOutput also bounds diagnostics produced before the remote
// command starts. Regular root commands retain their existing behavior.
func collectPreparationOutput(cmd *exec.Cmd, opts Options) ([]byte, error) {
	if opts.capture == nil {
		return cmd.CombinedOutput()
	}
	output := &captureDiagnostics{limit: 64 << 10}
	cmd.Stdout, cmd.Stderr = output, output
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	if output.truncated {
		return output.data, fmt.Errorf("SSH preparation diagnostics exceeded 64 KiB")
	}
	return output.data, err
}
