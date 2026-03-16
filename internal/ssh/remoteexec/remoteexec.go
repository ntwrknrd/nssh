// Package remoteexec provides non-interactive remote command execution via SSH.
// It takes pre-resolved host/user information and captures stdout/stderr.
package remoteexec

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// RemoteCommand describes a command to execute on a remote host.
type RemoteCommand struct {
	Argv    []string
	Sudo    bool
	Timeout time.Duration
}

// RemoteResult holds the output of a remote command execution.
type RemoteResult struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// HostInfo holds the resolved host information needed for remote execution.
type HostInfo struct {
	Target   string
	Hostname string
	Username string
}

// SSHRunner executes remote commands by shelling out to system ssh.
type SSHRunner struct {
	// ResolveHost is called to resolve a hostname into connection details.
	// This is injected by the CLI layer to share resolution with connect.
	ResolveHost func(host string) (*HostInfo, error)
}

// NewSSHRunner returns a new SSH-based remote runner with the given resolver.
func NewSSHRunner(resolver func(host string) (*HostInfo, error)) *SSHRunner {
	return &SSHRunner{ResolveHost: resolver}
}

// Run executes a command on a remote host via SSH.
func (r *SSHRunner) Run(ctx context.Context, host string, cmd RemoteCommand) (*RemoteResult, error) {
	info, err := r.ResolveHost(host)
	if err != nil {
		return nil, fmt.Errorf("resolve host %q: %w", host, err)
	}

	// Apply command timeout
	if cmd.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cmd.Timeout)
		defer cancel()
	}

	// Build SSH command
	argv := buildSSHArgs(info, cmd)

	sshCmd := exec.CommandContext(ctx, "ssh", argv...)

	var stdout, stderr bytes.Buffer
	sshCmd.Stdout = &stdout
	sshCmd.Stderr = &stderr

	err = sshCmd.Run()

	result := &RemoteResult{
		Stdout: stdout.Bytes(),
		Stderr: stderr.Bytes(),
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			return result, fmt.Errorf("ssh exec: %w", err)
		}
	}

	return result, nil
}

// buildSSHArgs constructs the SSH command-line arguments.
func buildSSHArgs(info *HostInfo, cmd RemoteCommand) []string {
	var args []string

	// Batch mode: no interactive prompts
	args = append(args, "-o", "BatchMode=yes")

	// User
	if info.Username != "" {
		args = append(args, "-l", info.Username)
	}

	// Use the original host alias when available so OpenSSH still applies
	// alias-bound config such as IdentityFile, ProxyJump, and Match rules.
	target := info.Target
	if target == "" {
		target = info.Hostname
	}
	args = append(args, target)

	// Build remote command
	remoteCmd := cmd.Argv
	if cmd.Sudo {
		remoteCmd = append([]string{"sudo"}, remoteCmd...)
	}

	quoted := make([]string, len(remoteCmd))
	for i, a := range remoteCmd {
		quoted[i] = "'" + strings.ReplaceAll(a, "'", "'\\''") + "'"
	}
	args = append(args, strings.Join(quoted, " "))

	return args
}
