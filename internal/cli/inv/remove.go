package inv

import (
	"fmt"
	"strings"

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
			ui.UsageLinesAnnotation: "nssh inv rm HOST\nnssh inv rm -g local/GROUP",
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
	cmd.Flags().BoolVarP(&group, "group", "g", false, "treat argument as a provider-qualified group")
	return cmd
}

func runRemoveHost(host string) error {
	cfg, err := config.LoadDefault()
	if err != nil {
		return err
	}
	paths := config.DefaultPaths()
	removedSSHConfig, _ := localWrittenHostConfig(cfg, paths, host)
	removedHostConfig, err := config.InventoryHostAuthConfigText(paths.ConfigFile, cfg, host)
	if err != nil {
		return err
	}
	removed, err := removeLocalHost(cfg, paths, host)
	if err != nil {
		return err
	}
	if !removed {
		ui.Noop("Host %q not found", host)
		return nil
	}
	if err := removeInventoryHostConfig(config.DefaultPaths().ConfigFile, cfg, host); err != nil {
		return err
	}
	ui.Success("Host %q removed", host)
	printRemovedConfig(removedSSHConfig, removedHostConfig)
	return nil
}

func removeInventoryHostConfig(configPath string, cfg *config.Config, host string) error {
	if cfg == nil {
		return nil
	}
	delete(cfg.Inventory.Host, host)
	return config.DeleteInventoryHostAuth(configPath, cfg, host)
}

func runRemoveGroup(group string) error {
	cfg, err := config.LoadDefault()
	if err != nil {
		return err
	}
	providerName, groupName, err := config.ParseInventoryGroupID(group)
	if err != nil {
		return err
	}
	if providerName != config.ProviderLocal {
		return fmt.Errorf("local inventory group must use local/<group>")
	}
	if _, ok := cfg.Inventory.ProviderGroup(providerName, groupName); !ok {
		ui.Noop("Group %q not found", group)
		return nil
	}
	hosts, err := inventoryHosts(cfg, config.DefaultPaths())
	if err != nil {
		return err
	}
	if err := ensureInventoryGroupEmpty(group, hosts, cfg, config.DefaultPaths()); err != nil {
		return err
	}
	removedGroupConfig, err := config.InventoryGroupConfigText(config.DefaultPaths().ConfigFile, cfg, group)
	if err != nil {
		return err
	}
	if err := removeInventoryGroupConfig(config.DefaultPaths().ConfigFile, cfg, group); err != nil {
		return err
	}
	ui.Success("Group %q removed", group)
	printRemovedConfig(removedGroupConfig)
	return nil
}

func ensureInventoryGroupEmpty(group string, hosts []*sshconfig.HostEntry, cfg *config.Config, paths *config.Paths) error {
	for _, host := range hosts {
		if metadataForHost(host, cfg, paths, nil).Group == group {
			return fmt.Errorf("group %q still contains host %q", group, host.Host)
		}
	}
	return nil
}

func removeInventoryGroupConfig(configPath string, cfg *config.Config, group string) error {
	providerName, groupName, err := config.ParseInventoryGroupID(group)
	if err != nil {
		return err
	}
	localProvider := cfg.Inventory.Provider[providerName]
	delete(localProvider.Group, groupName)
	cfg.Inventory.Provider[providerName] = localProvider
	return config.DeleteInventoryGroup(configPath, cfg, group)
}

func printRemovedConfig(blocks ...string) {
	text := removedConfigText(blocks...)
	if text == "" {
		return
	}
	ui.Info("Removed config:")
	fmt.Print(text)
}

func removedConfigText(blocks ...string) string {
	var b strings.Builder
	wrote := false
	for _, block := range blocks {
		block = strings.TrimRight(block, "\n")
		if strings.TrimSpace(block) == "" {
			continue
		}
		if wrote {
			b.WriteString("\n")
		}
		for _, line := range strings.Split(block, "\n") {
			if strings.TrimSpace(line) == "" {
				b.WriteString("\n")
				continue
			}
			b.WriteString("    ")
			b.WriteString(line)
			b.WriteString("\n")
		}
		wrote = true
	}
	return b.String()
}
