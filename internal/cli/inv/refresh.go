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
	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
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

	ui.CommandStart("INVENTORY REFRESH")
	cfg, err := config.LoadDefault()
	if err != nil {
		ui.CommandEnd(ui.StatusError)
		return err
	}
	if err := validateRefreshTarget(cfg, target); err != nil {
		ui.CommandEnd(ui.StatusError)
		return err
	}

	results := refreshProviderCaches(cfg, target)
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
	ui.CommandEnd(refreshResultStatus(results))
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
	results := make(map[string]string)
	parser := sshconfig.NewParser()
	runner := newConfigOnlyRunner(parser)
	now := time.Now().UTC()

	for name := range cfg.Inventory.Provider {
		providerCfg := cfg.Inventory.Provider[name]
		if providerCfg.Type == config.ProviderLocal {
			continue
		}
		if providerName != "" && providerName != name {
			continue
		}
		provider, err := invproviders.New(providerCfg.Type)
		if err != nil {
			results[name] = "error: " + err.Error()
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), providerRefreshTimeout)
		result := inventory.RefreshProvider(ctx, name, providerCfg, provider, runner, inventory.RefreshOptions{
			Now:            now,
			WriteSSHConfig: true,
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

func refreshResultStatus(results map[string]string) ui.StatusType {
	for _, result := range results {
		if strings.HasPrefix(result, "error") {
			return ui.StatusWarning
		}
	}
	return ui.StatusSuccess
}

func newConfigOnlyRunner(parser *sshconfig.Parser) inventory.RemoteRunner {
	return remoteexec.NewSSHRunner(func(host string) (*remoteexec.HostInfo, error) {
		entry, err := parser.FindHost(host)
		if err != nil || entry == nil {
			return &remoteexec.HostInfo{Target: host, Hostname: host}, err
		}
		return &remoteexec.HostInfo{Target: host, Hostname: entry.HostName, Username: entry.User()}, nil
	})
}
