package inv

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/inventory"
	invproviders "github.com/ntwrknrd/nssh/internal/inventory/providers"
	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	var refresh bool
	cmd := &cobra.Command{
		Use:   "status [provider]",
		Short: "Show inventory provider status",
		Long:  "Show inventory provider cache state, route ownership, and output files.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			provider := ""
			if len(args) > 0 {
				provider = args[0]
			}
			return runStatus(provider, refresh)
		},
	}
	cmd.Flags().BoolVar(&refresh, "refresh", false, "refresh external provider caches before showing status")
	return cmd
}

func runStatus(providerName string, refresh bool) error {
	ui.CommandStart("INVENTORY PROVIDERS")
	cfg, err := config.LoadDefault()
	if err != nil {
		ui.CommandEnd(ui.StatusError)
		return err
	}
	refreshResults := map[string]string(nil)
	if refresh {
		refreshResults = refreshProviderCaches(cfg, providerName)
	}
	out, err := renderStatusTree(cfg, config.DefaultPaths(), providerName, time.Now().UTC(), refreshResults)
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
	refreshResults map[string]string,
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
		if providerName != "" && name != providerName {
			continue
		}
		if count > 0 {
			b.WriteByte('\n')
		}
		writeExternalProviderStatus(&b, name, cfg.Inventory.Provider[name], paths, now, refreshResults)
		count++
	}
	if count == 0 {
		return "", fmt.Errorf("provider %q not found", providerName)
	}
	return b.String(), nil
}

func writeLocalStatus(b *strings.Builder, cfg *config.Config, paths *config.Paths) (bool, error) {
	localFile := localFilePath(paths, inventory.LocalProviderIncludeFile())
	parsed, err := sshconfig.NewParser().ParseFile(localFile)
	if err != nil {
		return false, err
	}
	if len(parsed.Hosts) == 0 {
		return false, nil
	}

	b.WriteString("local\n")
	b.WriteString("  type: local\n")
	fmt.Fprintf(b, "  output: %s\n", localFile)

	groupSet := make(map[string]bool)
	for _, host := range parsed.Hosts {
		groupSet[inventory.LocalHostGroup(host, "")] = true
	}
	names := make([]string, 0, len(groupSet))
	for name := range groupSet {
		if strings.TrimSpace(name) != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		b.WriteString("  groups: -\n")
		return true, nil
	}
	fmt.Fprintf(b, "  groups: %s\n", strings.Join(names, ", "))
	return true, nil
}

func writeExternalProviderStatus(
	b *strings.Builder,
	name string,
	providerCfg config.InventoryProviderConfig,
	paths *config.Paths,
	now time.Time,
	refreshResults map[string]string,
) {
	state, err := inventory.LoadProviderState(name)
	cache := "missing"
	lastError := "-"
	if err != nil {
		lastError = err.Error()
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
	}

	b.WriteString(name)
	b.WriteByte('\n')
	fmt.Fprintf(b, "  type: %s\n", providerCfg.Type)
	fmt.Fprintf(b, "  cache: %s\n", cache)
	fmt.Fprintf(b, "  output: %s\n", localFilePath(paths, inventory.ProviderIncludeFile(name)))
	fmt.Fprintf(b, "  last error: %s\n", lastError)
	if result, ok := refreshResults[name]; ok {
		fmt.Fprintf(b, "  refresh: %s\n", result)
	}
	b.WriteString("  routes\n")
	if len(providerCfg.Route) == 0 {
		b.WriteString("    -\n")
		return
	}
	for i, route := range providerCfg.Route {
		fmt.Fprintf(b, "    [%d] group %s -> %s\n", i, route.Group, localFilePath(paths, inventory.ProviderIncludeFile(name)))
		writeRouteMatch(b, route.Match)
	}
}

func writeRouteMatch(b *strings.Builder, match config.InventoryRouteMatch) {
	if len(match) == 0 {
		b.WriteString("        match all\n")
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
		fmt.Fprintf(b, "        match %s = %s\n", field, strings.Join(values, ", "))
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

func refreshProviderCaches(cfg *config.Config, providerName string) map[string]string {
	results := make(map[string]string)
	parser := sshconfig.NewParser()
	runner := newConfigOnlyRunner(parser)
	now := time.Now().UTC()

	for name := range cfg.Inventory.Provider {
		providerCfg := cfg.Inventory.Provider[name]
		if providerName != "" && providerName != name {
			continue
		}
		provider, err := invproviders.New(providerCfg.Type)
		if err != nil {
			results[name] = "error: " + err.Error()
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		result := inventory.RefreshProvider(ctx, name, providerCfg, provider, runner, inventory.RefreshOptions{
			Now:            now,
			WriteSSHConfig: true,
			Groups:         cfg.Inventory.Group,
		})
		cancel()
		if result.Err != nil {
			results[name] = "error: " + result.Err.Error()
			continue
		}
		state, err := inventory.LoadProviderState(name)
		if err != nil {
			results[name] = "error: " + err.Error()
			continue
		}
		count := 0
		if state != nil {
			count = len(state.Objects)
		}
		results[name] = fmt.Sprintf("ok (%d objects)", count)
	}
	return results
}
