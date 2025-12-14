//go:build linux || darwin

package session

import "github.com/ntwrknrd/nssh/internal/vault"

// Lock terminates the agent session.
// Delegates to vault.Manager.Lock() which uses injected SessionDeps.Lock.
func Lock(mgr *vault.Manager) error {
	return mgr.Lock()
}
