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
  nssh inv list -s corp
  nssh inv list -s group:corp
  nssh inv list -s 'group:corp user:admin'
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
	rows, err := loadInventoryGroupSummaries(cfg, sshconfig.NewParser(), config.DefaultPaths())
	if err != nil {
		ui.CommandEnd(ui.StatusError)
		return err
	}
	summaryHeaders, summaryRows, providerHeaders, providerRows := inventoryGroupTables(rows)
	summaryTable := ui.NewTable(summaryHeaders...)
	for _, row := range summaryRows {
		summaryTable.AddRow(row...)
	}
	providerTable := ui.NewTable(providerHeaders...)
	for _, row := range providerRows {
		providerTable.AddRow(row...)
	}
	ui.RenderTitledTablesSideBySide("Groups", summaryTable, "Provider counts", providerTable, 4)
	ui.CommandEnd(ui.StatusSuccess)
	return nil
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
	return inventoryGroupSummaries(cfg, hosts, func(host *sshconfig.HostEntry) hostMetadata {
		return metadataForHost(host, cfg, paths, index)
	}), nil
}

type inventoryGroupSummary struct {
	Name         string
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
	hosts []*sshconfig.HostEntry,
	metaForHost func(*sshconfig.HostEntry) hostMetadata,
) []inventoryGroupSummary {
	names := make([]string, 0, len(cfg.Inventory.Group))
	stats := make(map[string]map[string]int, len(cfg.Inventory.Group))
	for name := range cfg.Inventory.Group {
		names = append(names, name)
		stats[name] = make(map[string]int)
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
		rows = append(rows, inventoryGroupSummary{
			Name:         name,
			DomainSuffix: formatDomainSuffix(cfg.Inventory.Group[name].DomainSuffix),
			Total:        totalProviderHosts(providers),
			Sources:      inventoryGroupSources(providers),
		})
	}
	return rows
}

func inventoryGroupSelectOptions(rows []inventoryGroupSummary) []ui.SelectOption {
	options := make([]ui.SelectOption, 0, len(rows))
	for _, row := range rows {
		label := fmt.Sprintf("%s  %s %s", row.Name, formatCount(row.Total), pluralize("host", row.Total))
		if len(row.Sources) > 0 {
			label += fmt.Sprintf("  %s %s", formatCount(len(row.Sources)), pluralize("source", len(row.Sources)))
		}
		if row.DomainSuffix != "-" {
			label += "  " + row.DomainSuffix
		}
		options = append(options, ui.SelectOption{Label: label, Value: row.Name})
	}
	return options
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

func inventoryGroupTables(summaries []inventoryGroupSummary) ([]string, [][]string, []string, [][]string) {
	providers := inventoryGroupProviderNames(summaries)
	summaryHeaders := []string{"Group", "Domain Suffix", "Total"}
	providerHeaders := providers
	summaryRows := make([][]string, 0, len(summaries))
	providerRows := make([][]string, 0, len(summaries))
	for _, summary := range summaries {
		counts := make(map[string]int, len(summary.Sources))
		for _, source := range summary.Sources {
			counts[source.Provider] = source.Hosts
		}
		summaryRows = append(summaryRows, []string{summary.Name, summary.DomainSuffix, formatCount(summary.Total)})
		providerRow := make([]string, 0, len(providers))
		for _, provider := range providers {
			count, ok := counts[provider]
			if !ok {
				providerRow = append(providerRow, "-")
				continue
			}
			providerRow = append(providerRow, formatCount(count))
		}
		providerRows = append(providerRows, providerRow)
	}
	return summaryHeaders, summaryRows, providerHeaders, providerRows
}

func inventoryGroupProviderNames(summaries []inventoryGroupSummary) []string {
	seen := make(map[string]bool)
	for _, summary := range summaries {
		for _, source := range summary.Sources {
			seen[source.Provider] = true
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		if name != inventory.LocalProviderName {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if seen[inventory.LocalProviderName] {
		return append([]string{inventory.LocalProviderName}, names...)
	}
	return names
}

func totalInventoryGroupHosts(rows []inventoryGroupSummary) int {
	total := 0
	for _, row := range rows {
		total += row.Total
	}
	return total
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

func formatCount(count int) string {
	text := fmt.Sprintf("%d", count)
	if len(text) <= 3 {
		return text
	}
	var b strings.Builder
	first := len(text) % 3
	if first == 0 {
		first = 3
	}
	b.WriteString(text[:first])
	for i := first; i < len(text); i += 3 {
		b.WriteByte(',')
		b.WriteString(text[i : i+3])
	}
	return b.String()
}

func pluralize(word string, count int) string {
	if count == 1 {
		return word
	}
	return word + "s"
}
