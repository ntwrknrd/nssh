package inv

import (
	"fmt"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/inventory"
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
	cfg, err := config.LoadDefault()
	if err != nil {
		return err
	}
	index, err := inventory.BuildProviderIndex()
	if err != nil {
		return err
	}
	host, _, err := findInventoryHostWithLocation(nil, cfg, config.DefaultPaths(), hostName)
	if err != nil {
		return err
	}
	if host == nil {
		return fmt.Errorf("host %q not found", hostName)
	}
	meta := metadataForHost(host, cfg, config.DefaultPaths(), index)
	auth := effectiveInventoryAuth(cfg, host.Host, meta.Group)
	printInventoryDisplaySections(inventoryDisplaySections(
		inventoryHostSSHDisplayRows(host.Host, host.HostName, valueOrDash(host.User()), host.Port()),
		append([]inventoryDisplayRow{
			{Label: "Provider", Value: meta.Owner},
			{Label: "Group", Value: valueOrDash(meta.Group)},
		}, inventoryAuthDisplayRows(auth)...),
	))
	return nil
}

func valueOrDash(v string) string {
	if v == "" {
		return "-"
	}
	return v
}
