//go:build unix

package connector

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/ntwrknrd/nssh/internal/exit"
)

// bufferPool recycles read buffers to reduce GC pressure during PTY I/O.
var bufferPool = sync.Pool{
	New: func() any {
		buf := make([]byte, 4096)
		return &buf
	},
}

// stdinResult holds the result of a stdin read operation.
type stdinResult struct {
	data   []byte
	err    error
	pooled bool // true if data buffer should be returned to bufferPool
}

// ensureStdinReader spawns a single long-lived goroutine that reads from stdin.
// Safe to call multiple times.
func (c *Connector) ensureStdinReader() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.stdinStarted {
		return
	}

	ch := make(chan stdinResult, 1)
	c.stdinCh = ch
	c.stdinStarted = true

	go func() {
		defer close(ch)
		buf := make([]byte, 4096)
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil {
				ch <- stdinResult{err: err}
				return
			}
			// Get pooled buffer to avoid per-read allocations
			dataPtr := bufferPool.Get().(*[]byte)
			data := (*dataPtr)[:n]
			copy(data, buf[:n])
			ch <- stdinResult{data: data, pooled: true}
		}
	}()
}

// relay handles bidirectional I/O between the terminal and PTY.
func (c *Connector) relay(ctx context.Context) error {
	// Track session start for relative timing
	c.sessionStart = time.Now()

	firstRead := true
	var firstReadTimer *TimingEvent
	if TimingEnabled() {
		firstReadTimer = StartTiming(TimingFirstRead)
	}

	// Set up idle timer if configured
	var idleTimer *time.Timer
	var idleCh <-chan time.Time
	if c.timeouts != nil && c.timeouts.IdleTimeout.Duration() > 0 {
		idleTimer = time.NewTimer(c.timeouts.IdleTimeout.Duration())
		idleCh = idleTimer.C
		defer idleTimer.Stop()
	}

	// Stdin relay is started AFTER host key handling to avoid competing
	// with UI prompts for stdin input.
	stdinRelayStarted := false
	startStdinRelay := func() {
		if c.stdinDisabled {
			return
		}
		if stdinRelayStarted {
			return
		}
		stdinRelayStarted = true
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			for {
				// Check context first (priority exit)
				select {
				case <-ctx.Done():
					return
				default:
				}
				// Then wait for stdin or context
				select {
				case <-ctx.Done():
					return
				case result, ok := <-c.stdinCh:
					if !ok || result.err != nil {
						return
					}
					if c.ptyFile != nil {
						if _, err := c.ptyFile.Write(result.data); err != nil {
							slog.Debug("failed to write to pty", "err", err)
						}
						// Reset idle timer on user input
						if idleTimer != nil {
							if !idleTimer.Stop() {
								select {
								case <-idleTimer.C:
								default:
								}
							}
							idleTimer.Reset(c.timeouts.IdleTimeout.Duration())
						}
					}
					// Return buffer to pool after use
					if result.pooled {
						buf := result.data[:cap(result.data)]
						bufferPool.Put(&buf)
					}
				}
			}
		}()
	}

	// Fallback timer to start stdin relay if no PTY output is received.
	// This handles commands that wait for stdin without emitting any output first.
	const stdinRelayFallbackDelay = 100 * time.Millisecond
	var stdinFallbackTimer *time.Timer
	var stdinFallbackCh <-chan time.Time
	if !c.stdinDisabled {
		stdinFallbackTimer = time.NewTimer(stdinRelayFallbackDelay)
		stdinFallbackCh = stdinFallbackTimer.C
		defer stdinFallbackTimer.Stop()
	}

	// Start PTY reader goroutine for non-blocking reads with idle timeout
	type ptyResult struct {
		data   []byte
		err    error
		pooled bool // true if data buffer should be returned to bufferPool
	}
	ptyCh := make(chan ptyResult, 1)
	go func() {
		defer close(ptyCh)
		buf := make([]byte, 4096)
		for {
			n, err := c.ptyFile.Read(buf)
			if err != nil {
				ptyCh <- ptyResult{err: err}
				return
			}
			// Get pooled buffer to avoid per-read allocations
			dataPtr := bufferPool.Get().(*[]byte)
			data := (*dataPtr)[:n]
			copy(data, buf[:n])
			ptyCh <- ptyResult{data: data, pooled: true}
		}
	}()

	// Main loop: PTY master -> stdout (with inspection)
	for {
		// Wait for PTY data, idle timeout, fallback stdin timer, or context cancellation
		var buf []byte
		var currentResult ptyResult
		select {
		case <-ctx.Done():
			EmitWithValue(TimingSessionEnd, time.Since(c.sessionStart))
			return ctx.Err()
		case <-idleCh:
			slog.Info("session idle timeout", "duration", c.timeouts.IdleTimeout.Duration())
			return fmt.Errorf("session idle timeout (%v)", c.timeouts.IdleTimeout.Duration())
		case <-stdinFallbackCh:
			// No PTY output received within the fallback window.
			// Start stdin relay to handle commands that wait for input first.
			slog.Debug("stdin relay fallback timer fired, starting relay")
			c.hostKeyHandled = true
			startStdinRelay()
			continue
		case result, ok := <-ptyCh:
			if !ok || result.err != nil {
				// Emit session duration
				EmitWithValue(TimingSessionEnd, time.Since(c.sessionStart))
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return c.waitChild()
			}
			currentResult = result
			buf = result.data
			// Stop fallback timer once we receive any PTY output
			if stdinFallbackTimer != nil {
				if !stdinFallbackTimer.Stop() {
					select {
					case <-stdinFallbackTimer.C:
					default:
					}
				}
			}
			// Reset idle timer on PTY activity
			if idleTimer != nil {
				if !idleTimer.Stop() {
					select {
					case <-idleTimer.C:
					default:
					}
				}
				idleTimer.Reset(c.timeouts.IdleTimeout.Duration())
			}
		}

		// Emit first read timing
		if firstRead && firstReadTimer != nil {
			firstReadTimer.Emit()
			firstRead = false
		}

		c.mu.Lock()
		c.ringBuf.Write(buf)
		linearBuf := c.ringBuf.LinearBytes()

		// Check for host key prompts first (they take priority)
		if !c.hostKeyHandled {
			if handled, hkResult := c.handleHostKeyPrompt(linearBuf); handled {
				c.hostKeyHandled = true
				c.hostKeyPromptHit = true
				c.mu.Unlock()
				// Return buffer to pool before handling result
				if currentResult.pooled {
					poolBuf := currentResult.data[:cap(currentResult.data)]
					bufferPool.Put(&poolBuf)
				}
				switch hkResult {
				case HostKeyResultAbort:
					if c.captureMode {
						c.closeSession()
						if c.sshCmd != nil && c.sshCmd.Process != nil {
							_ = c.sshCmd.Process.Kill()
						}
						_ = c.waitChild()
						return errHostKeyPromptCapture
					}
					return exit.ErrAuthFailed
				case HostKeyResultRestart:
					return errRestartRequired
				}
				// Host key accepted - now safe to start stdin relay
				startStdinRelay()
				continue
			}
		}

		// Check for password prompts (only if we haven't already sent password)
		suppressPrompt := false
		if !c.passwordSent && matchPasswordPrompt(linearBuf) {
			// Password prompt means we're past host key phase
			c.hostKeyHandled = true
			EmitWithValue(TimingPasswordPrompt, time.Since(c.sessionStart))

			if c.hasPasswordSource() {
				// We have a password - inject it once
				passwordTimer := StartTiming(TimingPasswordSent)
				if err := c.injectPassword(ctx); err != nil {
					slog.Debug("password injection failed", "err", err)
				} else {
					passwordTimer.Emit()
					c.passwordSent = true
					suppressPrompt = true
				}
			}
			// If no password, let prompt through so user can type manually
		}

		// Start stdin relay once we're past host key handling phase.
		// For known hosts with key auth, we never see a prompt, so start
		// relay after first output that isn't a host key prompt.
		if c.hostKeyHandled || (!matchUnknownHost(linearBuf) && !matchHostKeyIntro(linearBuf) && !matchHostKeyChanged(linearBuf)) {
			c.hostKeyHandled = true
			startStdinRelay()
		}
		c.mu.Unlock()

		// Filter and write output
		output := c.filterOutput(buf, suppressPrompt)
		if len(output) > 0 {
			if _, err := c.output.Write(output); err != nil {
				slog.Debug("failed to write to stdout", "err", err)
			}
		}

		// Return buffer to pool after all processing is complete
		if currentResult.pooled {
			poolBuf := currentResult.data[:cap(currentResult.data)]
			bufferPool.Put(&poolBuf)
		}
	}
}

// GetStdinReader returns an io.Reader that reads from the connector's stdin channel.
// This allows diverting stdin to other consumers (like UI prompts) while ensuring
// no data is lost if ensureStdinReader has already consumed it.
func (c *Connector) GetStdinReader() io.Reader {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stdinReaderLocked()
}

func (c *Connector) stdinReaderLocked() io.Reader {
	if c.stdinCh == nil {
		return nil
	}
	return &channelReader{ch: c.stdinCh}
}
