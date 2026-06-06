package inv

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ntwrknrd/nssh/internal/cli/selection"
	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/exit"
	"github.com/ntwrknrd/nssh/internal/inventory"
	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/spf13/cobra"
)

func newListCmd() *cobra.Command {
	var selectPattern string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List inventory",
		Long: `List SSH inventory hosts.

Use -s/--select to filter visible rows. Plain text searches all fields;
field:value matches a specific field exactly.

Fields: host, hostname, id, user, port, provider, group.

Examples:
  nssh inv list -s corp
  nssh inv list -s group:corp
  nssh inv list -s 'group:corp user:admin'`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(selectPattern)
		},
	}
	cmd.Flags().StringVarP(&selectPattern, "select", "s", "", "filter by text or field:value")
	return cmd
}

func runList(selectPattern string) error {
	cfg, err := config.LoadDefault()
	if err != nil {
		return err
	}
	index, err := inventory.BuildProviderIndex()
	if err != nil {
		return err
	}
	hosts, err := inventoryHosts(sshconfig.NewParser(), cfg, config.DefaultPaths())
	if err != nil {
		return err
	}

	sort.Slice(hosts, func(i, j int) bool { return hosts[i].Host < hosts[j].Host })
	metaForHost := func(host *sshconfig.HostEntry) hostMetadata {
		return metadataForHost(host, cfg, config.DefaultPaths(), index)
	}
	if selectPattern != "" {
		hosts, err = filterInventoryHosts(hosts, selectPattern, metaForHost)
		if err != nil {
			return &exit.ExitError{Code: 1, Message: fmt.Sprintf("invalid regex pattern: %s", err)}
		}
		if len(hosts) == 0 {
			ui.WarningCentered("No hosts matching pattern: %s", selectPattern)
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

func loadInventoryGroupSummaries(cfg *config.Config, parser *sshconfig.Parser, paths *config.Paths) ([]inventoryGroupSummary, error) {
	index, err := inventory.BuildProviderIndex()
	if err != nil {
		return nil, err
	}
	hosts, err := inventoryHosts(parser, cfg, paths)
	if err != nil {
		return nil, err
	}
	return inventoryGroupSummaries(cfg, paths, hosts, func(host *sshconfig.HostEntry) hostMetadata {
		return metadataForHost(host, cfg, paths, index)
	}), nil
}

type inventoryGroupSummary struct {
	Name         string
	ConfigFile   string
	OutputFile   string
	DomainSuffix string
	Total        int
	Sources      []inventoryGroupSource
}

type inventoryGroupSource struct {
	Provider string
	Hosts    int
}

func inventoryGroupSummaries(
	cfg *config.Config,
	paths *config.Paths,
	hosts []*sshconfig.HostEntry,
	metaForHost func(*sshconfig.HostEntry) hostMetadata,
) []inventoryGroupSummary {
	if paths == nil {
		paths = config.DefaultPaths()
	}
	names := make([]string, 0)
	groupConfigs := make(map[string]config.GroupConfig)
	groupProviders := make(map[string]string)
	groupSourceFiles := make(map[string]string)
	stats := make(map[string]map[string]int)
	for providerName, provider := range cfg.Inventory.Provider {
		for groupName, groupCfg := range provider.Group {
			id := config.FormatInventoryGroupID(providerName, groupName)
			names = append(names, id)
			groupConfigs[id] = groupCfg
			groupProviders[id] = providerName
			groupSourceFiles[id] = inventoryGroupConfigFile(cfg, providerName, groupName)
			stats[id] = make(map[string]int)
		}
	}
	for groupName, groupCfg := range cfg.Inventory.Group {
		id := config.FormatInventoryGroupID(config.ProviderLocal, groupName)
		if _, ok := stats[id]; ok {
			continue
		}
		names = append(names, id)
		groupConfigs[id] = groupCfg
		groupProviders[id] = config.ProviderLocal
		groupSourceFiles[id] = inventoryGroupConfigFile(cfg, config.ProviderLocal, groupName)
		stats[id] = make(map[string]int)
	}
	sort.Strings(names)

	for _, host := range hosts {
		meta := metaForHost(host)
		if _, ok := stats[meta.Group]; !ok {
			continue
		}
		provider := meta.Owner
		if provider == "" {
			provider = inventory.LocalProviderName
		}
		stats[meta.Group][provider]++
	}

	rows := make([]inventoryGroupSummary, 0, len(names))
	for _, name := range names {
		providers := stats[name]
		providerName := groupProviders[name]
		rows = append(rows, inventoryGroupSummary{
			Name:         name,
			ConfigFile:   valueOrDash(groupSourceFiles[name]),
			OutputFile:   localFilePath(paths, inventory.ProviderIncludeFile(providerName)),
			DomainSuffix: formatDomainSuffix(groupConfigs[name].DomainSuffix),
			Total:        totalProviderHosts(providers),
			Sources:      inventoryGroupSources(providers),
		})
	}
	return rows
}

func inventoryGroupConfigFile(cfg *config.Config, providerName, groupName string) string {
	source := cfg.InventoryGroupSource(providerName, groupName)
	if source == "" {
		source = cfg.InventoryProviderSource(providerName)
	}
	return source
}

func inventoryGroupSelectOptions(rows []inventoryGroupSummary, host string) []ui.SelectOption {
	options := make([]ui.SelectOption, 0, len(rows))
	for _, row := range rows {
		label := fmt.Sprintf("%s -> %s", row.Name, defaultHostNameForGroupSummary(host, row))
		options = append(options, ui.SelectOption{Label: label, Value: row.Name})
	}
	return options
}

func defaultHostNameForGroupSummary(host string, row inventoryGroupSummary) string {
	if host == "" || strings.Contains(host, ".") || row.DomainSuffix == "-" || strings.Contains(row.DomainSuffix, ",") {
		return host
	}
	if strings.HasPrefix(row.DomainSuffix, ".") {
		return host + row.DomainSuffix
	}
	return host + "." + row.DomainSuffix
}

func inventoryGroupSelectOptionsForNames(groups []string, options []ui.SelectOption) []ui.SelectOption {
	byGroup := make(map[string]ui.SelectOption, len(options))
	for _, option := range options {
		byGroup[option.Value] = option
	}
	filtered := make([]ui.SelectOption, 0, len(groups))
	for _, group := range groups {
		if option, ok := byGroup[group]; ok {
			filtered = append(filtered, option)
			continue
		}
		filtered = append(filtered, ui.SelectOption{Label: group, Value: group})
	}
	return filtered
}

func inventoryGroupSources(providers map[string]int) []inventoryGroupSource {
	if len(providers) == 0 {
		return nil
	}
	sources := make([]inventoryGroupSource, 0, len(providers))
	for provider, hosts := range providers {
		sources = append(sources, inventoryGroupSource{Provider: provider, Hosts: hosts})
	}
	sort.Slice(sources, func(i, j int) bool {
		if sources[i].Hosts != sources[j].Hosts {
			return sources[i].Hosts > sources[j].Hosts
		}
		return sources[i].Provider < sources[j].Provider
	})
	return sources
}

func totalProviderHosts(providers map[string]int) int {
	total := 0
	for _, count := range providers {
		total += count
	}
	return total
}

func formatDomainSuffix(suffixes []string) string {
	if len(suffixes) == 0 {
		return "-"
	}
	return strings.Join(suffixes, ", ")
}
