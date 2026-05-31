// Package inv provides inventory management commands.
package inv

import (
	"fmt"

	"github.com/spf13/cobra"
)

// NewCmd creates the inventory command group.
func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "inv",
		Short: "Manage inventory",
		Long:  "Manage SSH inventory across local and external inventory providers.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newListCmd())
	cmd.AddCommand(newGetCmd())
	cmd.AddCommand(newSetCmd())
	cmd.AddCommand(newRemoveCmd())
	cmd.AddCommand(newImportCmd())
	cmd.AddCommand(newRefreshCmd())
	cmd.AddCommand(newStatusCmd())
	return cmd
}

func inventoryTargetArg(args []string, group bool) (string, error) {
	if len(args) > 1 {
		return "", fmt.Errorf("too many arguments")
	}
	if len(args) == 0 {
		if group {
			return "", fmt.Errorf("group is required")
		}
		return "", fmt.Errorf("host is required")
	}
	return args[0], nil
}
