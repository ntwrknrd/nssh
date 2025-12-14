package lock

import (
	"fmt"

	clisession "github.com/ntwrknrd/nssh/internal/cli/session"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/ntwrknrd/nssh/internal/vault"
	"github.com/spf13/cobra"
)

// NewCmd creates the lock command.
func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lock",
		Short: "Lock credential vault",
		Long: `Lock credential vault & terminate agent daemon.

This immediately:
  - Terminates the background agent process
  - Clears the decryption key from memory
  - Makes stored credentials inaccessible

After locking, the next SSH connection will prompt for your
passphrase to start a new session.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLock()
		},
	}

	return cmd
}

func runLock() error {
	// Create vault manager via session composition root
	mgr, err := clisession.NewManager(vault.Auto())
	if err != nil {
		return fmt.Errorf("initialize vault: %w", err)
	}

	// Lock via session orchestration
	if err := clisession.Lock(mgr); err != nil {
		return err
	}

	ui.Success("Session locked")
	return nil
}
