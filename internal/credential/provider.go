// Package credential provides the SSH target credential provider abstraction
// used by connect-time credential resolution.
package credential

import (
	"fmt"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/secret"
)

// Record is one SSH target credential attached to a host or group.
type Record struct {
	Username string
	Secret   *secret.Secret
	Ref      string
}

// Provider resolves host/group SSH target credentials.
type Provider interface {
	GetHost(host string) (*Record, error)
	GetGroup(group string) (*Record, error)
	GetRef(ref config.CredentialRefConfig) (*Record, error)
}

// Registry holds configured credential provider instances by name.
type Registry struct {
	providers map[string]Provider
}

// NewRegistry constructs all named credential provider instances.
func NewRegistry(cfg *config.Config) (*Registry, error) {
	if cfg == nil {
		cfg = config.DefaultConfig()
	}
	credCfg := cfg.Credential
	if err := credCfg.Validate(); err != nil {
		return nil, err
	}

	registry := &Registry{
		providers: make(map[string]Provider, len(credCfg.Provider)),
	}
	for name, providerCfg := range credCfg.Provider {
		provider, err := buildNamedProvider(name, providerCfg, cfg)
		if err != nil {
			return nil, err
		}
		registry.providers[name] = provider
	}
	return registry, nil
}

// Provider returns a configured provider instance by name.
func (r *Registry) Provider(name string) Provider {
	if r == nil {
		return nil
	}
	return r.providers[name]
}

func buildNamedProvider(name string, providerCfg config.CredentialProviderConfig, cfg *config.Config) (Provider, error) {
	hostRefs := hostRefsForProvider(cfg.Inventory.Host, name)
	groupRefs := groupRefsForProvider(cfg.Inventory.Provider, name)
	switch providerCfg.Type {
	case config.CredentialProviderPass:
		provider := newPassProvider(providerCfg).(*passProvider)
		provider.hostRefs = hostRefs
		provider.groupRefs = groupRefs
		return provider, nil
	case config.CredentialProvider1Password:
		provider := newOnePasswordProviderNamed(name, config.CredentialConfig{
			Config: providerCfg.Config,
			Host:   hostRefs,
			Group:  groupRefs,
		}).(*onePasswordProvider)
		provider.autoStartAgent = cfg.Agent.AutoStart
		return provider, nil
	case config.CredentialProviderBitwarden:
		provider := newBitwardenProvider(providerCfg).(*bitwardenProvider)
		provider.hostRefs = hostRefs
		provider.groupRefs = groupRefs
		return provider, nil
	default:
		return nil, fmt.Errorf("unsupported credential provider %q", providerCfg.Type)
	}
}

func hostRefsForProvider(refs map[string]config.InventoryHostConfig, providerName string) map[string]config.CredentialRefConfig {
	if len(refs) == 0 {
		return nil
	}
	filtered := make(map[string]config.CredentialRefConfig)
	for name, ref := range refs {
		auth := ref.Auth
		if !auth.IsSet() {
			continue
		}
		auth.Normalize()
		if auth.CredentialProvider == providerName {
			filtered[name] = auth.CredentialRef()
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

func groupRefsForProvider(refs map[string]config.InventoryProviderConfig, providerName string) map[string]config.CredentialRefConfig {
	if len(refs) == 0 {
		return nil
	}
	filtered := make(map[string]config.CredentialRefConfig)
	for inventoryProvider, providerCfg := range refs {
		for group, ref := range providerCfg.Group {
			auth := ref.Auth
			if !auth.IsSet() {
				continue
			}
			auth.Normalize()
			if auth.CredentialProvider == providerName {
				filtered[config.FormatInventoryGroupID(inventoryProvider, group)] = auth.CredentialRef()
			}
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}
