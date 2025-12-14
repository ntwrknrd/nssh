//go:build linux || darwin

package session

import (
	"os"

	"github.com/ntwrknrd/nssh/internal/vault"
	"golang.org/x/term"
)

// TryUnlockIfTTY attempts to unlock if TTY available and unlock needed.
// Returns nil on success or if no TTY (caller continues without credentials).
// Returns error only on unlock failure with TTY present.
func TryUnlockIfTTY(mgr *vault.Manager) error {
	if !mgr.NeedsUnlock() {
		return nil
	}

	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return nil // No TTY, silently skip
	}

	return Unlock(mgr, false)
}

// EnsureUnlocked ensures the vault is unlocked, prompting if needed.
// Returns error if unlock is needed but fails.
func EnsureUnlocked(mgr *vault.Manager) error {
	if !mgr.NeedsUnlock() {
		return nil
	}

	return Unlock(mgr, false)
}
