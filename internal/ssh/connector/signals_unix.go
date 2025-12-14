//go:build unix

package connector

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/sys/unix"
)

// handleSignals manages Unix signals for the PTY connection.
// Runs in a separate goroutine and handles:
// - SIGWINCH: Terminal resize propagation
// - SIGINT/SIGTSTP/SIGQUIT: Forward to SSH child (don't terminate nssh)
// - SIGTERM/SIGHUP: Graceful shutdown
// - SIGCONT: Refresh terminal size after resume from background
func (c *Connector) handleSignals(ctx context.Context) {
	// Ignore SIGPIPE to prevent crash when writing to closed PTY
	signal.Ignore(syscall.SIGPIPE)

	// Buffer of 16 handles rapid SIGWINCH during terminal resize without drops
	sigCh := make(chan os.Signal, 16)
	signal.Notify(sigCh,
		syscall.SIGWINCH, // Terminal resize
		syscall.SIGINT,   // Ctrl+C - forward to child
		syscall.SIGTERM,  // Graceful shutdown
		syscall.SIGHUP,   // Terminal disconnect
		syscall.SIGTSTP,  // Ctrl+Z - job control
		syscall.SIGCONT,  // Resume from background
		syscall.SIGQUIT,  // Ctrl+\ - forward to child
	)

	defer func() {
		signal.Stop(sigCh)
		// Drain any pending signals
		select {
		case <-sigCh:
		default:
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case sig := <-sigCh:
			switch sig {
			case syscall.SIGWINCH:
				c.propagateWindowSize()

			case syscall.SIGCONT:
				// After resuming from background (Ctrl+Z -> fg), the terminal
				// size may have changed. Re-sync PTY dimensions.
				c.propagateWindowSize()

			case syscall.SIGINT, syscall.SIGTSTP, syscall.SIGQUIT:
				// Forward to child WITHOUT terminating nssh
				// This allows the SSH session to handle Ctrl+C/Ctrl+Z/Ctrl+\ normally
				if c.sshCmd != nil && c.sshCmd.Process != nil {
					if err := c.sshCmd.Process.Signal(sig); err != nil {
						slog.Debug("failed to forward signal to child", "signal", sig, "err", err)
					}
				}

			case syscall.SIGTERM, syscall.SIGHUP:
				// Forward to child and exit - cleanup happens in Run()
				if c.sshCmd != nil && c.sshCmd.Process != nil {
					_ = c.sshCmd.Process.Signal(sig)
				}
				return
			}
		}
	}
}

// propagateWindowSize gets the current terminal size and sets it on the PTY.
func (c *Connector) propagateWindowSize() {
	if c.ptyFile == nil {
		return
	}

	// Get current terminal size from stdin
	ws, err := unix.IoctlGetWinsize(int(os.Stdin.Fd()), unix.TIOCGWINSZ)
	if err != nil {
		slog.Debug("failed to get window size", "err", err)
		return
	}

	// Set on PTY master
	if err := unix.IoctlSetWinsize(int(c.ptyFile.Fd()), unix.TIOCSWINSZ, ws); err != nil {
		slog.Debug("failed to set window size on pty", "err", err)
	}
}
