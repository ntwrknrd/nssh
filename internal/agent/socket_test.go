//go:build linux || darwin

package agent

import (
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestCreateSocket_Success(t *testing.T) {
	socketPath := testSocketPath(t)
	restore := SetSocketPathForTest(socketPath)
	defer restore()

	listener, err := CreateSocket(socketPath)
	if err != nil {
		t.Fatalf("CreateSocket() error = %v", err)
	}
	defer func() { _ = listener.Close() }()

	// Verify socket file exists
	info, err := os.Stat(socketPath)
	if err != nil {
		t.Fatalf("socket file not created: %v", err)
	}
	if info.Mode().Type() != os.ModeSocket {
		t.Errorf("file type = %v, want socket", info.Mode().Type())
	}
}

func TestCreateSocket_CreatesParentDir(t *testing.T) {
	// Use short path to avoid Unix socket path length limits
	tmpDir, err := os.MkdirTemp("/tmp", "nssh")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	socketPath := filepath.Join(tmpDir, "sub", "t.sock")
	restore := SetSocketPathForTest(socketPath)
	defer restore()

	listener, err := CreateSocket(socketPath)
	if err != nil {
		t.Fatalf("CreateSocket() error = %v", err)
	}
	defer func() { _ = listener.Close() }()

	// Verify parent directory was created
	parentDir := filepath.Dir(socketPath)
	info, err := os.Stat(parentDir)
	if err != nil {
		t.Fatalf("parent dir not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("parent should be a directory")
	}
	if info.Mode().Perm() != 0700 {
		t.Errorf("parent dir permissions = %o, want 0700", info.Mode().Perm())
	}
}

func TestCreateSocket_SecurePermissions(t *testing.T) {
	socketPath := testSocketPath(t)
	restore := SetSocketPathForTest(socketPath)
	defer restore()

	listener, err := CreateSocket(socketPath)
	if err != nil {
		t.Fatalf("CreateSocket() error = %v", err)
	}
	defer func() { _ = listener.Close() }()

	info, err := os.Stat(socketPath)
	if err != nil {
		t.Fatalf("stat socket: %v", err)
	}

	// Socket permissions should be restrictive (0700 via umask)
	// Note: actual socket permissions vary by OS, but should be owner-only
	perm := info.Mode().Perm()
	if perm&0077 != 0 {
		t.Errorf("socket permissions = %o, should not allow group/other access", perm)
	}
}

func TestCreateSocket_StaleSocketCleanup(t *testing.T) {
	socketPath := testSocketPath(t)
	restore := SetSocketPathForTest(socketPath)
	defer restore()

	// Create a stale socket file
	parentDir := filepath.Dir(socketPath)
	if err := os.MkdirAll(parentDir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create a regular file pretending to be a stale socket
	if err := os.WriteFile(socketPath, []byte("stale"), 0600); err != nil {
		t.Fatalf("create stale file: %v", err)
	}

	// CreateSocket should remove the stale file and create a real socket
	listener, err := CreateSocket(socketPath)
	if err != nil {
		t.Fatalf("CreateSocket() error = %v", err)
	}
	defer func() { _ = listener.Close() }()

	// Verify it's now a socket
	info, err := os.Stat(socketPath)
	if err != nil {
		t.Fatalf("stat socket: %v", err)
	}
	if info.Mode().Type() != os.ModeSocket {
		t.Errorf("file type = %v, want socket", info.Mode().Type())
	}
}

func TestCreateSocket_FlockTOCTOU(t *testing.T) {
	socketPath := testSocketPath(t)
	restore := SetSocketPathForTest(socketPath)
	defer restore()

	// Try to create sockets concurrently - flock should serialize them
	var wg sync.WaitGroup
	var mu sync.Mutex
	var listeners []net.Listener
	var errors []error

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			listener, err := CreateSocket(socketPath)
			mu.Lock()
			if err != nil {
				errors = append(errors, err)
			} else {
				listeners = append(listeners, listener)
			}
			mu.Unlock()
		}()
	}

	wg.Wait()

	// Cleanup
	for _, l := range listeners {
		_ = l.Close()
	}

	// At least one should succeed, others may fail due to "address in use"
	if len(listeners) == 0 {
		t.Error("no CreateSocket() calls succeeded")
	}
}

func TestRemoveSocket_Success(t *testing.T) {
	socketPath := testSocketPath(t)
	restore := SetSocketPathForTest(socketPath)
	defer restore()

	// Create a socket
	listener, err := CreateSocket(socketPath)
	if err != nil {
		t.Fatalf("CreateSocket() error = %v", err)
	}
	_ = listener.Close()

	// Remove it
	if err := RemoveSocket(socketPath); err != nil {
		t.Errorf("RemoveSocket() error = %v", err)
	}

	// Verify it's gone
	if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
		t.Error("socket file should be removed")
	}
}

func TestRemoveSocket_NotExist(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "nonexistent.sock")

	// Should not error on missing file
	if err := RemoveSocket(socketPath); err != nil {
		t.Errorf("RemoveSocket() on missing file error = %v", err)
	}
}

func TestIsSocketAlive_Running(t *testing.T) {
	socketPath := testSocketPath(t)
	restore := SetSocketPathForTest(socketPath)
	defer restore()

	// Create a listening socket
	listener, err := CreateSocket(socketPath)
	if err != nil {
		t.Fatalf("CreateSocket() error = %v", err)
	}
	defer func() { _ = listener.Close() }()

	// Accept connections in background
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	// Give the listener a moment to start
	time.Sleep(10 * time.Millisecond)

	if !IsSocketAlive(socketPath) {
		t.Error("IsSocketAlive() = false, want true for listening socket")
	}
}

func TestIsSocketAlive_NotRunning(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "not.sock")

	if IsSocketAlive(socketPath) {
		t.Error("IsSocketAlive() = true, want false for non-existent socket")
	}
}

func TestIsSocketAlive_StaleSocket(t *testing.T) {
	socketPath := testSocketPath(t)
	restore := SetSocketPathForTest(socketPath)
	defer restore()

	// Create and immediately close a socket (makes it stale)
	listener, err := CreateSocket(socketPath)
	if err != nil {
		t.Fatalf("CreateSocket() error = %v", err)
	}
	_ = listener.Close()

	// Socket file exists but nothing is listening
	if IsSocketAlive(socketPath) {
		t.Error("IsSocketAlive() = true, want false for stale socket")
	}
}

func TestLockPath(t *testing.T) {
	socketPath := testSocketPath(t)
	restore := SetSocketPathForTest(socketPath)
	defer restore()

	lockPath := LockPath()
	want := socketPath + ".lock"

	if lockPath != want {
		t.Errorf("LockPath() = %q, want %q", lockPath, want)
	}
}

func TestSocketPath_Override(t *testing.T) {
	customPath := "/custom/path/test.sock"
	restore := SetSocketPathForTest(customPath)
	defer restore()

	if got := SocketPath(); got != customPath {
		t.Errorf("SocketPath() = %q, want %q", got, customPath)
	}
}

func TestSocketPath_Default(t *testing.T) {
	// Without override, should return platform-specific path
	path := SocketPath()
	if path == "" {
		t.Error("SocketPath() returned empty string")
	}
	if !filepath.IsAbs(path) {
		t.Errorf("SocketPath() = %q, should be absolute path", path)
	}
	if filepath.Base(path) != "nssh.sock" {
		t.Errorf("SocketPath() = %q, should end with nssh.sock", path)
	}
}
