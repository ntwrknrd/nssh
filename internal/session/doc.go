//go:build linux || darwin

// Package session provides the composition root for vault manager construction.
//
// This package wires together the vault and agent packages, providing agent-backed
// session behavior for credential decryption. It serves as the dependency injection
// point that CLI commands use to obtain a configured vault manager.
//
// # Design Constraints
//
// This package may import:
//   - internal/vault (for manager construction)
//   - internal/agent (for session operations)
//
// This package must NOT import:
//   - internal/ui (no terminal dependencies)
//   - cobra or CLI packages
//   - golang.org/x/term
//
// This separation allows the session layer to be used in non-interactive contexts
// (e.g., the agent daemon itself).
//
// # Usage
//
// Use [NewVaultManager] to create a vault manager with agent session support:
//
//	mgr, err := session.NewVaultManager(vault.Auto())
//	if err != nil {
//	    return err
//	}
//	if mgr.NeedsUnlock() {
//	    // Prompt user and call unlock
//	}
//
// # Agent Communication
//
// The package provides agent-backed implementations of vault.SessionDeps:
//   - Availability checks with fast timeouts
//   - Decryption requests via Unix socket
//   - Session locking
package session
