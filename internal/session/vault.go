//go:build linux || darwin

// Package session provides the composition root for vault manager construction.
// This package may import internal/vault and internal/agent for wiring,
// but must NOT import internal/ui, cobra, or golang.org/x/term.
package session

import (
	"context"
	"time"

	"github.com/ntwrknrd/nssh/internal/agent"
	"github.com/ntwrknrd/nssh/internal/vault"
)

// NewVaultManager creates a vault.Manager with agent-backed session behavior.
// This is the single composition root for vault manager construction.
func NewVaultManager(mode vault.Mode, opts ...vault.Option) (*vault.Manager, error) {
	// Build SessionDeps with agent-backed implementations
	deps := vault.SessionDeps{
		BaseContext: context.Background(),
		Available:   agentAvailable,
		Decrypt:     agentDecrypt,
		Lock:        agentLock,
	}

	// Prepend SessionDeps option so caller-provided options can override
	allOpts := append([]vault.Option{vault.WithSessionDeps(deps)}, opts...)
	return vault.NewManager(mode, allOpts...)
}

// agentAvailable checks if the agent is running with a fast timeout.
func agentAvailable(ctx context.Context) bool {
	// Fast timeout for availability check
	ctx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()

	done := make(chan bool, 1)
	go func() { done <- agent.IsRunning() }()

	select {
	case result := <-done:
		return result
	case <-ctx.Done():
		return false
	}
}

// agentDecrypt decrypts ciphertext via the agent with a 2s timeout.
func agentDecrypt(ctx context.Context, ciphertext []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	// Run decrypt in goroutine to respect context cancellation
	type result struct {
		plaintext []byte
		err       error
	}
	done := make(chan result, 1)

	go func() {
		client, err := agent.Connect()
		if err != nil {
			done <- result{nil, vault.ErrSessionUnavailable}
			return
		}
		defer func() { _ = client.Close() }()

		plaintext, err := client.Decrypt(ciphertext)
		done <- result{plaintext, err}
	}()

	select {
	case r := <-done:
		return r.plaintext, r.err
	case <-ctx.Done():
		return nil, vault.ErrSessionUnavailable
	}
}

// agentLock terminates the agent session with a 2s timeout.
func agentLock(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	// Run lock in goroutine to respect context cancellation
	done := make(chan error, 1)

	go func() {
		client, err := agent.Connect()
		if err != nil {
			done <- nil // Agent not running is fine for lock
			return
		}
		defer func() { _ = client.Close() }()
		done <- client.Lock()
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return nil // Timeout on lock is not fatal
	}
}
