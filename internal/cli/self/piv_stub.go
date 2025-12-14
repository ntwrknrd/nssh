//go:build !hardware

package self

import (
	"fmt"

	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/spf13/cobra"
)

// NewPivCmd creates a stub piv command for non-hardware builds.
func NewPivCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "piv",
		Short: "Manage enrolled YubiKeys",
		Long: `Manage YubiKeys enrolled for PIV hardware security.

This command requires the hardware build. Rebuild with:
  go build -tags hardware ./cmd/nssh`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ui.Error("Hardware support not compiled into this binary")
			ui.Info("Rebuild with: go build -tags hardware ./cmd/nssh")
			return fmt.Errorf("hardware support not available")
		},
	}

	return cmd
}
