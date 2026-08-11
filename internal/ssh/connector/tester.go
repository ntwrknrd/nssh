//go:build unix

package connector

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/ssh/compat"
)

// TestResult holds the outcome of a connection test.
type TestResult struct {
	Success    bool   // Whether the connection/auth succeeded
	ExitCode   int    // SSH exit code
	Stderr     string // SSH verbose output (combined stdout/stderr)
	AuthMethod string // Detected auth method from verbose output
}

// TestConfig configures the connection test.
type TestConfig struct {
	Timeout time.Duration // Connection timeout (default 10s)
	Port    string        // SSH port (empty = use SSH config default)
	Env     []string      // Additional environment for isolated askpass channels
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
func buildTestSSHArgs(hostname, username string, cfg TestConfig) ([]string, string, func(), error) {
	clientLog, err := os.CreateTemp("", "nssh-test-client-log-*")
	if err != nil {
		return nil, "", func() {}, fmt.Errorf("create SSH client log: %w", err)
	}
	clientLogPath := clientLog.Name()
	if err := clientLog.Close(); err != nil {
		_ = os.Remove(clientLogPath)
		return nil, "", func() {}, fmt.Errorf("close SSH client log: %w", err)
	}
	cleanup := func() { _ = os.Remove(clientLogPath) }

	args := []string{
		"-vv",
		"-E", clientLogPath,
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
			cleanup()
			return nil, "", func() {}, fmt.Errorf("create temp known_hosts: %w", err)
		}
		_ = tmpFile.Close()
		args = append(args,
			"-o", "UserKnownHostsFile="+tmpFile.Name(),
			"-o", "GlobalKnownHostsFile=/dev/null",
		)
		cleanupClientLog := cleanup
		cleanup = func() {
			_ = os.Remove(tmpFile.Name())
			cleanupClientLog()
		}
	}

	if !hasTestEnv(cfg.Env, "SSH_ASKPASS") {
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

	return args, clientLogPath, cleanup, nil
}

func hasTestEnv(env []string, name string) bool {
	prefix := name + "="
	for _, entry := range env {
		if entry == name || strings.HasPrefix(entry, prefix) {
			return true
		}
	}
	return false
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
// Without a target askpass environment it uses BatchMode (compat-only testing).
// With askpass it performs a full authentication test without trusting PTY text.
func TestConnection(ctx context.Context, hostname, username string, cfg TestConfig) (*TestResult, error) {
	if cfg.Timeout == 0 {
		cfg.Timeout = 10 * time.Second
	}

	args, clientLogPath, cleanup, err := buildTestSSHArgs(hostname, username, cfg)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	// Create context with timeout
	testCtx, cancel := context.WithTimeout(ctx, cfg.Timeout+5*time.Second)
	defer cancel()

	return testNonInteractive(testCtx, args, cfg.Env, clientLogPath), nil
}

// testNonInteractive runs SSH without a PTY. Authentication is either batch
// mode or isolated askpass, as selected by buildTestSSHArgs.
func testNonInteractive(ctx context.Context, args, env []string, clientLogPath string) *TestResult {
	cmd := exec.CommandContext(ctx, "ssh", args...)
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}

	// Capture combined output
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	err := cmd.Run()
	clientLog, _ := os.ReadFile(clientLogPath)
	return connectionTestResult(err, ctx.Err(), string(clientLog), output.String())
}

func connectionTestResult(runErr, contextErr error, clientLog, remoteOutput string) *TestResult {
	stderr := strings.TrimSuffix(clientLog, "\n")
	if stderr != "" && remoteOutput != "" {
		stderr += "\n"
	}
	stderr += remoteOutput

	result := &TestResult{
		Stderr:     stderr,
		AuthMethod: compat.ExtractAuthMethod(clientLog),
	}

	if runErr == nil {
		result.Success = true
		result.ExitCode = 0
		return result
	}

	// Extract exit code
	if exitErr, ok := runErr.(*exec.ExitError); ok {
		result.ExitCode = exitErr.ExitCode()
	} else {
		result.ExitCode = 1
	}

	// Check for timeout
	if contextErr == context.DeadlineExceeded || strings.Contains(strings.ToLower(stderr), "timed out") {
		result.ExitCode = 124
		return result
	}

	// Check if auth actually succeeded despite non-zero exit
	// (remote may reject "exit" command on some devices)
	if compat.DidAuthSucceed(clientLog) {
		result.Success = true
		result.Stderr = "[nssh] Remote CLI rejected probe command 'exit'; treating connection as successful.\n" + stderr
		return result
	}

	return result
}
