//go:build linux || darwin

package agent

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
)

// CreateSocket creates a Unix domain socket with secure permissions.
//
// Security measures:
// - Parent directory created with 0700 permissions
// - Socket created with restrictive umask (0077)
// - flock-based locking prevents TOCTOU races during creation
// - Stale sockets are removed before creating new ones
func CreateSocket(path string) (net.Listener, error) {
	// Ensure parent directory exists with secure permissions
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create socket directory: %w", err)
	}

	// Acquire exclusive lock to prevent TOCTOU race during socket creation.
	// Without this, a malicious same-user process could race to create a socket
	// between our Remove() and Listen() calls.
	lockPath := LockPath()
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("create lock file: %w", err)
	}

	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		_ = lockFile.Close()
		return nil, fmt.Errorf("acquire socket lock: %w", err)
	}

	// Clean up function releases lock and removes lock file
	cleanup := func() {
		_ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
		_ = lockFile.Close()
		_ = os.Remove(lockPath)
	}

	// Remove stale socket (now protected by lock)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		cleanup()
		return nil, fmt.Errorf("remove stale socket: %w", err)
	}

	// Set restrictive umask before creating socket to prevent permission race.
	// This ensures the socket is created with 0700 permissions (owner only).
	oldMask := syscall.Umask(0077)
	ln, err := net.Listen("unix", path)
	syscall.Umask(oldMask) // Restore original umask

	if err != nil {
		cleanup()
		return nil, fmt.Errorf("listen on socket: %w", err)
	}

	// Release lock - socket is now owned by this process
	cleanup()

	return ln, nil
}

// RemoveSocket removes the agent socket file.
// Safe to call even if socket doesn't exist.
func RemoveSocket(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// IsSocketAlive checks if an agent is listening on the socket.
func IsSocketAlive(path string) bool {
	conn, err := net.Dial("unix", path)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
