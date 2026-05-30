package inv

import (
	"fmt"
	"sort"

	"github.com/ntwrknrd/nssh/internal/cli/selection"
	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/exit"
	"github.com/ntwrknrd/nssh/internal/inventory"
	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/spf13/cobra"
)

func newListCmd() *cobra.Command {
	var groups bool
	var selectPattern string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List inventory",
		Long: `List SSH inventory hosts or groups.

Use -s/--select to filter visible rows. Plain text searches all fields;
field:value matches a specific field exactly.

Fields: host, hostname, id, user, port, provider, group.

Examples:
  nssh inv list -s cbb
  nssh inv list -s group:cbb
  nssh inv list -s 'group:cbb user:admin'
  nssh inv list -g`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if groups {
				return runListGroups()
			}
			return runList(selectPattern)
		},
	}
	cmd.Flags().StringVarP(&selectPattern, "select", "s", "", "filter by text or field:value")
	cmd.Flags().BoolVarP(&groups, "groups", "g", false, "list groups")
	return cmd
}

func runList(selectPattern string) error {
	ui.CommandStart("INVENTORY")
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
	hosts, err := inventoryHosts(sshconfig.NewParser(), cfg, config.DefaultPaths())
	if err != nil {
		ui.CommandEnd(ui.StatusError)
		return err
	}

	sort.Slice(hosts, func(i, j int) bool { return hosts[i].Host < hosts[j].Host })
	metaForHost := func(host *sshconfig.HostEntry) hostMetadata {
		return metadataForHost(host, cfg, config.DefaultPaths(), index)
	}
	if selectPattern != "" {
		hosts, err = filterInventoryHosts(hosts, selectPattern, metaForHost)
		if err != nil {
			ui.CommandEnd(ui.StatusError)
			return &exit.ExitError{Code: 1, Message: fmt.Sprintf("invalid regex pattern: %s", err)}
		}
		if len(hosts) == 0 {
			ui.WarningCentered("No hosts matching pattern: %s", selectPattern)
			ui.CommandEnd(ui.StatusWarning)
			return nil
		}
	}
	table := ui.NewTable("Host", "HostName", "User", "Port", "Provider", "Group")
	count := 0
	for _, host := range hosts {
		meta := metaForHost(host)
		hostName := host.HostName
		if hostName == host.Host {
			hostName = "-"
		}
		user := host.User()
		if user == "" {
			user = "-"
		}
		table.AddRow(host.Host, hostName, user, host.Port(), meta.Owner, meta.Group)
		count++
	}
	if selectPattern != "" {
		ui.InfoWithMargin(table.LeftMargin(), "Filter: %s", selectPattern)
	}
	table.Render()
	ui.InfoWithMargin(table.LeftMargin(), "Total: %d hosts", count)
	ui.CommandEnd(ui.StatusSuccess)
	return nil
}

func filterInventoryHosts(
	hosts []*sshconfig.HostEntry,
	selectPattern string,
	metaForHost func(*sshconfig.HostEntry) hostMetadata,
) ([]*sshconfig.HostEntry, error) {
	selector, err := selection.Compile(selectPattern, []string{"host", "hostname", "id", "user", "port", "provider", "group"})
	if err != nil {
		return nil, err
	}
	filtered := make([]*sshconfig.HostEntry, 0, len(hosts))
	for _, host := range hosts {
		meta := metaForHost(host)
		if selector.Match(selection.Row{
			"host":     host.Host,
			"hostname": host.HostName,
			"id":       sshconfig.DeriveHostID(host.Host),
			"user":     host.User(),
			"port":     host.Port(),
			"provider": meta.Owner,
			"group":    meta.Group,
		}) {
			filtered = append(filtered, host)
		}
	}
	return filtered, nil
}

func runListGroups() error {
	ui.CommandStart("INVENTORY GROUPS")
	cfg, err := config.LoadDefault()
	if err != nil {
		ui.CommandEnd(ui.StatusError)
		return err
	}
	names := make([]string, 0, len(cfg.Inventory.Group))
	for name := range cfg.Inventory.Group {
		names = append(names, name)
	}
	sort.Strings(names)
	table := ui.NewTable("Group", "Domain Suffix")
	for _, name := range names {
		group := cfg.Inventory.Group[name]
		table.AddRow(name, fmt.Sprintf("%v", group.DomainSuffix))
	}
	table.Render()
	ui.InfoWithMargin(table.LeftMargin(), "Total: %d groups", len(names))
	ui.CommandEnd(ui.StatusSuccess)
	return nil
}
