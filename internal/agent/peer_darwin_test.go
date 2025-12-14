//go:build darwin

package agent

import (
	"net"
	"testing"
)

func TestVerifyPeer_SameUID_Darwin(t *testing.T) {
	socketPath := testSocketPath(t)
	restore := SetSocketPathForTest(socketPath)
	defer restore()

	// Create a listener
	listener, err := CreateSocket(socketPath)
	if err != nil {
		t.Fatalf("CreateSocket() error = %v", err)
	}
	defer func() { _ = listener.Close() }()

	// Connect from same process (same UID)
	clientConn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer func() { _ = clientConn.Close() }()

	// Accept the connection
	serverConn, err := listener.Accept()
	if err != nil {
		t.Fatalf("Accept() error = %v", err)
	}
	defer func() { _ = serverConn.Close() }()

	// Verify peer - should succeed for same UID
	unixConn := serverConn.(*net.UnixConn)
	if err := VerifyPeer(unixConn); err != nil {
		t.Errorf("VerifyPeer() error = %v, want nil for same UID", err)
	}
}

func TestVerifyPeer_DifferentUID_Darwin(t *testing.T) {
	// This test would require running as root to create a connection from a different UID.
	// Skip in normal test runs.
	t.Skip("requires root to test different UID rejection")
}

func TestVerifyPeer_UsesLocalPeercred(t *testing.T) {
	// We can't directly test that LOCAL_PEERCRED is used without inspecting syscalls.
	// However, we can verify the function exists and returns expected results.
	socketPath := testSocketPath(t)
	restore := SetSocketPathForTest(socketPath)
	defer restore()

	listener, err := CreateSocket(socketPath)
	if err != nil {
		t.Fatalf("CreateSocket() error = %v", err)
	}
	defer func() { _ = listener.Close() }()

	clientConn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer func() { _ = clientConn.Close() }()

	serverConn, err := listener.Accept()
	if err != nil {
		t.Fatalf("Accept() error = %v", err)
	}
	defer func() { _ = serverConn.Close() }()

	unixConn := serverConn.(*net.UnixConn)

	// The function should not panic and should return nil for same-process connections
	err = VerifyPeer(unixConn)
	if err != nil {
		t.Errorf("VerifyPeer() error = %v", err)
	}
}
