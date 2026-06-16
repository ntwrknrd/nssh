package inv

import (
	"fmt"

	"github.com/ntwrknrd/nssh/internal/config"
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
	cfg, err := config.LoadDefault()
	if err != nil {
		return err
	}
	result, err := importLocalCSV(nil, cfg, config.DefaultPaths(), path, group)
	if err != nil {
		return err
	}
	for _, msg := range result.Errors {
		ui.Warning("%s", msg)
	}
	ui.PrintKeyValue("Added", fmt.Sprintf("%d", result.Added))
	ui.PrintKeyValue("Skipped", fmt.Sprintf("%d", result.Skipped))
	ui.PrintKeyValue("Failed", fmt.Sprintf("%d", result.Failed))
	if result.Failed > 0 {
		return nil
	}
	return nil
}
