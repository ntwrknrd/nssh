package inv

import (
	"context"
	"log/slog"
	"time"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/inventory"
	invproviders "github.com/ntwrknrd/nssh/internal/inventory/providers"
	"github.com/ntwrknrd/nssh/internal/ssh/remoteexec"
	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
)

func refreshStaleProvidersBestEffort(cfg *config.Config) {
	if cfg == nil || len(cfg.Inventory.Provider) == 0 {
		return
	}
	parser := sshconfig.NewParser()
	runner := newConfigOnlyRunner(parser)

	now := time.Now().UTC()
	for name, providerCfg := range cfg.Inventory.Provider {
		state, err := inventory.LoadProviderState(name)
		if err != nil {
			slog.Warn("skip provider refresh: state load failed", "provider", name, "err", err)
			continue
		}
		if !inventory.ProviderIsStale(state, providerCfg, now) {
			continue
		}
		provider, err := invproviders.New(providerCfg.Type)
		if err != nil {
			slog.Warn("skip provider refresh: provider unavailable", "provider", name, "err", err)
			continue
		}
		result := inventory.RefreshProvider(context.Background(), name, providerCfg, provider, runner, inventory.RefreshOptions{Now: now, WriteSSHConfig: true})
		if result.Err != nil {
			slog.Warn("provider refresh failed", "provider", name, "err", result.Err)
		}
	}
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
