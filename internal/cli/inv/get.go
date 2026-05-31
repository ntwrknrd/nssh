package inv

import (
	"fmt"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/inventory"
	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/spf13/cobra"
)

func newGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get NAME",
		Short: "Show inventory details",
		Long:  "Show host inventory details.",
		Args:  cobra.MaximumNArgs(1),
		Annotations: map[string]string{
			ui.UsageLinesAnnotation: "nssh inv get HOST",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			target, err := inventoryTargetArg(args, false)
			if err != nil {
				return err
			}
			return runGet(target)
		},
	}
	return cmd
}

func runGet(hostName string) error {
	ui.CommandStart("INVENTORY HOST")
	cfg, err := config.LoadDefault()
	if err != nil {
		ui.CommandEnd(ui.StatusError)
		return err
	}
	index, err := inventory.BuildProviderIndex()
	if err != nil {
		ui.CommandEnd(ui.StatusError)
		return err
	}
	host, _, err := findInventoryHostWithLocation(sshconfig.NewParser(), cfg, config.DefaultPaths(), hostName)
	if err != nil {
		ui.CommandEnd(ui.StatusError)
		return err
	}
	if host == nil {
		ui.CommandEnd(ui.StatusError)
		return fmt.Errorf("host %q not found", hostName)
	}
	meta := metadataForHost(host, cfg, config.DefaultPaths(), index)
	auth := effectiveInventoryAuth(cfg, host.Host, meta.Group)
	printInventoryDisplaySections(inventoryDisplaySections(
		inventoryHostSSHDisplayRows(host.Host, host.HostName, valueOrDash(host.User()), host.Port()),
		append([]inventoryDisplayRow{
			{Label: "Provider", Value: meta.Owner},
			{Label: "Group", Value: meta.Group},
		}, inventoryAuthDisplayRows(auth)...),
	))
	ui.CommandEnd(ui.StatusSuccess)
	return nil
}

func valueOrDash(v string) string {
	if v == "" {
		return "-"
	}
	return v
}
