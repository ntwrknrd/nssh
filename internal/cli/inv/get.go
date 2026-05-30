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
	var group bool
	cmd := &cobra.Command{
		Use:   "get NAME",
		Short: "Show inventory details",
		Long:  "Show host or group inventory details.",
		Args:  cobra.MaximumNArgs(1),
		Annotations: map[string]string{
			ui.UsageLinesAnnotation: "nssh inv get HOST\nnssh inv get -g GROUP",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			target, err := inventoryTargetArg(args, group)
			if err != nil {
				return err
			}
			if group {
				return runGetGroup(target)
			}
			return runGet(target)
		},
	}
	cmd.Flags().BoolVarP(&group, "group", "g", false, "treat argument as a group name")
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

func runGetGroup(name string) error {
	ui.CommandStart("INVENTORY GROUP")
	cfg, err := config.LoadDefault()
	if err != nil {
		ui.CommandEnd(ui.StatusError)
		return err
	}
	group, ok := cfg.Inventory.Group[name]
	if !ok {
		ui.CommandEnd(ui.StatusError)
		return fmt.Errorf("group %q not found", name)
	}
	auth := inventoryAuthViewFromAuth("group "+name, group.Auth)
	if !group.Auth.IsSet() {
		auth = inventoryAuthView{Source: "-", Provider: "-", Ref: "-", Username: "-", UsernameRef: "-"}
	}
	printInventoryDisplaySections(inventoryDisplaySections(
		inventoryGroupSSHDisplayRows(fmt.Sprintf("%v", group.DomainSuffix), valueOrDash(group.DefaultUser)),
		append([]inventoryDisplayRow{
			{Label: "Group", Value: name},
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
