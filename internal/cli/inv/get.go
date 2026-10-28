package inv

import (
	"fmt"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/connect"
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
	host, err := findInventoryHost(cfg, config.DefaultPaths(), hostName)
	if err != nil {
		return err
	}
	if host == nil {
		return fmt.Errorf("host %q not found", hostName)
	}
	meta := metadataForHost(host, cfg, config.DefaultPaths(), index)
	auth := effectiveInventoryAuth(cfg, host.Host, meta.Group)
	nsshRows := []inventoryDisplayRow{
		{Label: "Provider", Value: meta.Owner},
		{Label: "Group", Value: valueOrDash(meta.Group)},
	}
	shadowRows, err := shadowedProviderRows(cfg, config.DefaultPaths(), host.Host)
	if err != nil {
		return err
	}
	nsshRows = append(nsshRows, shadowRows...)
	printInventoryDisplaySections(inventoryDisplaySections(
		inventoryHostSSHDisplayRows(host.Host, host.HostName, valueOrDash(host.User()), host.Port()),
		append(nsshRows, inventoryAuthDisplayRows(auth)...),
	))
	return nil
}

// shadowedProviderRows names provider objects that this local host replaces.
func shadowedProviderRows(cfg *config.Config, paths *config.Paths, host string) ([]inventoryDisplayRow, error) {
	states, err := inventoryProviderStates(paths)
	if err != nil {
		return nil, err
	}
	var rows []inventoryDisplayRow
	for _, finding := range connect.ProviderHostFindings(cfg, states) {
		if finding.ShadowedBy != host {
			continue
		}
		rows = append(rows, inventoryDisplayRow{
			Label: "Shadows",
			Value: fmt.Sprintf("%s %s (move local settings to inventory.providers.%s.hosts)", finding.Provider, finding.Host, finding.Provider),
		})
	}
	return rows, nil
}

func valueOrDash(v string) string {
	if v == "" {
		return "-"
	}
	return v
}
