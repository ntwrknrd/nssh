// Package sync implements the nssh sync command group for managing
// external inventory sync sources.
package sync

import "github.com/spf13/cobra"

// NewCmd creates the sync command group.
func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Manage inventory sync sources",
		Long:  "Discover, sync, and manage SSH targets from external sources of truth.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(NewListCmd())
	cmd.AddCommand(NewGetCmd())
	cmd.AddCommand(NewRunCmd())
	cmd.AddCommand(newCredentialCmd())

	return cmd
}
