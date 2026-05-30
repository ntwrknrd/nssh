package connect

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/inventory"
	invproviders "github.com/ntwrknrd/nssh/internal/inventory/providers"
	"github.com/ntwrknrd/nssh/internal/ssh/remoteexec"
	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
	"github.com/ntwrknrd/nssh/internal/ui"
)

// ResolveHostname performs smart hostname resolution:
// - Exact match: returns hostname unchanged
// - Single partial match: returns the matched hostname
// - Multiple partial matches: opens fuzzy finder with query pre-filled
// - No matches: returns HostNotFoundError to trigger local inventory creation
func ResolveHostname(hostname string) (string, error) {
	parser := sshconfig.NewParser()

	result, err := parser.MatchHost(hostname)
	if err != nil {
		slog.Debug("failed to match host", "err", err)
		return hostname, nil
	}

	if result.Host != nil {
		if result.Host.Host != hostname {
			slog.Debug("auto-resolved hostname", "input", hostname, "resolved", result.Host.Host)
		}
		return result.Host.Host, nil
	}

	if len(result.Suggestions) > 0 {
		sort.Strings(result.Suggestions)
		selected, err := ui.FuzzySelectString("Select host", result.Suggestions, hostname)
		if err != nil {
			return "", fmt.Errorf("fuzzy select: %w", err)
		}
		if selected == "" {
			return "", fmt.Errorf("selection canceled")
		}
		return selected, nil
	}

	if hostname == "" {
		return "", fmt.Errorf("no hosts found in SSH config")
	}
	if refreshInventoryProvidersOnLookupMiss(hostname) {
		result, err = parser.MatchHost(hostname)
		if err != nil {
			slog.Debug("failed to match host after provider refresh", "err", err)
			return hostname, nil
		}
		if result.Host != nil {
			if result.Host.Host != hostname {
				slog.Debug("auto-resolved hostname after provider refresh", "input", hostname, "resolved", result.Host.Host)
			}
			return result.Host.Host, nil
		}
		if len(result.Suggestions) > 0 {
			sort.Strings(result.Suggestions)
			selected, err := ui.FuzzySelectString("Select host", result.Suggestions, hostname)
			if err != nil {
				return "", fmt.Errorf("fuzzy select: %w", err)
			}
			if selected == "" {
				return "", fmt.Errorf("selection canceled")
			}
			return selected, nil
		}
	}
	return "", &HostNotFoundError{Hostname: hostname}
}

var refreshInventoryProvidersOnLookupMiss = refreshProvidersOnce

func refreshProvidersOnce(hostname string) bool {
	cfg, err := config.LoadDefault()
	if err != nil {
		slog.Debug("skip provider refresh on lookup miss: config load failed", "host", hostname, "err", err)
		return false
	}
	if len(cfg.Inventory.Provider) == 0 {
		return false
	}
	runner := remoteexec.NewSSHRunner(func(host string) (*remoteexec.HostInfo, error) {
		resolved, err := ResolveHostForConnect(host, "", cfg)
		if err != nil {
			return nil, err
		}
		username := resolved.Username
		if resolved.HostEntry != nil && resolved.HostEntry.User() != "" {
			username = resolved.HostEntry.User()
		}
		return &remoteexec.HostInfo{Target: host, Hostname: resolved.Hostname, Username: username}, nil
	})

	refreshed := false
	now := time.Now().UTC()
	for name := range cfg.Inventory.Provider {
		providerCfg := cfg.Inventory.Provider[name]
		provider, err := invproviders.New(providerCfg.Type)
		if err != nil {
			slog.Warn("skip provider refresh: provider unavailable", "provider", name, "err", err)
			continue
		}
		result := inventory.RefreshProvider(context.Background(), name, providerCfg, provider, runner, inventory.RefreshOptions{Now: now, WriteSSHConfig: true})
		if result.Err != nil {
			slog.Warn("provider refresh failed during lookup miss", "provider", name, "host", hostname, "err", result.Err)
			continue
		}
		refreshed = true
	}
	return refreshed
}
