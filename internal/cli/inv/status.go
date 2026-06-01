package inv

import (
	"fmt"
	"os"
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

	snapshot, ok, err := statusProviderSnapshotForName(cfg, paths, providerName, now)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("provider %q not found", providerName)
	}
	var b strings.Builder
	writeStatusProvider(&b, snapshot, true)
	if providerName == config.ProviderLocal {
		if err := writeLocalFindings(&b, cfg, paths); err != nil {
			return "", err
		}
	}
	return b.String(), nil
}

func statusProviderSnapshotForName(
	cfg *config.Config,
	paths *config.Paths,
	providerName string,
	now time.Time,
) (statusProviderSnapshot, bool, error) {
	if providerName == config.ProviderLocal {
		return localStatusSnapshot(cfg, paths)
	}
	providerCfg, ok := cfg.Inventory.Provider[providerName]
	if !ok {
		return statusProviderSnapshot{}, false, nil
	}
	return externalStatusSnapshot(cfg, providerName, providerCfg, paths, now), true, nil
}

func writeLocalFindings(b *strings.Builder, cfg *config.Config, paths *config.Paths) error {
	localFile := localFilePath(paths, inventory.LocalProviderIncludeFile())
	parsed, err := sshconfig.NewParser().ParseFile(localFile)
	if err != nil {
		return err
	}
	var findings []localRefreshFinding
	visitLocalRefreshFindings(parsed.Hosts, cfg, paths, nil, localRefreshSkipDNS, func(finding localRefreshFinding) {
		findings = append(findings, finding)
	})
	if len(findings) > 0 {
		fmt.Fprintf(b, "  %s\n", statusSection("findings:"))
		for _, finding := range findings {
			fmt.Fprintf(b, "    %s [%s] %s: %s\n", finding.Host, finding.Group, finding.Issue, finding.Detail)
		}
	}
	return nil
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

	var b strings.Builder
	for i, snapshot := range snapshots {
		if i > 0 {
			b.WriteByte('\n')
		}
		writeStatusProvider(&b, snapshot, false)
	}
	return b.String(), nil
}

func writeStatusProvider(b *strings.Builder, snapshot statusProviderSnapshot, detailed bool) {
	fmt.Fprintf(b, "%s %s\n", statusProviderName(snapshot.Name), statusProviderType(snapshot.Type))
	if snapshot.Name != config.ProviderLocal {
		cache := dashboardCache(snapshot)
		if detailed {
			cache = snapshot.Cache
		}
		fmt.Fprintf(b, "  %s %s\n", statusLabel("cache:"), statusCache(cache))
	}
	fmt.Fprintf(b, "  %s %s\n", statusLabel("output:"), statusPath(displayStatusFile(snapshot.OutputFile)))
	if detailed && snapshot.Name != config.ProviderLocal {
		fmt.Fprintf(b, "  %s %s\n", statusLabel("last error:"), statusMaybeError(snapshot.LastError))
	}
	fmt.Fprintf(b, "  %s %s\n", statusLabel("hosts:"), pluralizeHosts(snapshot.Hosts))
	fmt.Fprintf(b, "  %s\n", statusSection("groups:"))
	if len(snapshot.Groups) == 0 {
		fmt.Fprintf(b, "    %s\n", statusDash("-"))
		return
	}
	for _, group := range snapshot.Groups {
		writeStatusGroup(b, group, detailed)
	}
}

func writeStatusGroup(b *strings.Builder, group statusProviderGroup, detailed bool) {
	fmt.Fprintf(b, "    %s\n", statusGroupName(group.Name))
	fmt.Fprintf(b, "      %s %s\n", statusLabel("hosts:"), pluralizeHosts(group.Hosts))
	fmt.Fprintf(b, "      %s %s\n", statusLabel("config:"), statusPath(displayStatusConfig(group.ConfigFile)))
	if detailed {
		fmt.Fprintf(b, "      %s %s\n", statusLabel("output:"), statusPath(displayStatusFile(group.OutputFile)))
		writeStatusGroupMatchTree(b, group.Match)
	}
	fmt.Fprintf(b, "      %s\n", statusSection("auth:"))
	if detailed {
		fmt.Fprintf(b, "        %s %s\n", statusLabel("mode:"), statusValue(group.Auth.AuthMode))
		fmt.Fprintf(b, "        %s %s\n", statusLabel("credential provider:"), statusValue(group.Auth.CredentialProvider))
	}
	fmt.Fprintf(b, "        %s %s\n", statusLabel("username:"), statusValue(dashboardUsername(group.Auth)))
	fmt.Fprintf(b, "        %s %s\n", statusLabel("username ref:"), statusPath(dashboardUsernameRef(group.Auth)))
	fmt.Fprintf(b, "        %s %s\n", statusLabel("password ref:"), statusPath(dashboardPasswordRef(group.Auth)))
}

func writeStatusGroupMatchTree(b *strings.Builder, match config.InventoryMatch) {
	if len(match) == 0 {
		fmt.Fprintf(b, "      %s %s\n", statusLabel("match:"), statusDash("-"))
		return
	}
	fmt.Fprintf(b, "      %s\n", statusSection("match:"))
	fields := make([]string, 0, len(match))
	for field := range match {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	for _, field := range fields {
		values := append([]string(nil), match[field]...)
		sort.Strings(values)
		fmt.Fprintf(b, "        %s %s\n", statusLabel(field+":"), statusValue(strings.Join(values, ", ")))
	}
}

func statusProviderName(value string) string {
	return ui.StyleCyan.Render(value)
}

func statusProviderType(value string) string {
	return ui.StyleDim.Render("(" + value + ")")
}

func statusGroupName(value string) string {
	return ui.StyleWhite.Render(value)
}

func statusSection(value string) string {
	return ui.StyleCyan.Faint(true).Render(value)
}

func statusLabel(value string) string {
	return ui.StyleDim.Render(value)
}

func statusValue(value string) string {
	if value == "-" {
		return statusDash(value)
	}
	return value
}

func statusPath(value string) string {
	if value == "-" {
		return statusDash(value)
	}
	return value
}

func statusDash(value string) string {
	return ui.StyleDim.Render(value)
}

func statusMaybeError(value string) string {
	if value != "" && value != "-" {
		return ui.StyleRed.Render(value)
	}
	return statusValue(value)
}

func statusCache(value string) string {
	switch {
	case strings.HasSuffix(value, " ok"):
		return strings.TrimSuffix(value, " ok") + " " + ui.StyleGreen.Render("ok")
	case strings.HasSuffix(value, " error"):
		return strings.TrimSuffix(value, " error") + " " + ui.StyleRed.Render("error")
	case strings.HasPrefix(value, "stale"), strings.HasPrefix(value, "missing"), strings.HasPrefix(value, "never"):
		return ui.StyleYellow.Render(value)
	default:
		return value
	}
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

func dashboardUsername(auth inventoryAuthView) string {
	if auth.Username != "" && auth.Username != "-" {
		return auth.Username
	}
	return "-"
}

func dashboardUsernameRef(auth inventoryAuthView) string {
	if auth.UsernameRef != "" && auth.UsernameRef != "-" {
		return auth.UsernameRef
	}
	return "-"
}

func dashboardPasswordRef(auth inventoryAuthView) string {
	if auth.PasswordRef != "" && auth.PasswordRef != "-" {
		return auth.PasswordRef
	}
	return "-"
}

func displayStatusFile(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || path == "-" {
		return "-"
	}
	return abbreviateHomePath(path)
}

func displayStatusConfig(path string) string {
	return displayStatusFile(path)
}

func abbreviateHomePath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	home = strings.TrimRight(home, "/")
	if path == home {
		return "~"
	}
	if strings.HasPrefix(path, home+"/") {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
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

func localRefreshSkipDNS(string) localRefreshDNSResult {
	return localRefreshDNSResult{status: "skip"}
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
