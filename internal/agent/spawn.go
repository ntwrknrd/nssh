//go:build linux || darwin

package agent

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/ntwrknrd/nssh/internal/secret"
)

// SpawnTimeout is the maximum time to wait for the agent to signal readiness.
const SpawnTimeout = 10 * time.Second

// Spawn starts a new agent daemon in the background with the given identity.
//
// The agent is started as a new session leader (via Setsid) so it survives
// terminal close. On Linux with systemd, the agent dies on logout (systemd
// kills user session processes) and XDG_RUNTIME_DIR is cleared. On macOS,
// the agent persists across logout/login and only terminates on reboot,
// idle timeout, max lifetime, or explicit lock command.
//
// The identity is passed to the agent via an inherited pipe (fd 3). The agent
// signals readiness on another pipe (fd 4) by writing "ok\n" on success or
// "err:message\n" on failure.
//
// After Spawn returns successfully, the identity secret has been transferred
// to the agent and should be destroyed by the caller.
func Spawn(identitySecret *secret.Secret) error {
	// Create pipe for identity transfer (parent writes, agent reads)
	identityR, identityW, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("create identity pipe: %w", err)
	}

	// Create pipe for readiness signal (agent writes, parent reads)
	readyR, readyW, err := os.Pipe()
	if err != nil {
		_ = identityR.Close()
		_ = identityW.Close()
		return fmt.Errorf("create ready pipe: %w", err)
	}

	// Prepare the agent command
	cmd := exec.Command(os.Args[0], "__agent")

	// Pass pipes as extra files (will become fd 3 and fd 4 in child)
	cmd.ExtraFiles = []*os.File{identityR, readyW}

	// Daemonize: create new session (no controlling terminal)
	// This is sufficient to survive parent terminal death
	// Note: Setpgid is redundant with Setsid and can cause EPERM on macOS
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true, // New session - survives parent terminal death
	}

	// Prevent stdin/stdout/stderr inheritance
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil

	// Start the agent
	if err := cmd.Start(); err != nil {
		_ = identityR.Close()
		_ = identityW.Close()
		_ = readyR.Close()
		_ = readyW.Close()
		return fmt.Errorf("start agent: %w", err)
	}

	// Close pipe ends we don't need
	_ = identityR.Close() // Agent will read from fd 3
	_ = readyW.Close()    // Agent will write to fd 4

	// Pass identity through pipe using secure memory access
	err = identitySecret.Use(func(identityBytes []byte) error {
		_, writeErr := identityW.Write(identityBytes)
		return writeErr
	})
	_ = identityW.Close()

	if err != nil {
		_ = readyR.Close()
		return fmt.Errorf("write identity to pipe: %w", err)
	}

	// Wait for agent to signal readiness (or report error)
	return waitForReady(readyR, SpawnTimeout)
}

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

// SpawnCache starts a cache-only agent daemon.
func SpawnCache() error {
	// Keep the fd layout identical to Spawn so __agent startup stays simple.
	dataR, dataW, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("create data pipe: %w", err)
	}

	readyR, readyW, err := os.Pipe()
	if err != nil {
		_ = dataR.Close()
		_ = dataW.Close()
		return fmt.Errorf("create ready pipe: %w", err)
	}

	cmd := exec.Command(os.Args[0], "__agent")
	cmd.ExtraFiles = []*os.File{dataR, readyW}
	cmd.Env = append(os.Environ(), "NSSH_AGENT_CACHE_ONLY=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		_ = dataR.Close()
		_ = dataW.Close()
		_ = readyR.Close()
		_ = readyW.Close()
		return fmt.Errorf("start agent: %w", err)
	}

	_ = dataR.Close()
	_ = dataW.Close()
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

	_, err = client.Hello()
	return err == nil
}
