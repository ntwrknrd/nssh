package credential

import (
	"reflect"
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
)

func TestProviderInterfaceIsReadOnly(t *testing.T) {
	providerType := reflect.TypeOf((*Provider)(nil)).Elem()
	for _, method := range []string{"SetHost", "SetGroup", "RemoveHost", "RemoveGroup", "Status"} {
		if _, ok := providerType.MethodByName(method); ok {
			t.Fatalf("Provider should not expose %s after credential CLI removal", method)
		}
	}
}

func TestNewRegistryRejectsActiveAgeProvider(t *testing.T) {
	_, err := NewRegistry(&config.Config{
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
