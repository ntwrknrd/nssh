//go:build unix

package connector

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/secret"
	"github.com/ntwrknrd/nssh/internal/ssh/compat"
)

// TestResult holds the outcome of a connection test.
type TestResult struct {
	Success    bool   // Whether the connection/auth succeeded
	ExitCode   int    // SSH exit code
	Stderr     string // SSH verbose output (combined stdout/stderr from PTY)
	AuthMethod string // Detected auth method from verbose output
}

// TestConfig configures the connection test.
type TestConfig struct {
	Timeout          time.Duration  // Connection timeout (default 10s)
	Password         *secret.Secret // Password for auth testing (nil = BatchMode)
	PasswordResolver func(context.Context, string) (*secret.Secret, error)
	Port             string // SSH port (empty = use SSH config default)
	// UseSystemKnownHosts controls whether probes write to the user's real
	// known_hosts file. Default false uses a temp file to avoid persistence.
	UseSystemKnownHosts bool
	// ConfigFile specifies a custom SSH config file to use (-F flag).
	// If empty, uses the default SSH config.
	ConfigFile string
	// SSHOptions specifies nssh-rendered OpenSSH options for probes.
	SSHOptions config.SSHHostConfig
}

// buildTestSSHArgs builds SSH arguments for a probe and returns a cleanup
// function for any temporary resources (e.g., temp known_hosts).
func buildTestSSHArgs(hostname, username string, cfg TestConfig) ([]string, func(), error) {
	cleanup := func() {}

	args := []string{
		"-vv",
		"-o", fmt.Sprintf("ConnectTimeout=%d", int(cfg.Timeout.Seconds())),
		"-o", "NumberOfPasswordPrompts=1",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "ControlPath=none", // Disable multiplexing to ensure fresh connection
	}

	// Use custom config file if specified
	if cfg.ConfigFile != "" {
		args = append(args, "-F", cfg.ConfigFile)
	} else if hasSSHOptions(cfg.SSHOptions) {
		args = append(args, RenderSSHOptions(diagnosticSSHOptions(cfg.SSHOptions), 0)...)
	}

	if !cfg.UseSystemKnownHosts {
		tmpFile, err := os.CreateTemp("", "nssh-test-known-hosts-*")
		if err != nil {
			return nil, cleanup, fmt.Errorf("create temp known_hosts: %w", err)
		}
		_ = tmpFile.Close()
		args = append(args,
			"-o", "UserKnownHostsFile="+tmpFile.Name(),
			"-o", "GlobalKnownHostsFile=/dev/null",
		)
		cleanup = func() { _ = os.Remove(tmpFile.Name()) }
	}

	if cfg.Password == nil && cfg.PasswordResolver == nil {
		args = append(args, "-o", "BatchMode=yes")
	} else {
		args = append(args, "-o", "PreferredAuthentications=password,keyboard-interactive,publickey")
	}

	// Add explicit port if specified
	if cfg.Port != "" && cfg.Port != "22" {
		args = append(args, "-p", cfg.Port)
	}

	target := hostname
	if username != "" {
		target = fmt.Sprintf("%s@%s", username, hostname)
	}
	args = append(args, target, "--", "exit")

	return args, cleanup, nil
}

func hasSSHOptions(opts config.SSHHostConfig) bool {
	return len(RenderSSHOptions(opts, 0)) > 2
}

func diagnosticSSHOptions(opts config.SSHHostConfig) config.SSHHostConfig {
	out := opts
	if len(opts.Options) == 0 {
		return out
	}
	out.Options = make(config.SSHOptions, len(opts.Options))
	for key, value := range opts.Options {
		if strings.EqualFold(key, "LogLevel") {
			continue
		}
		out.Options[key] = value
	}
	return out
}

// TestConnection runs a non-interactive SSH connection test.
// It uses -vv for verbose output to capture negotiation details.
// If password is nil, uses BatchMode (compat-only testing).
// If password is provided, injects it via PTY (full auth testing).
func TestConnection(ctx context.Context, hostname, username string, cfg TestConfig) (*TestResult, error) {
	if cfg.Timeout == 0 {
		cfg.Timeout = 10 * time.Second
	}

	args, cleanup, err := buildTestSSHArgs(hostname, username, cfg)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	// Create context with timeout
	testCtx, cancel := context.WithTimeout(ctx, cfg.Timeout+5*time.Second)
	defer cancel()

	if cfg.PasswordResolver != nil {
		return testWithPasswordResolver(testCtx, args, cfg.PasswordResolver)
	}
	if cfg.Password != nil {
		return testWithPasswordResolver(testCtx, args, func(context.Context, string) (*secret.Secret, error) {
			return cfg.Password, nil
		})
	}
	return testBatchMode(testCtx, args), nil
}

// testBatchMode runs SSH in batch mode (no password injection).
func testBatchMode(ctx context.Context, args []string) *TestResult {
	cmd := exec.CommandContext(ctx, "ssh", args...)

	// Capture combined output
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	err := cmd.Run()
	stderr := output.String()

	result := &TestResult{
		Stderr:     stderr,
		AuthMethod: compat.ExtractAuthMethod(stderr),
	}

	if err == nil {
		result.Success = true
		result.ExitCode = 0
		return result
	}

	// Extract exit code
	if exitErr, ok := err.(*exec.ExitError); ok {
		result.ExitCode = exitErr.ExitCode()
	} else {
		result.ExitCode = 1
	}

	// Check for timeout
	if ctx.Err() == context.DeadlineExceeded || strings.Contains(strings.ToLower(stderr), "timed out") {
		result.ExitCode = 124
		return result
	}

	// Check if auth actually succeeded despite non-zero exit
	// (remote may reject "exit" command on some devices)
	if compat.DidAuthSucceed(stderr) {
		result.Success = true
		result.Stderr = "[nssh] Remote CLI rejected probe command 'exit'; treating connection as successful.\n" + stderr
		return result
	}

	return result
}

// testWithPasswordResolver runs SSH via PTY with prompt-aware password injection.
func testWithPasswordResolver(ctx context.Context, args []string, resolve func(context.Context, string) (*secret.Secret, error)) (*TestResult, error) {
	cmd := exec.CommandContext(ctx, "ssh", args...)

	// Start with PTY
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, fmt.Errorf("pty start: %w", err)
	}
	defer func() { _ = ptmx.Close() }()

	// Read output with password injection
	var output bytes.Buffer
	ringBuf := NewRingBuffer(DefaultRingBufferSize)
	var resolverErr error
	var resolverErrMu sync.Mutex
	setResolverErr := func(err error) {
		resolverErrMu.Lock()
		resolverErr = err
		resolverErrMu.Unlock()
	}
	done := make(chan error, 1)

	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if err != nil {
				done <- err
				return
			}
			output.Write(buf[:n])
			ringBuf.Write(buf[:n])

			// Check for password prompt and inject if needed
			if matchPasswordPrompt(ringBuf.LinearBytes()) {
				prompt := string(ringBuf.LinearBytes())
				password, err := resolve(ctx, prompt)
				if err != nil {
					setResolverErr(err)
					ringBuf.Reset()
					continue
				}
				if password == nil {
					setResolverErr(fmt.Errorf("no password configured for SSH prompt"))
					ringBuf.Reset()
					continue
				}
				if err := injectTestPassword(ptmx, password); err != nil {
					setResolverErr(err)
				}
				ringBuf.Reset()
			}
		}
	}()

	// Wait for completion or context cancellation
	var waitErr error
	select {
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		waitErr = ctx.Err()
	case err := <-done:
		if err != nil {
			// PTY read error usually means process exited
			_ = err
		}
		waitErr = cmd.Wait()
	}

	stderr := output.String()
	result := &TestResult{
		Stderr:     stderr,
		AuthMethod: compat.ExtractAuthMethod(stderr),
	}
	resolverErrMu.Lock()
	promptErr := resolverErr
	resolverErrMu.Unlock()
	if promptErr != nil {
		return result, promptErr
	}

	if waitErr == nil {
		result.Success = true
		result.ExitCode = 0
		return result, nil
	}

	// Extract exit code
	if exitErr, ok := waitErr.(*exec.ExitError); ok {
		result.ExitCode = exitErr.ExitCode()
	} else if ctx.Err() == context.DeadlineExceeded {
		result.ExitCode = 124
	} else {
		result.ExitCode = 1
	}

	// Check if auth succeeded despite non-zero exit
	if compat.DidAuthSucceed(stderr) {
		result.Success = true
		result.Stderr = "[nssh] Remote CLI rejected probe command 'exit'; treating connection as successful.\n" + stderr
		return result, nil
	}

	return result, nil
}

// injectTestPassword writes the password to the PTY.
func injectTestPassword(ptmx *os.File, password *secret.Secret) error {
	return password.Use(func(pw []byte) error {
		if _, err := ptmx.Write(pw); err != nil {
			return err
		}
		_, err := ptmx.Write([]byte{'\n'})
		return err
	})
}
