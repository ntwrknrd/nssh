package unlock

import (
	"fmt"
	"os"
	"time"

	clisession "github.com/ntwrknrd/nssh/internal/cli/session"
	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/ntwrknrd/nssh/internal/vault"
	"github.com/spf13/cobra"
)

// NewCmd creates the unlock command.
func NewCmd() *cobra.Command {
	var useStdin bool

	cmd := &cobra.Command{
		Use:   "unlock",
		Short: "Unlock credential vault",
		Long: `Start agent daemon & unlock credential vault.

This:
  - Prompts for your passphrase (or reads from stdin with --stdin)
  - Starts a background agent that holds the decryption key
  - Makes stored credentials accessible for SSH connections

The session remains active until idle timeout (default 4h) or
until you run 'nssh lock'.

For automation:
  echo "$PASSPHRASE" | nssh unlock --stdin`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return Run(useStdin)
		},
	}

	cmd.Flags().BoolVar(&useStdin, "stdin", false, "read passphrase from stdin (for automation)")

	return cmd
}

// Run executes the unlock operation. Exported for use by other packages (e.g., init).
func Run(useStdin bool) error {
	// Create vault manager via session composition root
	mgr, err := clisession.NewManager(vault.Auto())
	if err != nil {
		return fmt.Errorf("initialize vault: %w", err)
	}

	// Check if unlock is needed
	if !mgr.NeedsUnlock() {
		ui.Info("Already unlocked")
		return nil
	}

	// Unlock via session orchestration
	if err := clisession.Unlock(mgr, useStdin); err != nil {
		if err == ui.ErrInterrupted {
			os.Exit(130) // Standard exit code for SIGINT
		}
		return err
	}

	cfg, err := config.LoadDefault()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	idleTimeout := cfg.Agent.IdleTimeout.Duration()
	if idleTimeout == 0 {
		idleTimeout = 1 * time.Hour // Default
	}
	ui.Success("Session unlocked (idle timeout %s)", idleTimeout)
	return nil
}
