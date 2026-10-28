package connect

import (
	"sort"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/inventory"
)

// Provider-host rules shared by the connect catalog and inventory commands so
// both see one host per name:
//   - A host in a provider's YAML hosts table overlays the provider object it
//     names; it is not a second host.
//   - A local inventory host shadows a provider object with the same name.
//     Local inventory is operator-owned; provider state is a refreshable cache.

// ProviderHostFinding describes a provider entry that does not appear as its
// own host.
type ProviderHostFinding struct {
	Provider string
	Host     string
	// ShadowedBy names the local host that replaces this provider object.
	ShadowedBy string
	// Orphaned reports a provider YAML host override with no provider object.
	Orphaned bool
}

// ResolveProviderStateHost resolves a provider object with its YAML overlay,
// or returns nil when a local inventory host shadows it.
func ResolveProviderStateHost(cfg *config.Config, state *inventory.ProviderState, host *inventory.ProviderHost) *ResolvedHostData {
	if cfg == nil || state == nil || host == nil {
		return nil
	}
	provider := normalizeCatalogProvider(catalogProviders(cfg)[state.Provider])
	data := resolvedHostFromState(cfg, state, provider, host)
	if _, shadowed := shadowingLocalHost(localHostNames(cfg), data); shadowed {
		return nil
	}
	return data
}

// ProviderHostFindings lists provider objects shadowed by local hosts and
// provider YAML host overrides that match no provider object.
func ProviderHostFindings(cfg *config.Config, states []*inventory.ProviderState) []ProviderHostFinding {
	if cfg == nil {
		return nil
	}
	providers := catalogProviders(cfg)
	local := localHostNames(cfg)
	var findings []ProviderHostFinding
	for _, state := range states {
		if state == nil {
			continue
		}
		providerCfg, ok := providers[state.Provider]
		if !ok {
			continue
		}
		provider := normalizeCatalogProvider(providerCfg)
		matched := make(map[string]bool)
		for _, host := range state.Objects {
			if host == nil {
				continue
			}
			if key, ok := providerOverlayKey(provider, host); ok {
				matched[key] = true
			}
			data := resolvedHostFromState(cfg, state, provider, host)
			if owner, shadowed := shadowingLocalHost(local, data); shadowed {
				findings = append(findings, ProviderHostFinding{Provider: state.Provider, Host: data.Canonical, ShadowedBy: owner})
			}
		}
		for name := range provider.Hosts {
			if !matched[name] {
				findings = append(findings, ProviderHostFinding{Provider: state.Provider, Host: name, Orphaned: true})
			}
		}
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Provider != findings[j].Provider {
			return findings[i].Provider < findings[j].Provider
		}
		return findings[i].Host < findings[j].Host
	})
	return findings
}

// providerOverlayKey returns the provider YAML host that overlays a provider
// object: a grouped entry by address first, then by host name, then by address.
func providerOverlayKey(provider config.InventoryProviderConfig, host *inventory.ProviderHost) (string, bool) {
	if overlay, ok := provider.Hosts[host.HostName]; ok && overlay.Group != "" {
		return host.HostName, true
	}
	if _, ok := provider.Hosts[host.Host]; ok {
		return host.Host, true
	}
	if _, ok := provider.Hosts[host.HostName]; ok {
		return host.HostName, true
	}
	return "", false
}

func catalogProviders(cfg *config.Config) map[string]config.InventoryProviderConfig {
	if len(cfg.Inventory.Providers) == 0 {
		return cfg.Inventory.Provider
	}
	return cfg.Inventory.Providers
}

func localHostNames(cfg *config.Config) map[string]bool {
	names := make(map[string]bool)
	for _, provider := range catalogProviders(cfg) {
		if normalizeCatalogProvider(provider).Type != config.ProviderLocal {
			continue
		}
		for name := range provider.Hosts {
			names[name] = true
		}
	}
	return names
}

func shadowingLocalHost(local map[string]bool, data *ResolvedHostData) (string, bool) {
	if data == nil {
		return "", false
	}
	for _, name := range []string{data.Canonical, data.Hostname} {
		if name != "" && local[name] {
			return name, true
		}
	}
	return "", false
}
