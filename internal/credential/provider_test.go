package credential

import (
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
)

func TestNewProviderUsesDefaultNamedProvider(t *testing.T) {
	provider, err := NewProvider(config.DefaultConfig())
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	if _, ok := provider.(*passProvider); !ok {
		t.Fatalf("provider = %T, want *passProvider", provider)
	}
}

func TestNewProviderRejectsActiveAgeProvider(t *testing.T) {
	_, err := NewProvider(&config.Config{
		Credential: config.CredentialConfig{
			DefaultProvider: "local-age",
			Provider: map[string]config.CredentialProviderConfig{
				"local-age": {Type: "age"},
			},
		},
	})
	if err == nil {
		t.Fatal("expected age provider rejection")
	}
}

func TestNewRegistryBuildsNamedProviders(t *testing.T) {
	registry, err := NewRegistry(&config.Config{
		Credential: config.CredentialConfig{
			DefaultProvider: "pass-local",
			Provider: map[string]config.CredentialProviderConfig{
				"pass-local": {Type: config.CredentialProviderPass},
				"op-network": {Type: config.CredentialProvider1Password, Config: config.CredentialProviderDetailConfig{Vault: "Network"}},
			},
		},
		Inventory: config.InventoryConfig{
			Group: map[string]config.GroupConfig{
				"default": {Auth: config.InventoryAuthConfig{Provider: "pass-local", Ref: "nssh/groups/default"}},
				"prod":    {Auth: config.InventoryAuthConfig{Provider: "op-network", Ref: "Network Shared Admin"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if _, ok := registry.Provider("pass-local").(*passProvider); !ok {
		t.Fatalf("pass-local = %T", registry.Provider("pass-local"))
	}
	op, ok := registry.Provider("op-network").(*onePasswordProvider)
	if !ok {
		t.Fatalf("op-network = %T", registry.Provider("op-network"))
	}
	if op.groupRefs["prod"].Ref != "Network Shared Admin" {
		t.Fatalf("op-network prod ref = %+v", op.groupRefs["prod"])
	}
	if registry.DefaultProviderName() != "pass-local" {
		t.Fatalf("default provider = %q", registry.DefaultProviderName())
	}
}

func TestProviderSessionCapabilities(t *testing.T) {
	tests := []struct {
		name     string
		provider Provider
		want     string
	}{
		{name: "pass external", provider: &passProvider{}, want: config.ProviderSessionExternal},
		{name: "1password agent", provider: &onePasswordProvider{}, want: config.ProviderSessionAgentOwned},
		{name: "bitwarden external", provider: &bitwardenProvider{}, want: config.ProviderSessionExternal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capable, ok := tt.provider.(CapabilityProvider)
			if !ok {
				t.Fatalf("%T does not expose capabilities", tt.provider)
			}
			caps := capable.Capabilities()
			if caps.ProviderSessionPolicy != tt.want {
				t.Fatalf("session policy = %q, want %q", caps.ProviderSessionPolicy, tt.want)
			}
			if caps.SupportsResolvedSecretCache {
				t.Fatalf("%T advertises resolved secret caching", tt.provider)
			}
		})
	}
}
