//go:build linux || darwin

package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

// socketPathOverride allows tests to override the socket path.
var (
	socketPathOverride string
	socketPathMu       sync.RWMutex
)

// SetSocketPathForTest overrides the socket path for testing.
// Returns a cleanup function to restore the original.
func SetSocketPathForTest(path string) func() {
	socketPathMu.Lock()
	old := socketPathOverride
	socketPathOverride = path
	socketPathMu.Unlock()
	return func() {
		socketPathMu.Lock()
		socketPathOverride = old
		socketPathMu.Unlock()
	}
}

// SocketPath returns the platform-appropriate Unix socket path for the agent.
//
// On Linux: $XDG_RUNTIME_DIR/nssh.sock (fallback: /tmp/nssh-{uid}.sock)
// On macOS: $TMPDIR/nssh.sock (per-user directory, cleared on reboot)
//
// On Linux with systemd, XDG_RUNTIME_DIR is cleared on logout, effectively
// terminating the session. On macOS, both the socket directory and agent
// process persist across logout/login - only reboot clears them.
func SocketPath() string {
	socketPathMu.RLock()
	override := socketPathOverride
	socketPathMu.RUnlock()
	if override != "" {
		return override
	}
	switch runtime.GOOS {
	case "linux":
		// XDG_RUNTIME_DIR is per-user, cleared on logout by systemd
		if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
			return filepath.Join(dir, "nssh.sock")
		}
		// Fallback for systems without systemd (containers, etc.)
		return filepath.Join(os.TempDir(), fmt.Sprintf("nssh-%d.sock", os.Getuid()))

	case "darwin":
		// $TMPDIR on macOS points to per-user sandbox (/var/folders/.../T/)
		// This directory persists across logout/login but is cleared on reboot.
		// The agent process also persists across logout (setsid detaches it from
		// the login session). Session terminates on: reboot, idle timeout (4h
		// default), max lifetime (24h default), or explicit lock command.
		return filepath.Join(os.TempDir(), "nssh.sock")

	default:
		// Generic fallback with UID to prevent collisions
		return filepath.Join(os.TempDir(), fmt.Sprintf("nssh-%d.sock", os.Getuid()))
	}
}

// LockPath returns the path to the socket creation lock file.
// This lock prevents TOCTOU races during socket creation.
func LockPath() string {
	return SocketPath() + ".lock"
}
