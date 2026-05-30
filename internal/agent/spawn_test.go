//go:build linux || darwin

package agent

import (
	"os"
	"testing"
	"time"
)

func TestSpawn_IsRunning(t *testing.T) {
	socketPath := testSocketPath(t)
	restore := SetSocketPathForTest(socketPath)
	defer restore()

	// Initially not running
	if IsRunning() {
		t.Error("IsRunning() = true, want false when no agent")
	}

	// Start an agent
	cancel, done := startTestAgent(t)
	defer func() {
		cancel()
		<-done
	}()

	if !waitForSocket(t, 5*time.Second) {
		t.Fatal("agent did not start in time")
	}

	// Now should be running
	if !IsRunning() {
		t.Error("IsRunning() = false, want true when agent is running")
	}

	// Stop and verify
	cancel()
	<-done

	if !waitForSocketGone(t, 2*time.Second) {
		t.Error("socket still exists after agent stopped")
	}

	// Should not be running anymore
	if IsRunning() {
		t.Error("IsRunning() = true, want false after agent stopped")
	}
}

func TestWaitForReady_Success(t *testing.T) {
	// Create a pipe to simulate readiness signaling
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("create pipe: %v", err)
	}
	defer func() { _ = r.Close() }()
	defer func() { _ = w.Close() }()

	// Write success signal
	go func() {
		time.Sleep(10 * time.Millisecond)
		_, _ = w.WriteString("ok\n")
	}()

	err = waitForReady(r, 1*time.Second)
	if err != nil {
		t.Errorf("waitForReady() error = %v, want nil", err)
	}
}

func TestWaitForReady_Error(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("create pipe: %v", err)
	}
	defer func() { _ = r.Close() }()
	defer func() { _ = w.Close() }()

	// Write error signal
	go func() {
		time.Sleep(10 * time.Millisecond)
		_, _ = w.WriteString("err:something went wrong\n")
	}()

	err = waitForReady(r, 1*time.Second)
	if err == nil {
		t.Error("waitForReady() expected error for err: signal")
	}
	// Error message includes prefix from waitForReady
	want := "agent startup failed: something went wrong"
	if err != nil && err.Error() != want {
		t.Errorf("waitForReady() error = %q, want %q", err.Error(), want)
	}
}

func TestWaitForReady_Timeout(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("create pipe: %v", err)
	}
	defer func() { _ = r.Close() }()
	defer func() { _ = w.Close() }()

	// Don't write anything - should timeout
	err = waitForReady(r, 100*time.Millisecond)
	if err == nil {
		t.Error("waitForReady() expected error for timeout")
	}
}

func TestWaitForReady_UnexpectedResponse(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("create pipe: %v", err)
	}
	defer func() { _ = r.Close() }()
	defer func() { _ = w.Close() }()

	// Write unexpected response
	go func() {
		time.Sleep(10 * time.Millisecond)
		_, _ = w.WriteString("unexpected\n")
	}()

	err = waitForReady(r, 1*time.Second)
	if err == nil {
		t.Error("waitForReady() expected error for unexpected response")
	}
}

func TestWaitForReady_EmptyResponse(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("create pipe: %v", err)
	}
	defer func() { _ = r.Close() }()

	// Close write end to signal EOF
	_ = w.Close()

	err = waitForReady(r, 1*time.Second)
	if err == nil {
		t.Error("waitForReady() expected error for empty response")
	}
}

func TestSpawnTimeout_Constant(t *testing.T) {
	// Verify the spawn timeout constant is set correctly
	if SpawnTimeout != 10*time.Second {
		t.Errorf("SpawnTimeout = %v, want 10s", SpawnTimeout)
	}
}

// TestSpawn_FullCycle tests the full spawn lifecycle using RunInBackground
// as a proxy since actual Spawn() creates a subprocess.
func TestSpawn_FullCycle(t *testing.T) {
	socketPath := testSocketPath(t)
	restore := SetSocketPathForTest(socketPath)
	defer restore()

	// Use RunInBackground as proxy for testing agent startup
	cancel, done := startTestAgent(t)
	defer cancel()

	if !waitForSocket(t, 5*time.Second) {
		t.Fatal("agent did not start in time")
	}

	// Verify agent is functional
	client, err := Connect()
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer func() { _ = client.Close() }()

	mode, err := client.Hello()
	if err != nil {
		t.Fatalf("Hello() error = %v", err)
	}
	if mode != ModeRuntime {
		t.Errorf("Hello() = %q, want %q", mode, ModeRuntime)
	}

	// Cleanup
	cancel()
	<-done
}

// Note: The following tests would require actual process spawning which is
// difficult to test in unit tests. They are documented here for completeness.

// TestSpawn_ReadinessSignaling would verify:
// - Agent signals readiness via pipe (fd 3)
// - Parent waits for readiness before returning
// - Timeout occurs if agent doesn't signal

// TestSpawn_Daemonization would verify:
// - Agent creates new session (setsid)
// - Agent survives parent terminal death
// - Agent runs in background
