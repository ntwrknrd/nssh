package credential

import (
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
)

func TestNewRegistryBuildsNamedProviders(t *testing.T) {
	registry, err := NewRegistry(&config.Config{
		Credential: config.CredentialConfig{
			Provider: map[string]config.CredentialProviderConfig{
				"pass-local": {Type: config.CredentialProviderPass},
				"op-network": {Type: config.CredentialProvider1Password, Config: config.CredentialProviderDetailConfig{Vault: "Network"}},
			},
		},
		Inventory: config.InventoryConfig{
			Provider: map[string]config.InventoryProviderConfig{
				"local": {Type: config.ProviderLocal, Group: map[string]config.GroupConfig{
					"default": {Auth: config.InventoryAuthConfig{CredentialProvider: "pass-local", PasswordRef: "nssh/groups/default"}},
					"prod":    {Auth: config.InventoryAuthConfig{CredentialProvider: "op-network", PasswordRef: "Network Shared Admin"}},
				}},
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
	if op.groupRefs["local/prod"].Ref != "Network Shared Admin" {
		t.Fatalf("op-network prod ref = %+v", op.groupRefs["local/prod"])
	}
}
