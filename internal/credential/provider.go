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

// Status describes the active credential provider backend.
type Status struct {
	Type      string
	Available bool
	Detail    string
}

// Capabilities describes provider behavior that callers can depend on without
// knowing provider internals.
type Capabilities struct {
	ProviderSessionPolicy       string
	SupportsHostCRUD            bool
	SupportsGroupCRUD           bool
	SupportsSecretRefs          bool
	SupportsStatusCheck         bool
	SupportsResolvedSecretCache bool
}

// Provider stores and retrieves host/group SSH target credentials.
type Provider interface {
	GetHost(host string) (*Record, error)
	SetHost(host string, record *Record) error
	RemoveHost(host string) (bool, error)
	GetGroup(group string) (*Record, error)
	SetGroup(group string, record *Record) error
	RemoveGroup(group string) (bool, error)
	Status() Status
}

// CapabilityProvider marks providers that expose explicit behavior metadata.
type CapabilityProvider interface {
	Capabilities() Capabilities
}

// HostCredentialIndex marks providers that can cheaply tell whether a host
// override relationship exists without probing the backing secret manager.
type HostCredentialIndex interface {
	HasHostCredential(host string) bool
}

// Registry holds configured credential provider instances by name.
type Registry struct {
	defaultProvider string
	providers       map[string]Provider
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
		defaultProvider: credCfg.DefaultProvider,
		providers:       make(map[string]Provider, len(credCfg.Provider)),
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

// DefaultProviderName returns the configured default provider instance name.
func (r *Registry) DefaultProviderName() string {
	if r == nil {
		return ""
	}
	return r.defaultProvider
}

// DefaultProvider returns the configured default provider instance.
func (r *Registry) DefaultProvider() Provider {
	if r == nil {
		return nil
	}
	return r.providers[r.defaultProvider]
}

// NewProvider constructs the configured default credential provider.
func NewProvider(cfg *config.Config) (Provider, error) {
	registry, err := NewRegistry(cfg)
	if err != nil {
		return nil, err
	}
	provider := registry.DefaultProvider()
	if provider == nil {
		return nil, fmt.Errorf("credential.default_provider references unknown provider %q", registry.DefaultProviderName())
	}
	return provider, nil
}

func buildNamedProvider(name string, providerCfg config.CredentialProviderConfig, cfg *config.Config) (Provider, error) {
	credCfg := cfg.Credential
	hostRefs := hostRefsForProvider(cfg.Inventory.Host, name, credCfg.DefaultProvider)
	groupRefs := groupRefsForProvider(cfg.Inventory.Group, name, credCfg.DefaultProvider)
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

func hostRefsForProvider(refs map[string]config.InventoryHostConfig, providerName, defaultProvider string) map[string]config.CredentialRefConfig {
	if len(refs) == 0 {
		return nil
	}
	filtered := make(map[string]config.CredentialRefConfig)
	for name, ref := range refs {
		auth := ref.Auth
		if !auth.IsSet() {
			continue
		}
		refProvider := auth.Provider
		if refProvider == "" {
			refProvider = defaultProvider
		}
		if refProvider == providerName {
			filtered[name] = auth.CredentialRef()
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

func groupRefsForProvider(refs map[string]config.GroupConfig, providerName, defaultProvider string) map[string]config.CredentialRefConfig {
	if len(refs) == 0 {
		return nil
	}
	filtered := make(map[string]config.CredentialRefConfig)
	for name, ref := range refs {
		auth := ref.Auth
		if !auth.IsSet() {
			continue
		}
		refProvider := auth.Provider
		if refProvider == "" {
			refProvider = defaultProvider
		}
		if refProvider == providerName {
			filtered[name] = auth.CredentialRef()
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}
