//go:build unix

package connector

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"

	"github.com/creack/pty"
	"github.com/ntwrknrd/nssh/internal/exit"
	"golang.org/x/term"
)

// errRestartRequired is a sentinel error indicating the connection should restart
// with different SSH arguments (e.g., temp known_hosts for AcceptOnce).
var errRestartRequired = errors.New("restart required")

// Run is the main entry point for the connector. It handles the full lifecycle
// including host key restart if needed.
func (c *Connector) Run(ctx context.Context) error {
	totalTimer := StartTiming(TimingTotal)
	defer totalTimer.Emit()

	if isTerminal(os.Stdin.Fd()) {
		state, err := term.MakeRaw(int(os.Stdin.Fd()))
		if err != nil {
			return fmt.Errorf("failed to set raw mode: %w", err)
		}
		c.oldState = state
	}

	defer c.restoreTerminal()
	defer c.cleanup()

	c.ensureStdinReader()

	for {
		if err := c.start(ctx); err != nil {
			return err
		}

		sessionCtx, sessionCancel := context.WithCancel(ctx)

		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			c.handleSignals(sessionCtx)
		}()

		err := c.relay(sessionCtx)
		sessionCancel()

		if errors.Is(err, errRestartRequired) && c.useTemporaryKnownHosts && c.tempKnownHosts == "" {
			slog.Debug("restarting SSH with temporary known_hosts for AcceptOnce")
			c.closeSession()
			c.resetForRetry()
			continue
		}

		c.closeSession()
		c.cleanupTempFiles()
		return err
	}
}

// start spawns the SSH process with an allocated PTY.
func (c *Connector) start(ctx context.Context) error {
	ptyTimer := StartTiming(TimingPTYStart)

	args, err := c.buildSSHArgs()
	if err != nil {
		return err
	}
	logOpenSSHCommand(args)
	c.sshCmd = exec.CommandContext(ctx, "ssh", args...)
	if len(c.env) > 0 {
		c.sshCmd.Env = append(os.Environ(), c.env...)
	}

	ptyFile, err := startPTYWithInheritedSize(c.sshCmd)
	if err != nil {
		return fmt.Errorf("pty start: %w", err)
	}
	c.ptyFile = ptyFile
	ptyTimer.Emit()

	c.propagateWindowSize()

	return nil
}

func startPTYWithInheritedSize(cmd *exec.Cmd) (*os.File, error) {
	if isTerminal(os.Stdin.Fd()) {
		if size, err := pty.GetsizeFull(os.Stdin); err == nil && size.Rows > 0 && size.Cols > 0 {
			return pty.StartWithSize(cmd, size)
		}
	}
	return pty.Start(cmd)
}

func logOpenSSHCommand(args []string) {
	argv := append([]string{"ssh"}, args...)
	slog.Debug("executing openssh", "argv", argv)
}

// waitChild waits for the SSH child process and returns an appropriate error.
func (c *Connector) waitChild() error {
	if c.sshCmd == nil {
		return nil
	}

	err := c.sshCmd.Wait()
	if err == nil {
		return nil
	}

	if exitErr, ok := err.(*exec.ExitError); ok {
		code := exitErr.ExitCode()
		switch code {
		case 255:
			return &exit.ExitError{Code: exit.ExitConnectionFailed, Message: "connection failed", Cause: err}
		case 5:
			return &exit.ExitError{Code: exit.ExitAuthFailed, Message: "authentication failed", Cause: err}
		default:
			return &exit.ExitError{Code: code, Message: fmt.Sprintf("ssh exited with code %d", code), Cause: err}
		}
	}

	return fmt.Errorf("ssh process error: %w", err)
}

// cleanup releases all resources including password.
func (c *Connector) cleanup() {
	c.closeSession()

	c.passwordMu.Lock()
	defer c.passwordMu.Unlock()
	if c.password != nil {
		c.password.Destroy()
		c.password = nil
	}
}

// closeSession releases resources for the current session (pty, etc)
// but preserves the password and stdin reader for potential retries.
func (c *Connector) closeSession() {
	c.wg.Wait()

	if c.ptyFile != nil {
		if err := c.ptyFile.Close(); err != nil {
			slog.Debug("failed to close pty", "err", err)
		}
		c.ptyFile = nil
	}
}

// cleanupTempFiles removes temporary files created during the session.
func (c *Connector) cleanupTempFiles() {
	if c.tempKnownHosts != "" {
		_ = os.Remove(c.tempKnownHosts)
		c.tempKnownHosts = ""
	}
}

// resetForRetry resets state for a connection retry.
func (c *Connector) resetForRetry() {
	c.passwordSent = false
	c.hostKeyHandled = false
	c.ringBuf.Reset()
}

// setRawMode puts the terminal into raw mode.
func (c *Connector) setRawMode() {
	if c.oldState != nil && isTerminal(os.Stdin.Fd()) {
		if _, err := term.MakeRaw(int(os.Stdin.Fd())); err != nil {
			slog.Debug("failed to set raw mode", "err", err)
		}
	}
}

// restoreTerminal restores the terminal to its original state.
func (c *Connector) restoreTerminal() {
	if c.oldState != nil {
		if err := term.Restore(int(os.Stdin.Fd()), c.oldState); err != nil {
			slog.Debug("failed to restore terminal", "err", err)
		}
	}
}
