//go:build linux || darwin

// Package session provides CLI session orchestration.
// This wraps internal/session with interactive prompting and TTY/env/FD policy.
package session

import (
	"github.com/ntwrknrd/nssh/internal/session"
	"github.com/ntwrknrd/nssh/internal/vault"
)

// NewManager creates a vault.Manager via the session composition root.
// This is a convenience wrapper that adds no policy - just delegates.
func NewManager(mode vault.Mode, opts ...vault.Option) (*vault.Manager, error) {
	return session.NewVaultManager(mode, opts...)
}
