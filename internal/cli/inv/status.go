package inv

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/inventory"
	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status [provider]",
		Short: "Show inventory provider status",
		Long:  "Show inventory provider cache state, group ownership, local findings, and output files.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			provider := ""
			if len(args) > 0 {
				provider = args[0]
			}
			return runStatus(provider)
		},
	}
	return cmd
}

func runStatus(providerName string) error {
	ui.CommandStart("INVENTORY PROVIDERS")
	cfg, err := config.LoadDefault()
	if err != nil {
		ui.CommandEnd(ui.StatusError)
		return err
	}
	out, err := renderStatusTree(cfg, config.DefaultPaths(), providerName, time.Now().UTC())
	if err != nil {
		ui.CommandEnd(ui.StatusError)
		return err
	}
	fmt.Print(out)
	ui.CommandEnd(ui.StatusSuccess)
	return nil
}

func renderStatusTree(
	cfg *config.Config,
	paths *config.Paths,
	providerName string,
	now time.Time,
) (string, error) {
	if cfg == nil {
		cfg = config.DefaultConfig()
	}
	if paths == nil {
		paths = config.DefaultPaths()
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if err := cfg.Inventory.Validate(); err != nil {
		return "", err
	}
	if providerName == "" {
		return renderStatusDashboard(cfg, paths, now)
	}

	var b strings.Builder
	count := 0
	if providerName == "" || providerName == "local" {
		wroteLocal, err := writeLocalStatus(&b, cfg, paths)
		if err != nil {
			return "", err
		}
		if wroteLocal {
			count++
		}
	}

	names := make([]string, 0, len(cfg.Inventory.Provider))
	for name := range cfg.Inventory.Provider {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if name == config.ProviderLocal {
			continue
		}
		if providerName != "" && name != providerName {
			continue
		}
		if count > 0 {
			b.WriteByte('\n')
		}
		writeExternalProviderStatus(&b, cfg, name, cfg.Inventory.Provider[name], paths, now)
		count++
	}
	if count == 0 {
		return "", fmt.Errorf("provider %q not found", providerName)
	}
	return b.String(), nil
}

func renderStatusDashboard(cfg *config.Config, paths *config.Paths, now time.Time) (string, error) {
	snapshots := make([]statusProviderSnapshot, 0, len(cfg.Inventory.Provider)+1)
	if local, ok, err := localStatusSnapshot(cfg, paths); err != nil {
		return "", err
	} else if ok {
		snapshots = append(snapshots, local)
	}

	names := make([]string, 0, len(cfg.Inventory.Provider))
	for name := range cfg.Inventory.Provider {
		if name == config.ProviderLocal {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		snapshots = append(snapshots, externalStatusSnapshot(cfg, name, cfg.Inventory.Provider[name], paths, now))
	}
	if len(snapshots) == 0 {
		return "", fmt.Errorf("provider %q not found", "")
	}

	providers := ui.NewTable("Provider", "Type", "Cache", "Hosts", "Groups", "Output")
	groups := ui.NewTable("Group", "Hosts", "Config", "Auth")
	for _, snapshot := range snapshots {
		providers.AddRow(
			snapshot.Name,
			snapshot.Type,
			dashboardCache(snapshot),
			pluralizeHosts(snapshot.Hosts),
			pluralizeGroups(len(snapshot.Groups)),
			displayStatusFile(snapshot.OutputFile),
		)
		for _, group := range snapshot.Groups {
			groups.AddRow(
				group.Name,
				fmt.Sprintf("%d", group.Hosts),
				displayStatusConfig(group.ConfigFile),
				dashboardAuth(group.Auth),
			)
		}
	}
	width := providers.Width()
	if groupWidth := groups.Width(); groupWidth > width {
		width = groupWidth
	}
	providers.WithMinWidth(width)
	groups.WithMinWidth(width)

	var b strings.Builder
	b.WriteString(providers.String())
	b.WriteString("\n\n")
	b.WriteString(groups.String())
	b.WriteByte('\n')
	return b.String(), nil
}

type statusProviderSnapshot struct {
	Name       string
	Type       string
	Cache      string
	LastError  string
	OutputFile string
	Hosts      int
	Groups     []statusProviderGroup
}

func localStatusSnapshot(cfg *config.Config, paths *config.Paths) (statusProviderSnapshot, bool, error) {
	localFile := localFilePath(paths, inventory.LocalProviderIncludeFile())
	parsed, err := sshconfig.NewParser().ParseFile(localFile)
	if err != nil {
		return statusProviderSnapshot{}, false, err
	}
	if len(parsed.Hosts) == 0 && !hasConfiguredLocalGroups(cfg) {
		return statusProviderSnapshot{}, false, nil
	}
	groupCounts := make(map[string]int)
	for _, host := range parsed.Hosts {
		group := normalizeStatusGroup(inventory.LocalHostGroup(host, "-"))
		groupCounts[group]++
	}
	return statusProviderSnapshot{
		Name:       config.ProviderLocal,
		Type:       config.ProviderLocal,
		Cache:      "-",
		LastError:  "-",
		OutputFile: localFile,
		Hosts:      len(parsed.Hosts),
		Groups:     statusProviderGroups(cfg, paths, config.ProviderLocal, groupCounts),
	}, true, nil
}

func externalStatusSnapshot(
	cfg *config.Config,
	name string,
	providerCfg config.InventoryProviderConfig,
	paths *config.Paths,
	now time.Time,
) statusProviderSnapshot {
	state, err := inventory.LoadProviderState(name)
	cache := "missing"
	lastError := "-"
	var groupCounts map[string]int
	hosts := 0
	if err != nil {
		if inventory.IsUnsupportedStateVersion(err) {
			cache = "stale, refresh required"
		} else {
			lastError = err.Error()
		}
	} else if state != nil {
		hosts = len(state.Objects)
		if state.LastRefresh.IsZero() {
			cache = fmt.Sprintf("never refreshed, %d objects", hosts)
		} else {
			cache = fmt.Sprintf("%s old, %d objects", formatCacheAge(now, state.LastRefresh), hosts)
		}
		if state.LastError != "" {
			lastError = state.LastError
		}
		groupCounts = providerGroupCounts(state)
	}
	return statusProviderSnapshot{
		Name:       name,
		Type:       providerCfg.Type,
		Cache:      cache,
		LastError:  lastError,
		OutputFile: localFilePath(paths, inventory.ProviderIncludeFile(name)),
		Hosts:      hosts,
		Groups:     statusProviderGroups(cfg, paths, name, groupCounts),
	}
}

func dashboardCache(snapshot statusProviderSnapshot) string {
	if snapshot.Name == config.ProviderLocal {
		return "-"
	}
	age := dashboardCacheAge(snapshot.Cache)
	if snapshot.LastError != "" && snapshot.LastError != "-" {
		if age != "" {
			return age + " error"
		}
		return "error"
	}
	if age != "" {
		return age + " ok"
	}
	switch {
	case strings.HasPrefix(snapshot.Cache, "never refreshed"):
		return "never"
	case strings.HasPrefix(snapshot.Cache, "stale"):
		return "stale"
	default:
		return snapshot.Cache
	}
}

func dashboardCacheAge(cache string) string {
	if before, _, ok := strings.Cut(cache, " old,"); ok {
		return before
	}
	return ""
}

func dashboardAuth(auth inventoryAuthView) string {
	values := make([]string, 0, 2)
	if auth.CredentialProvider != "" && auth.CredentialProvider != "-" {
		values = append(values, auth.CredentialProvider)
	} else if auth.AuthMode != "" && auth.AuthMode != "-" {
		values = append(values, auth.AuthMode)
	}
	if auth.Username != "" && auth.Username != "-" {
		values = append(values, auth.Username)
	}
	if len(values) == 0 {
		return "-"
	}
	return strings.Join(values, "/")
}

func displayStatusFile(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || path == "-" {
		return "-"
	}
	return filepath.Base(path)
}

func displayStatusConfig(path string) string {
	base := displayStatusFile(path)
	if strings.HasSuffix(base, ".toml") {
		return strings.TrimSuffix(base, ".toml")
	}
	return base
}

func writeLocalStatus(b *strings.Builder, cfg *config.Config, paths *config.Paths) (bool, error) {
	localFile := localFilePath(paths, inventory.LocalProviderIncludeFile())
	parsed, err := sshconfig.NewParser().ParseFile(localFile)
	if err != nil {
		return false, err
	}
	if len(parsed.Hosts) == 0 && !hasConfiguredLocalGroups(cfg) {
		return false, nil
	}

	b.WriteString("local\n")
	b.WriteString("  type: local\n")
	fmt.Fprintf(b, "  output: %s\n", localFile)
	fmt.Fprintf(b, "  hosts: %d\n", len(parsed.Hosts))

	groupCounts := make(map[string]int)
	for _, host := range parsed.Hosts {
		group := normalizeStatusGroup(inventory.LocalHostGroup(host, "-"))
		groupCounts[group]++
	}
	writeProviderGroups(b, cfg, paths, config.ProviderLocal, groupCounts)

	var findings []localRefreshFinding
	visitLocalRefreshFindings(parsed.Hosts, cfg, paths, nil, localRefreshSkipDNS, func(finding localRefreshFinding) {
		findings = append(findings, finding)
	})
	if len(findings) > 0 {
		b.WriteString("  findings\n")
		for _, finding := range findings {
			fmt.Fprintf(b, "    %s [%s] %s: %s\n", finding.Host, finding.Group, finding.Issue, finding.Detail)
		}
	}
	return true, nil
}

func hasConfiguredLocalGroups(cfg *config.Config) bool {
	if cfg == nil {
		return false
	}
	if provider, ok := cfg.Inventory.Provider[config.ProviderLocal]; ok && len(provider.Group) > 0 {
		return true
	}
	return len(cfg.Inventory.Group) > 0
}

func pluralizeHosts(count int) string {
	if count == 1 {
		return "1 host"
	}
	return fmt.Sprintf("%d hosts", count)
}

func pluralizeGroups(count int) string {
	if count == 1 {
		return "1 group"
	}
	return fmt.Sprintf("%d groups", count)
}

func localRefreshSkipDNS(string) localRefreshDNSResult {
	return localRefreshDNSResult{status: "skip"}
}

func writeExternalProviderStatus(
	b *strings.Builder,
	cfg *config.Config,
	name string,
	providerCfg config.InventoryProviderConfig,
	paths *config.Paths,
	now time.Time,
) {
	state, err := inventory.LoadProviderState(name)
	cache := "missing"
	lastError := "-"
	var groupCounts map[string]int
	if err != nil {
		if inventory.IsUnsupportedStateVersion(err) {
			cache = "stale, refresh required"
		} else {
			lastError = err.Error()
		}
	} else if state != nil {
		objectCount := len(state.Objects)
		if state.LastRefresh.IsZero() {
			cache = fmt.Sprintf("never refreshed, %d objects", objectCount)
		} else {
			cache = fmt.Sprintf("%s old, %d objects", formatCacheAge(now, state.LastRefresh), objectCount)
		}
		if state.LastError != "" {
			lastError = state.LastError
		}
		groupCounts = providerGroupCounts(state)
	}

	b.WriteString(name)
	b.WriteByte('\n')
	fmt.Fprintf(b, "  type: %s\n", providerCfg.Type)
	fmt.Fprintf(b, "  cache: %s\n", cache)
	fmt.Fprintf(b, "  output: %s\n", localFilePath(paths, inventory.ProviderIncludeFile(name)))
	fmt.Fprintf(b, "  last error: %s\n", lastError)
	if state != nil {
		fmt.Fprintf(b, "  hosts: %d\n", len(state.Objects))
	}
	writeProviderGroups(b, cfg, paths, name, groupCounts)
}

func writeProviderGroups(b *strings.Builder, cfg *config.Config, paths *config.Paths, provider string, groupCounts map[string]int) {
	b.WriteString("  groups\n")
	groups := statusProviderGroups(cfg, paths, provider, groupCounts)
	if len(groups) == 0 {
		b.WriteString("    -\n")
		return
	}
	for _, group := range groups {
		fmt.Fprintf(b, "    %s\n", group.Name)
		fmt.Fprintf(b, "      config: %s\n", valueOrDash(group.ConfigFile))
		fmt.Fprintf(b, "      output: %s\n", valueOrDash(group.OutputFile))
		fmt.Fprintf(b, "      hosts: %s\n", pluralizeHosts(group.Hosts))
		writeStatusGroupMatch(b, group.Match)
		fmt.Fprintf(b, "      auth mode: %s\n", group.Auth.AuthMode)
		fmt.Fprintf(b, "      credential provider: %s\n", group.Auth.CredentialProvider)
		fmt.Fprintf(b, "      username: %s\n", group.Auth.Username)
		fmt.Fprintf(b, "      username ref: %s\n", group.Auth.UsernameRef)
		fmt.Fprintf(b, "      password ref: %s\n", group.Auth.PasswordRef)
	}
}

type statusProviderGroup struct {
	Name       string
	ConfigFile string
	OutputFile string
	Hosts      int
	Match      config.InventoryMatch
	Auth       inventoryAuthView
}

func statusProviderGroups(cfg *config.Config, paths *config.Paths, provider string, groupCounts map[string]int) []statusProviderGroup {
	names := make(map[string]bool)
	configs := make(map[string]config.GroupConfig)
	configFiles := make(map[string]string)
	if cfg != nil {
		if providerCfg, ok := cfg.Inventory.Provider[provider]; ok {
			for groupName, groupCfg := range providerCfg.Group {
				groupID := config.FormatInventoryGroupID(provider, groupName)
				names[groupID] = true
				configs[groupID] = groupCfg
				configFiles[groupID] = inventoryGroupConfigFile(cfg, provider, groupName)
			}
		}
	}
	for group := range groupCounts {
		names[normalizeStatusGroup(group)] = true
	}

	sorted := make([]string, 0, len(names))
	for name := range names {
		sorted = append(sorted, name)
	}
	sort.Strings(sorted)

	groups := make([]statusProviderGroup, 0, len(sorted))
	for _, name := range sorted {
		providerName := provider
		groupCfg, hasConfig := configs[name]
		if !hasConfig && cfg != nil {
			if parsedProvider, parsedGroup, err := config.ParseInventoryGroupID(name); err == nil {
				providerName = parsedProvider
				groupCfg, hasConfig = cfg.Inventory.ProviderGroup(parsedProvider, parsedGroup)
				if hasConfig {
					configFiles[name] = inventoryGroupConfigFile(cfg, parsedProvider, parsedGroup)
				}
			} else if provider == config.ProviderLocal && name != "-" {
				groupCfg, hasConfig = cfg.Inventory.ProviderGroup(config.ProviderLocal, name)
				if hasConfig {
					configFiles[name] = inventoryGroupConfigFile(cfg, config.ProviderLocal, name)
				}
			}
		}
		auth := emptyInventoryAuthView()
		if hasConfig && groupCfg.Auth.IsSet() {
			auth = inventoryAuthViewFromAuth("group "+name, groupCfg.Auth)
		}
		groups = append(groups, statusProviderGroup{
			Name:       name,
			ConfigFile: valueOrDash(configFiles[name]),
			OutputFile: localFilePath(paths, inventory.ProviderIncludeFile(providerName)),
			Hosts:      groupCounts[name],
			Match:      groupCfg.Match,
			Auth:       auth,
		})
	}
	return groups
}

func providerGroupCounts(state *inventory.ProviderState) map[string]int {
	counts := make(map[string]int)
	if state == nil {
		return counts
	}
	for _, host := range state.Objects {
		group := "-"
		if host != nil && strings.TrimSpace(host.Group) != "" {
			group = host.Group
		}
		group = normalizeStatusGroup(group)
		counts[group]++
	}
	return counts
}

func normalizeStatusGroup(group string) string {
	group = strings.TrimSpace(group)
	if group == "" {
		return "-"
	}
	return group
}

func writeStatusGroupMatch(b *strings.Builder, match config.InventoryMatch) {
	if len(match) == 0 {
		b.WriteString("      match: -\n")
		return
	}
	fields := make([]string, 0, len(match))
	for field := range match {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	for _, field := range fields {
		values := append([]string(nil), match[field]...)
		sort.Strings(values)
		fmt.Fprintf(b, "      match %s = %s\n", field, strings.Join(values, ", "))
	}
}

func formatCacheAge(now, lastRefresh time.Time) string {
	age := now.Sub(lastRefresh)
	if age < 0 {
		age = 0
	}
	switch {
	case age >= 24*time.Hour:
		return fmt.Sprintf("%dd", int(age/(24*time.Hour)))
	case age >= time.Hour:
		return fmt.Sprintf("%dh", int(age/time.Hour))
	case age >= time.Minute:
		return fmt.Sprintf("%dm", int(age/time.Minute))
	default:
		return fmt.Sprintf("%ds", int(age/time.Second))
	}
}
