package inv

import (
	"fmt"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/spf13/cobra"
)

func newImportCmd() *cobra.Command {
	var group string
	cmd := &cobra.Command{
		Use:   "import FILE.csv",
		Short: "Import inventory from CSV",
		Long:  "Import local-provider inventory from a CSV file.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runImport(args[0], group)
		},
	}
	cmd.Flags().StringVar(&group, "group", "", "target provider-qualified group")
	return cmd
}

func runImport(path, group string) error {
	ui.CommandStart("IMPORT INVENTORY")
	cfg, err := config.LoadDefault()
	if err != nil {
		ui.CommandEnd(ui.StatusError)
		return err
	}
	result, err := importLocalCSV(sshconfig.NewParser(), cfg, config.DefaultPaths(), path, group)
	if err != nil {
		ui.CommandEnd(ui.StatusError)
		return err
	}
	for _, msg := range result.Errors {
		ui.Warning("%s", msg)
	}
	ui.PrintKeyValue("Added", fmt.Sprintf("%d", result.Added))
	ui.PrintKeyValue("Skipped", fmt.Sprintf("%d", result.Skipped))
	ui.PrintKeyValue("Failed", fmt.Sprintf("%d", result.Failed))
	if result.Failed > 0 {
		ui.CommandEnd(ui.StatusError)
		return nil
	}
	ui.CommandEnd(ui.StatusSuccess)
	return nil
}
