package inventory

import (
	"context"
	"time"

	"github.com/ntwrknrd/nssh/internal/config"
)

// RefreshOptions controls provider refresh side effects.
type RefreshOptions struct {
	Now            time.Time
	WriteSSHConfig bool
}

// RefreshResult reports one provider refresh outcome.
type RefreshResult struct {
	Provider string
	Plan     *Plan
	Err      error
}

// RefreshProvider discovers, reconciles, saves provider state, and optionally
// rewrites provider-owned SSH config. It never touches credentials.
func RefreshProvider(
	ctx context.Context,
	name string,
	cfg config.InventoryProviderConfig,
	provider InventoryProvider,
	runner RemoteRunner,
	opts RefreshOptions,
) RefreshResult {
	if opts.Now.IsZero() {
		opts.Now = time.Now().UTC()
	}
	current, err := LoadProviderState(name)
	if err != nil {
		return RefreshResult{Provider: name, Err: err}
	}
	objects, err := provider.Discover(ctx, name, cfg, runner)
	if err != nil {
		if current != nil {
			next := cloneProviderState(current)
			next.LastError = err.Error()
			_ = SaveProviderState(next)
		}
		return RefreshResult{Provider: name, Err: err}
	}

	plan := Reconcile(objects, cfg.Route, name, current)
	allHosts := make(map[string]*ProviderHost)
	if current != nil {
		for id, host := range current.Objects {
			hostClone := *host
			allHosts[id] = &hostClone
		}
	}
	for _, host := range plan.Adds {
		host.ProviderType = cfg.Type
		allHosts[host.ObjectID] = host
	}
	for _, host := range plan.Updates {
		host.ProviderType = cfg.Type
		allHosts[host.ObjectID] = host
	}
	for _, host := range plan.Removals {
		delete(allHosts, host.ObjectID)
	}

	state := &ProviderState{
		Version:               StateVersion,
		Provider:              name,
		Type:                  cfg.Type,
		StrictHostKeyChecking: providerStrictHostKeyChecking(cfg),
		LastRefresh:           opts.Now.UTC(),
		IncludeFile:           ProviderIncludeFile(name),
		Objects:               allHosts,
	}
	if err := SaveProviderState(state); err != nil {
		return RefreshResult{Provider: name, Plan: plan, Err: err}
	}
	if opts.WriteSSHConfig {
		if len(allHosts) == 0 {
			if err := RemoveProviderSSHConfig(state.IncludeFile); err != nil {
				return RefreshResult{Provider: name, Plan: plan, Err: err}
			}
		} else if err := WriteProviderSSHConfig(state.IncludeFile, state.Hosts(), name, cfg.Type, state.StrictHostKeyChecking); err != nil {
			if current != nil {
				_ = SaveProviderState(current)
			} else {
				_ = DeleteProviderState(name)
			}
			return RefreshResult{Provider: name, Plan: plan, Err: err}
		}
	}
	return RefreshResult{Provider: name, Plan: plan}
}

func providerStrictHostKeyChecking(cfg config.InventoryProviderConfig) bool {
	if cfg.Type == config.ProviderContainerlab {
		return cfg.Config.StrictHostKeyChecking
	}
	return true
}

// ProviderIsStale reports whether normal inventory use may refresh a provider.
func ProviderIsStale(state *ProviderState, cfg config.InventoryProviderConfig, now time.Time) bool {
	interval := cfg.RefreshInterval.Duration()
	if state == nil {
		return interval > 0
	}
	if interval <= 0 {
		return false
	}
	if state.LastRefresh.IsZero() {
		return true
	}
	return now.Sub(state.LastRefresh) >= interval
}
