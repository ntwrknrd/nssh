//go:build linux || darwin

package agent

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/ntwrknrd/nssh/internal/config"
)

func TestClient_Connect_NoAgent(t *testing.T) {
	// Use a non-existent socket path
	socketPath := testSocketPath(t)
	restore := SetSocketPathForTest(socketPath)
	defer restore()

	_, err := Connect()
	if err == nil {
		t.Error("Connect() expected error for non-existent agent")
	}
	if !errors.Is(err, ErrAgentNotRunning) {
		t.Errorf("Connect() error = %v, want ErrAgentNotRunning", err)
	}
}

func TestClient_Connect_Success(t *testing.T) {
	socketPath := testSocketPath(t)
	restore := SetSocketPathForTest(socketPath)
	defer restore()

	// Start a test agent
	identity := testIdentity(t)
	cancel, done := startTestAgent(t, identity)
	defer cancel()

	// Wait for agent to be ready
	if !waitForSocket(t, 5*time.Second) {
		t.Fatal("agent did not start in time")
	}

	// Connect
	client, err := Connect()
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer func() { _ = client.Close() }()

	// Cleanup
	cancel()
	<-done
}

func TestClient_Hello(t *testing.T) {
	socketPath := testSocketPath(t)
	restore := SetSocketPathForTest(socketPath)
	defer restore()

	identity := testIdentity(t)
	cancel, done := startTestAgent(t, identity)
	defer func() {
		cancel()
		<-done
	}()

	if !waitForSocket(t, 5*time.Second) {
		t.Fatal("agent did not start in time")
	}

	client, err := Connect()
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer func() { _ = client.Close() }()

	mode, err := client.Hello()
	if err != nil {
		t.Fatalf("Hello() error = %v", err)
	}
	if mode != ModeSoftware {
		t.Errorf("Hello() = %q, want %q", mode, ModeSoftware)
	}
}

func TestClient_Decrypt(t *testing.T) {
	socketPath := testSocketPath(t)
	restore := SetSocketPathForTest(socketPath)
	defer restore()

	identity := testIdentity(t)
	cancel, done := startTestAgent(t, identity)
	defer func() {
		cancel()
		<-done
	}()

	if !waitForSocket(t, 5*time.Second) {
		t.Fatal("agent did not start in time")
	}

	client, err := Connect()
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer func() { _ = client.Close() }()

	// Encrypt some data
	plaintext := []byte("secret message")
	ciphertext := encryptTestData(t, identity.Recipient(), plaintext)

	// Decrypt via agent
	got, err := client.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Errorf("Decrypt() = %q, want %q", got, plaintext)
	}
}

func TestClient_CachePutGet(t *testing.T) {
	socketPath := testSocketPath(t)
	restore := SetSocketPathForTest(socketPath)
	defer restore()

	identity := testIdentity(t)
	cancel, done := startTestAgent(t, identity)
	defer func() {
		cancel()
		<-done
	}()

	if !waitForSocket(t, 5*time.Second) {
		t.Fatal("agent did not start in time")
	}

	client, err := Connect()
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer func() { _ = client.Close() }()

	found, got, err := client.CacheGet("missing")
	if err != nil {
		t.Fatalf("CacheGet missing: %v", err)
	}
	if found || got != nil {
		t.Fatalf("missing cache found=%v got=%q", found, got)
	}

	if err := client.CachePut("credential:edge01", []byte(`{"username":"netops","password":"secret"}`)); err != nil {
		t.Fatalf("CachePut: %v", err)
	}
	found, got, err = client.CacheGet("credential:edge01")
	if err != nil {
		t.Fatalf("CacheGet: %v", err)
	}
	if !found || string(got) != `{"username":"netops","password":"secret"}` {
		t.Fatalf("cache found=%v got=%q", found, got)
	}
}

func TestClient_Recipient(t *testing.T) {
	socketPath := testSocketPath(t)
	restore := SetSocketPathForTest(socketPath)
	defer restore()

	identity := testIdentity(t)
	cancel, done := startTestAgent(t, identity)
	defer func() {
		cancel()
		<-done
	}()

	if !waitForSocket(t, 5*time.Second) {
		t.Fatal("agent did not start in time")
	}

	client, err := Connect()
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer func() { _ = client.Close() }()

	recipient, err := client.Recipient()
	if err != nil {
		t.Fatalf("Recipient() error = %v", err)
	}

	want := identity.Recipient().String()
	if recipient != want {
		t.Errorf("Recipient() = %q, want %q", recipient, want)
	}
}

func TestClient_Status(t *testing.T) {
	socketPath := testSocketPath(t)
	restore := SetSocketPathForTest(socketPath)
	defer restore()

	identity := testIdentity(t)
	cfg := RuntimeConfig{
		Agent: &config.AgentConfig{
			IdleTimeout: config.Duration(30 * time.Second),
			MaxLifetime: config.Duration(60 * time.Second),
		},
		Logger: testLogger(),
	}
	cancel, done := startTestAgentWithConfig(t, identity, cfg)
	defer func() {
		cancel()
		<-done
	}()

	if !waitForSocket(t, 5*time.Second) {
		t.Fatal("agent did not start in time")
	}

	client, err := Connect()
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer func() { _ = client.Close() }()

	status, err := client.Status()
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}

	if status.Mode != ModeSoftware {
		t.Errorf("Status.Mode = %q, want %q", status.Mode, ModeSoftware)
	}
	if status.IdleTimeout != 30 {
		t.Errorf("Status.IdleTimeout = %d, want 30", status.IdleTimeout)
	}
	if status.MaxLifetime != 60 {
		t.Errorf("Status.MaxLifetime = %d, want 60", status.MaxLifetime)
	}
	if status.RemainingLife <= 0 || status.RemainingLife > 60 {
		t.Errorf("Status.RemainingLife = %d, should be > 0 and <= 60", status.RemainingLife)
	}
	if status.RemainingIdle <= 0 || status.RemainingIdle > 30 {
		t.Errorf("Status.RemainingIdle = %d, should be > 0 and <= 30", status.RemainingIdle)
	}
}

func TestClient_Lock(t *testing.T) {
	socketPath := testSocketPath(t)
	restore := SetSocketPathForTest(socketPath)
	defer restore()

	identity := testIdentity(t)
	cancel, done := startTestAgent(t, identity)
	defer cancel()

	if !waitForSocket(t, 5*time.Second) {
		t.Fatal("agent did not start in time")
	}

	client, err := Connect()
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer func() { _ = client.Close() }()

	// Lock the agent
	if err := client.Lock(); err != nil {
		t.Fatalf("Lock() error = %v", err)
	}

	// Wait for agent to terminate
	select {
	case <-done:
		// Expected
	case <-time.After(5 * time.Second):
		t.Error("agent did not terminate after Lock()")
	}

	// Socket should be gone
	if !waitForSocketGone(t, 2*time.Second) {
		t.Error("socket still exists after Lock()")
	}
}

func TestClient_Close(t *testing.T) {
	socketPath := testSocketPath(t)
	restore := SetSocketPathForTest(socketPath)
	defer restore()

	identity := testIdentity(t)
	cancel, done := startTestAgent(t, identity)
	defer func() {
		cancel()
		<-done
	}()

	if !waitForSocket(t, 5*time.Second) {
		t.Fatal("agent did not start in time")
	}

	client, err := Connect()
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	// Close should not error
	if err := client.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// Further operations should fail
	_, err = client.Hello()
	if err == nil {
		t.Error("Hello() after Close() should error")
	}
}
