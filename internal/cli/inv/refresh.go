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
	"github.com/ntwrknrd/nssh/internal/ssh/remoteexec"
	"github.com/ntwrknrd/nssh/internal/ui"
	"github.com/spf13/cobra"
)

const providerRefreshTimeout = 2 * time.Minute

func newRefreshCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "refresh [provider|local]",
		Short: "Refresh inventory",
		Long:  "Refresh external provider caches, or run interactive local inventory repair with 'local'.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := ""
			if len(args) > 0 {
				target = args[0]
			}
			return runRefresh(target)
		},
	}
	return cmd
}

func runRefresh(target string) error {
	if target == "local" {
		return runLocalRefresh()
	}

	cfg, err := config.LoadDefault()
	if err != nil {
		return err
	}
	if err := validateRefreshTarget(cfg, target); err != nil {
		return err
	}

	var results map[string]string
	if err := ui.RunWithStatusSpinner("Refreshing inventory", func(update func(string)) error {
		results = refreshProviderCachesWithProgress(cfg, target, update)
		return nil
	}); err != nil {
		return err
	}
	names := make([]string, 0, len(results))
	for name := range results {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if result := results[name]; strings.HasPrefix(result, "error") {
			ui.Warning("%s: %s", name, result)
			continue
		}
		ui.Success("%s: %s", name, results[name])
	}
	return nil
}

func validateRefreshTarget(cfg *config.Config, target string) error {
	if target == "" || target == "local" {
		return nil
	}
	if cfg != nil {
		if _, ok := cfg.Inventory.Provider[target]; ok {
			return nil
		}
	}
	return fmt.Errorf("refresh target %q is not \"local\" or a configured provider", target)
}

func refreshProviderCaches(cfg *config.Config, providerName string) map[string]string {
	return refreshProviderCachesWithProgress(cfg, providerName, nil)
}

func refreshProviderCachesWithProgress(cfg *config.Config, providerName string, progress func(string)) map[string]string {
	results := make(map[string]string)
	now := time.Now().UTC()

	names := make([]string, 0, len(cfg.Inventory.Provider))
	for name := range cfg.Inventory.Provider {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		providerCfg := cfg.Inventory.Provider[name]
		if providerCfg.Type == config.ProviderLocal {
			continue
		}
		if providerName != "" && providerName != name {
			continue
		}
		if progress != nil {
			progress("Refreshing " + name)
		}
		provider, err := invproviders.New(providerCfg.Type)
		if err != nil {
			results[name] = "error: " + err.Error()
			continue
		}
		var runner inventory.RemoteRunner
		if providerCfg.Type == config.ProviderContainerlab {
			runner = newDirectRunner()
		}
		ctx, cancel := context.WithTimeout(context.Background(), providerRefreshTimeout)
		result := inventory.RefreshProvider(ctx, name, providerCfg, provider, runner, inventory.RefreshOptions{
			Now: now,
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

func newDirectRunner() inventory.RemoteRunner {
	return remoteexec.NewSSHRunner(func(host string) (*remoteexec.HostInfo, error) {
		return &remoteexec.HostInfo{Target: host, Hostname: host}, nil
	})
}
