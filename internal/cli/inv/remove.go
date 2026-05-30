package inv

import (
	"fmt"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/spf13/cobra"
)

func newRemoveCmd() *cobra.Command {
	var group bool
	cmd := &cobra.Command{
		Use:     "rm NAME",
		Aliases: []string{"remove"},
		Short:   "Remove inventory entries",
		Long:    "Remove a local-provider host or group.",
		Args:    cobra.MaximumNArgs(1),
		Annotations: map[string]string{
			ui.UsageLinesAnnotation: "nssh inv rm HOST\nnssh inv rm -g GROUP",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			target, err := inventoryTargetArg(args, group)
			if err != nil {
				return err
			}
			if group {
				return runRemoveGroup(target)
			}
			return runRemoveHost(target)
		},
	}
	cmd.Flags().BoolVarP(&group, "group", "g", false, "treat argument as a group name")
	return cmd
}

func runRemoveHost(host string) error {
	ui.CommandStart("REMOVE INVENTORY HOST")
	cfg, err := config.LoadDefault()
	if err != nil {
		ui.CommandEnd(ui.StatusError)
		return err
	}
	removed, err := removeLocalHost(sshconfig.NewParser(), cfg, config.DefaultPaths(), host)
	if err != nil {
		ui.CommandEnd(ui.StatusError)
		return err
	}
	if !removed {
		ui.Noop("Host %q not found", host)
		ui.CommandEnd(ui.StatusNoop)
		return nil
	}
	ui.Success("Host %q removed", host)
	ui.CommandEnd(ui.StatusSuccess)
	return nil
}

func runRemoveGroup(group string) error {
	ui.CommandStart("REMOVE INVENTORY GROUP")
	cfg, err := config.LoadDefault()
	if err != nil {
		ui.CommandEnd(ui.StatusError)
		return err
	}
	if _, ok := cfg.Inventory.Group[group]; !ok {
		ui.Noop("Group %q not found", group)
		ui.CommandEnd(ui.StatusNoop)
		return nil
	}
	for provider := range cfg.Inventory.Provider {
		providerCfg := cfg.Inventory.Provider[provider]
		for _, route := range providerCfg.Route {
			if route.Group == group {
				ui.CommandEnd(ui.StatusError)
				return fmt.Errorf("group %q is referenced by inventory.provider.%s route config", group, provider)
			}
		}
	}
	hosts, err := inventoryHosts(sshconfig.NewParser(), cfg, config.DefaultPaths())
	if err != nil {
		ui.CommandEnd(ui.StatusError)
		return err
	}
	for _, host := range hosts {
		if metadataForHost(host, cfg, config.DefaultPaths(), nil).Group == group {
			ui.CommandEnd(ui.StatusError)
			return fmt.Errorf("group %q still contains host %q", group, host.Host)
		}
	}
	delete(cfg.Inventory.Group, group)
	if err := config.DeleteInventoryGroup(config.DefaultPaths().ConfigFile, cfg, group); err != nil {
		ui.CommandEnd(ui.StatusError)
		return err
	}
	ui.Success("Group %q removed", group)
	ui.CommandEnd(ui.StatusSuccess)
	return nil
}
