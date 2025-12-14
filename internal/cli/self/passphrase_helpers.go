package self

import (
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/ntwrknrd/nssh/internal/vault/software"
)

// promptAndInitialize prompts for a new passphrase with confirmation and initializes the store.
func promptAndInitialize(ks software.Store) error {
	passphraseBuf, err := ui.PasswordSecureWithConfirm("passphrase")
	if err != nil {
		return err
	}
	defer passphraseBuf.Destroy()

	return ks.InitializeWithPassphrase(passphraseBuf.Bytes(), false)
}
