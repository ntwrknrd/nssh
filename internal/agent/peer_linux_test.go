//go:build linux

package agent

import (
	"net"
	"testing"
)

func TestVerifyPeer_SameUID_Linux(t *testing.T) {
	socketPath := testSocketPath(t)
	restore := SetSocketPathForTest(socketPath)
	defer restore()

	// Create a listener
	listener, err := CreateSocket(socketPath)
	if err != nil {
		t.Fatalf("CreateSocket() error = %v", err)
	}
	t.Cleanup(func() {
		if err := listener.Close(); err != nil {
			t.Fatalf("listener.Close() error = %v", err)
		}
	})

	// Connect from same process (same UID)
	clientConn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	t.Cleanup(func() {
		if err := clientConn.Close(); err != nil {
			t.Fatalf("clientConn.Close() error = %v", err)
		}
	})

	// Accept the connection
	serverConn, err := listener.Accept()
	if err != nil {
		t.Fatalf("Accept() error = %v", err)
	}
	t.Cleanup(func() {
		if err := serverConn.Close(); err != nil {
			t.Fatalf("serverConn.Close() error = %v", err)
		}
	})

	// Verify peer - should succeed for same UID
	unixConn := serverConn.(*net.UnixConn)
	if err := VerifyPeer(unixConn); err != nil {
		t.Errorf("VerifyPeer() error = %v, want nil for same UID", err)
	}
}

func TestVerifyPeer_DifferentUID_Linux(t *testing.T) {
	// This test would require running as root to create a connection from a different UID.
	// Skip in normal test runs.
	t.Skip("requires root to test different UID rejection")
}

func TestVerifyPeer_UsesSoPeercred(t *testing.T) {
	// We can't directly test that SO_PEERCRED is used without inspecting syscalls.
	// However, we can verify the function exists and returns expected results.
	socketPath := testSocketPath(t)
	restore := SetSocketPathForTest(socketPath)
	defer restore()

	listener, err := CreateSocket(socketPath)
	if err != nil {
		t.Fatalf("CreateSocket() error = %v", err)
	}
	t.Cleanup(func() {
		if err := listener.Close(); err != nil {
			t.Fatalf("listener.Close() error = %v", err)
		}
	})

	clientConn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	t.Cleanup(func() {
		if err := clientConn.Close(); err != nil {
			t.Fatalf("clientConn.Close() error = %v", err)
		}
	})

	serverConn, err := listener.Accept()
	if err != nil {
		t.Fatalf("Accept() error = %v", err)
	}
	t.Cleanup(func() {
		if err := serverConn.Close(); err != nil {
			t.Fatalf("serverConn.Close() error = %v", err)
		}
	})

	unixConn := serverConn.(*net.UnixConn)

	// The function should not panic and should return nil for same-process connections
	err = VerifyPeer(unixConn)
	if err != nil {
		t.Errorf("VerifyPeer() error = %v", err)
	}
}
