//go:build linux || darwin

package agent

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"filippo.io/age"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/secret"
)

// testIdentity generates a new age identity for testing.
func testIdentity(t *testing.T) *age.X25519Identity {
	t.Helper()
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("generate test identity: %v", err)
	}
	return identity
}

// testIdentitySecret wraps a test identity in a secret.Secret.
func testIdentitySecret(t *testing.T) (*secret.Secret, *age.X25519Identity) {
	t.Helper()
	identity := testIdentity(t)
	return secret.NewFromString(identity.String()), identity
}

// testSocketPath returns a unique socket path for testing.
// Uses /tmp with short names to avoid Unix socket path length limits (~104 chars on macOS).
func testSocketPath(t *testing.T) string {
	t.Helper()
	// Create a short unique directory in /tmp
	dir, err := os.MkdirTemp("/tmp", "nssh")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "t.sock")
}

// startTestAgent starts an agent with short timeouts for testing.
// Returns cancel function and done channel.
func startTestAgent(t *testing.T, identity *age.X25519Identity) (cancel func(), done <-chan struct{}) {
	t.Helper()

	provider := NewSoftwareProvider(identity)
	cfg := RuntimeConfig{
		Agent: &config.AgentConfig{
			IdleTimeout: config.Duration(30 * time.Second),
			MaxLifetime: config.Duration(60 * time.Second),
		},
		Logger: testLogger(),
		Clock:  realClock{},
	}

	ctx := context.Background()
	return RunInBackground(ctx, provider, cfg)
}

// startTestAgentWithConfig starts an agent with custom config.
func startTestAgentWithConfig(t *testing.T, identity *age.X25519Identity, cfg RuntimeConfig) (cancel func(), done <-chan struct{}) {
	t.Helper()

	provider := NewSoftwareProvider(identity)
	if cfg.Logger == nil {
		cfg.Logger = testLogger()
	}
	// Use fast max sleep for tests (50ms) unless explicitly set
	if cfg.MaxSleep == 0 {
		cfg.MaxSleep = 50 * time.Millisecond
	}
	if cfg.Clock == nil {
		cfg.Clock = realClock{}
	}

	ctx := context.Background()
	return RunInBackground(ctx, provider, cfg)
}

// testLogger returns a logger that discards output.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// waitForSocket waits for the agent socket to become available.
func waitForSocket(t *testing.T, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if IsSocketAlive(SocketPath()) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// waitForSocketGone waits for the agent socket to be removed.
func waitForSocketGone(t *testing.T, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !IsSocketAlive(SocketPath()) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

func waitDone(t *testing.T, done <-chan struct{}, timeout time.Duration) bool {
	t.Helper()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// encryptTestData encrypts plaintext for decryption testing.
func encryptTestData(t *testing.T, recipient age.Recipient, plaintext []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, recipient)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if _, err := w.Write(plaintext); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return buf.Bytes()
}
