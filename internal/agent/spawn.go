//go:build linux || darwin

package agent

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// SpawnTimeout is the maximum time to wait for the agent to signal readiness.
const SpawnTimeout = 10 * time.Second

// waitForReady waits for the agent to signal readiness or report an error.
// The agent writes "ok\n" on success or "err:message\n" on failure.
func waitForReady(r *os.File, timeout time.Duration) error {
	defer func() { _ = r.Close() }()

	// Set read deadline
	_ = r.SetReadDeadline(time.Now().Add(timeout))

	// Read response from agent
	buf := make([]byte, 256)
	n, err := r.Read(buf)
	if err != nil {
		return fmt.Errorf("agent failed to start (no response within %v): %w", timeout, err)
	}

	msg := strings.TrimSpace(string(buf[:n]))
	if msg == "ok" {
		return nil
	}
	if strings.HasPrefix(msg, "err:") {
		return fmt.Errorf("agent startup failed: %s", strings.TrimPrefix(msg, "err:"))
	}
	return fmt.Errorf("agent sent unexpected response: %q", msg)
}

// SpawnRuntime starts a provider-session runtime agent daemon.
func SpawnRuntime() error {
	readyR, readyW, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("create ready pipe: %w", err)
	}

	cmd := exec.Command(os.Args[0], "__agent")
	cmd.ExtraFiles = []*os.File{readyW}
	cmd.Env = append(os.Environ(), "NSSH_AGENT_RUNTIME=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		_ = readyR.Close()
		_ = readyW.Close()
		return fmt.Errorf("start agent: %w", err)
	}

	_ = readyW.Close()

	return waitForReady(readyR, SpawnTimeout)
}

// IsRunning checks if an agent is currently running and responsive.
func IsRunning() bool {
	client, err := Connect()
	if err != nil {
		return false
	}
	defer func() { _ = client.Close() }()

	_, err = client.Status()
	return err == nil
}
