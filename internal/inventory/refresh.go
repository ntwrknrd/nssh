package inventory

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ntwrknrd/nssh/internal/config"
)

// RefreshOptions controls provider refresh side effects.
type RefreshOptions struct {
	Now time.Time
}

// RefreshResult reports one provider refresh outcome.
type RefreshResult struct {
	Provider string
	Plan     *Plan
	Err      error
}

// RefreshProvider discovers, reconciles, and saves provider state. It never
// touches credentials or generated SSH config.
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
		if !IsUnsupportedStateVersion(err) {
			return RefreshResult{Provider: name, Err: err}
		}
		current = nil
	}
	selectors := config.InventoryConfig{Provider: map[string]config.InventoryProviderConfig{name: cfg}}.ProviderSelectors(name)
	discoverCfg := cfg
	discoverCfg.Selectors = selectors
	objects, err := provider.Discover(ctx, name, discoverCfg, runner)
	if err != nil {
		if current != nil {
			next := cloneProviderState(current)
			next.LastError = err.Error()
			_ = SaveProviderState(next)
		}
		return RefreshResult{Provider: name, Err: err}
	}

	plan := Reconcile(objects, selectors, name, current, cfg.Group)
	if len(plan.Conflicts) > 0 {
		return RefreshResult{Provider: name, Plan: plan, Err: selectorConflictError(name, plan.Conflicts)}
	}
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
	return RefreshResult{Provider: name, Plan: plan}
}

func selectorConflictError(provider string, conflicts []GroupConflict) error {
	if len(conflicts) == 0 {
		return nil
	}
	conflict := conflicts[0]
	return fmt.Errorf("provider %s object %q matched multiple groups: %s", provider, conflict.Object.ObjectID, strings.Join(conflict.Groups, ", "))
}

func providerStrictHostKeyChecking(cfg config.InventoryProviderConfig) bool {
	if cfg.Type == config.ProviderContainerlab {
		return cfg.Config.StrictHostKeyChecking
	}
	return true
}
