//go:build unix

package connector

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/exit"
	"github.com/ntwrknrd/nssh/internal/secret"
	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

// bufferPool recycles read buffers to reduce GC pressure during PTY I/O.
var bufferPool = sync.Pool{
	New: func() any {
		buf := make([]byte, 4096)
		return &buf
	},
}

// Default configuration values.
const (
	DefaultRingBufferSize       = 2048
	DefaultPasswordFilterWindow = 100 * time.Millisecond
)

// errRestartRequired is a sentinel error indicating the connection should restart
// with different SSH arguments (e.g., temp known_hosts for AcceptOnce).
var errRestartRequired = errors.New("restart required")

// Connector manages a PTY-based SSH connection with credential injection.
type Connector struct {
	hostname       string
	username       string
	password       *secret.Secret
	sshArgs        []string
	acceptOnceMode string

	ptyFile *os.File  // PTY master from creack/pty.Start()
	sshCmd  *exec.Cmd // SSH child process

	ringBuf        *RingBuffer
	passwordSent   bool
	passwordSentAt time.Time
	hostKeyHandled bool
	pinnedHostKey  *pinnedKey // Captured key type + fingerprint from AcceptOnce flow

	// Stdin handling
	stdinCh      <-chan stdinResult
	stdinStarted bool

	mu sync.RWMutex
	wg sync.WaitGroup

	timeouts *config.SSHConnectionConfig

	useTemporaryKnownHosts bool   // Set by AcceptOnce, triggers restart with temp file
	tempKnownHosts         string // Path to temp known_hosts, cleaned up on exit

	// Terminal state for restoration
	oldState *term.State

	// Timing instrumentation
	sessionStart time.Time // When relay() started (for relative timing)

	// Resolved endpoint from SSH config (HostName/Port). Used for keyscan in AcceptOnce
	// host-key pinning. NOT used as SSH target - we use hostname (alias) for that so
	// SSH config Host pattern matching works correctly.
	resolvedHost string
	resolvedPort string
}

// pinnedKey stores the host key type and fingerprint observed during the
// initial prompt. Used to pin the AcceptOnce retry to the exact key.
type pinnedKey struct {
	typeName    string // e.g., "ED25519"
	fingerprint string // e.g., "SHA256:abcd..."
}

// NewConnector creates a new PTY connector for SSH connections.
func NewConnector(host, user string, pass *secret.Secret, sshArgs []string) *Connector {
	return &Connector{
		hostname:       host,
		username:       user,
		password:       pass,
		sshArgs:        sshArgs,
		ringBuf:        NewRingBuffer(DefaultRingBufferSize),
		acceptOnceMode: "pin",
	}
}

// SetResolvedEndpoint sets the concrete hostname and port derived from SSH config.
// Used for host-key pinning (keyscan) when the user connects via an alias.
// NOT used as the SSH command target - we use the alias for proper config matching.
func (c *Connector) SetResolvedEndpoint(host, port string) {
	c.resolvedHost = strings.TrimSpace(host)
	c.resolvedPort = strings.TrimSpace(port)
}

// SetAcceptOnceMode configures how AcceptOnce handles host keys: "pin" (default)
// pre-seeds a temp known_hosts with the observed key; "accept-new" uses TOFU.
func (c *Connector) SetAcceptOnceMode(mode string) {
	switch strings.ToLower(mode) {
	case "accept-new":
		c.acceptOnceMode = "accept-new"
	default:
		c.acceptOnceMode = "pin"
	}
}

// SetTimeouts configures connection timeouts from config.
func (c *Connector) SetTimeouts(cfg *config.SSHConnectionConfig) {
	c.timeouts = cfg
}

// Run is the main entry point for the connector. It handles the full lifecycle
// including host key restart if needed.
func (c *Connector) Run(ctx context.Context) error {
	// Start total timing
	totalTimer := StartTiming(TimingTotal)
	defer totalTimer.Emit()

	// Save terminal state for restoration
	if isTerminal(os.Stdin.Fd()) {
		state, err := term.MakeRaw(int(os.Stdin.Fd()))
		if err != nil {
			return fmt.Errorf("failed to set raw mode: %w", err)
		}
		c.oldState = state
	}

	// Ensure terminal restoration on any exit path
	defer c.restoreTerminal()

	// Ensure password is destroyed on exit
	defer c.cleanup()

	// Start shared stdin reader
	c.ensureStdinReader()

	for {
		if err := c.start(ctx); err != nil {
			return err
		}

		// Create cancellable context for this session's goroutines
		sessionCtx, sessionCancel := context.WithCancel(ctx)

		// Start signal handler
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			c.handleSignals(sessionCtx)
		}()

		err := c.relay(sessionCtx)

		// Cancel all session goroutines (stdin reader, signal handler)
		sessionCancel()

		// Check if we need to restart for AcceptOnce
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
	c.sshCmd = exec.CommandContext(ctx, "ssh", args...)

	ptyFile, err := pty.Start(c.sshCmd)
	if err != nil {
		return fmt.Errorf("pty start: %w", err)
	}
	c.ptyFile = ptyFile
	ptyTimer.Emit()

	// Sync initial terminal size
	c.propagateWindowSize()

	return nil
}

// buildSSHArgs constructs SSH arguments, adding temp known_hosts if needed.
// SSH syntax: ssh [options] hostname [command]
// The -- separator marks the start of the remote command, which must come AFTER hostname.
func (c *Connector) buildSSHArgs() ([]string, error) {
	args := []string{"-tt"} // Force PTY allocation

	// Build target (user@host or just host)
	// Use the Host identifier/alias so SSH config Host pattern matching works correctly.
	// This ensures host-specific settings (KexAlgorithms, Ciphers, etc.) are applied.
	// Users who want consistent ControlMaster sockets can enable CanonicalizeHostname.
	target := c.hostname
	if c.username != "" {
		target = fmt.Sprintf("%s@%s", c.username, target)
	}

	// Split sshArgs into options (before --) and command (-- and after)
	// SSH requires: ssh [options] hostname [-- command]
	var options, command []string
	separatorIdx := -1
	for i, arg := range c.sshArgs {
		if arg == "--" {
			separatorIdx = i
			break
		}
	}

	if separatorIdx >= 0 {
		options = c.sshArgs[:separatorIdx]
		command = c.sshArgs[separatorIdx:] // Includes --
	} else {
		options = c.sshArgs
	}

	// Add connection timeout if configured
	if c.timeouts != nil && c.timeouts.Timeout.Duration() > 0 {
		args = append(args,
			"-o", fmt.Sprintf("ConnectTimeout=%d", int(c.timeouts.Timeout.Duration().Seconds())),
		)
	}

	// Port is handled by SSH config Host matching or user's -p flag in sshArgs.
	// No need to add it explicitly when using the Host identifier as target.

	// Add SSH options
	args = append(args, options...)

	// Add temp known_hosts options if needed
	if c.useTemporaryKnownHosts && c.tempKnownHosts == "" {
		// Create temp file that will be discarded after session
		f, err := os.CreateTemp("", "nssh-known-hosts-*")
		if err != nil {
			slog.Warn("failed to create temp known_hosts, using real file", "err", err)
		} else {
			c.tempKnownHosts = f.Name()
			if err := c.populateTempKnownHosts(); err != nil {
				return nil, err
			}
			if err := f.Close(); err != nil {
				slog.Debug("failed to close temp known_hosts file", "err", err)
			}
			args = append(args,
				"-o", "UserKnownHostsFile="+c.tempKnownHosts,
				"-o", "StrictHostKeyChecking=yes",
			)
		}
	}

	// Add target hostname
	args = append(args, target)

	// Add remote command (if any)
	if len(command) > 0 {
		args = append(args, command...)
	}

	return args, nil
}

// populateTempKnownHosts writes exactly one pinned host key (captured during
// AcceptOnce) into the temporary known_hosts file. This prevents a key-swap
// between the initial prompt and the restarted connection.
func (c *Connector) populateTempKnownHosts() error {
	if c.tempKnownHosts == "" {
		return fmt.Errorf("temp known_hosts path not set")
	}
	if c.pinnedHostKey == nil || c.pinnedHostKey.fingerprint == "" {
		return fmt.Errorf("cannot use Accept once: no pinned host key available")
	}

	keyType := strings.ToLower(c.pinnedHostKey.typeName)
	if keyType == "" {
		return fmt.Errorf("cannot use Accept once: missing key type")
	}

	// Determine real host/port for keyscan
	hostToScan := c.resolvedHost
	if hostToScan == "" {
		hostToScan = c.hostname
	}

	port := c.resolvedPort
	if cliPort := c.parsePortFromSSHArgs(); cliPort != "" {
		port = cliPort
	}
	if port == "" {
		port = "22"
	}

	args := []string{"-t", keyType}
	if port != "22" {
		args = append(args, "-p", port)
	}
	args = append(args, hostToScan)

	output, err := exec.Command("ssh-keyscan", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ssh-keyscan failed: %w (%s)", err, strings.TrimSpace(string(output)))
	}

	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		pubKey, _, _, _, parseErr := ssh.ParseAuthorizedKey([]byte(line))
		if parseErr != nil {
			continue
		}
		fp := ssh.FingerprintSHA256(pubKey)
		if fp != c.pinnedHostKey.fingerprint {
			continue
		}
		if err := os.WriteFile(c.tempKnownHosts, []byte(line+"\n"), 0600); err != nil {
			return fmt.Errorf("write temp known_hosts: %w", err)
		}
		return nil
	}

	return fmt.Errorf("failed to pin host key: fingerprint mismatch after ssh-keyscan")
}

// parsePortFromSSHArgs extracts an explicit port from sshArgs (-p or -o Port=...)
// before the -- separator. Returns empty string if none found.
func (c *Connector) parsePortFromSSHArgs() string {
	for i := 0; i < len(c.sshArgs); i++ {
		arg := c.sshArgs[i]
		if arg == "--" {
			break
		}
		if arg == "-p" && i+1 < len(c.sshArgs) {
			return c.sshArgs[i+1]
		}
		if strings.HasPrefix(arg, "-p") && len(arg) > 2 {
			return arg[2:]
		}
		if arg == "-o" && i+1 < len(c.sshArgs) {
			next := strings.ToLower(c.sshArgs[i+1])
			if strings.HasPrefix(next, "port=") {
				return c.sshArgs[i+1][5:]
			}
			i++
		}
	}
	return ""
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
	stdinFallbackTimer := time.NewTimer(stdinRelayFallbackDelay)
	defer stdinFallbackTimer.Stop()

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
		case <-stdinFallbackTimer.C:
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
			if !stdinFallbackTimer.Stop() {
				select {
				case <-stdinFallbackTimer.C:
				default:
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
				c.mu.Unlock()
				// Return buffer to pool before handling result
				if currentResult.pooled {
					poolBuf := currentResult.data[:cap(currentResult.data)]
					bufferPool.Put(&poolBuf)
				}
				switch hkResult {
				case HostKeyResultAbort:
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

			if c.password != nil {
				// We have a password - inject it once
				passwordTimer := StartTiming(TimingPasswordSent)
				if err := c.injectPassword(); err != nil {
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
		if c.hostKeyHandled || (!matchUnknownHost(linearBuf) && !matchHostKeyChanged(linearBuf)) {
			c.hostKeyHandled = true
			startStdinRelay()
		}
		c.mu.Unlock()

		// Filter and write output
		output := c.filterOutput(buf, suppressPrompt)
		if len(output) > 0 {
			if _, err := os.Stdout.Write(output); err != nil {
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

// injectPassword writes the password to the PTY.
func (c *Connector) injectPassword() error {
	if c.password == nil {
		return fmt.Errorf("no password configured")
	}

	err := c.password.Use(func(pw []byte) error {
		// Write password + newline to PTY master
		if _, err := c.ptyFile.Write(pw); err != nil {
			return err
		}
		_, err := c.ptyFile.Write([]byte{'\n'})
		return err
	})

	if err == nil {
		c.passwordSentAt = time.Now()
	}
	return err
}

// filterOutput scrubs sensitive data from PTY output before display/recording.
// If suppressPrompt is true, removes password prompt lines from output.
func (c *Connector) filterOutput(data []byte, suppressPrompt bool) []byte {
	result := data

	// Suppress password prompt line if requested
	if suppressPrompt {
		result = removePasswordPromptLines(result)
	}

	// Only filter password echo if we recently sent a password
	if c.recentPasswordSent() && c.password != nil {
		// Strip leading newlines (echo from password submission)
		result = bytes.TrimLeft(result, "\r\n")

		// Check if password appears in output (echo from misconfigured server)
		if err := c.password.Use(func(pw []byte) error {
			if bytes.Contains(result, pw) {
				result = bytes.ReplaceAll(result, pw, []byte("********"))
			}
			return nil
		}); err != nil {
			slog.Debug("failed to access password for filtering", "err", err)
		}
	}

	return result
}

// removePasswordPromptLines filters out lines containing password prompts.
func removePasswordPromptLines(data []byte) []byte {
	lines := bytes.Split(data, []byte("\n"))
	var filtered [][]byte

	for _, line := range lines {
		if !matchPasswordPrompt(line) {
			filtered = append(filtered, line)
		}
	}

	// If we filtered everything, return empty
	if len(filtered) == 0 {
		return nil
	}

	return bytes.Join(filtered, []byte("\n"))
}

// recentPasswordSent returns true if password was sent in the last filter window.
func (c *Connector) recentPasswordSent() bool {
	if !c.passwordSent {
		return false
	}
	return time.Since(c.passwordSentAt) < DefaultPasswordFilterWindow
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

	// Extract exit code from ProcessState
	if exitErr, ok := err.(*exec.ExitError); ok {
		code := exitErr.ExitCode()
		switch code {
		case 255:
			// SSH returns 255 for connection failures
			return &exit.ExitError{Code: exit.ExitConnectionFailed, Message: "connection failed", Cause: err}
		case 5:
			// Common for auth failures on some systems
			return &exit.ExitError{Code: exit.ExitAuthFailed, Message: "authentication failed", Cause: err}
		default:
			// Preserve SSH's exit code
			return &exit.ExitError{Code: code, Message: fmt.Sprintf("ssh exited with code %d", code), Cause: err}
		}
	}

	return fmt.Errorf("ssh process error: %w", err)
}

// cleanup releases all resources including password.
func (c *Connector) cleanup() {
	c.closeSession()

	if c.password != nil {
		c.password.Destroy()
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

// GetStdinReader returns an io.Reader that reads from the connector's stdin channel.
// This allows diverting stdin to other consumers (like UI prompts) while ensuring
// no data is lost if ensureStdinReader has already consumed it.
func (c *Connector) GetStdinReader() io.Reader {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stdinCh == nil {
		return nil
	}
	return &channelReader{ch: c.stdinCh}
}
