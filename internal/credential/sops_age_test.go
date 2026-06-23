package credential

import (
	"testing"

	"github.com/ntwrknrd/nssh/internal/agent"
	"github.com/ntwrknrd/nssh/internal/config"
)

func TestSOPSAgeProviderGetRefUsesAgent(t *testing.T) {
	client := stubProviderAgent(t, &agent.ProviderResponse{
		Found:    true,
		Username: "cj",
		Secret:   []byte("secret"),
		Ref:      "expedient.password",
	})

	provider := &sopsAgeProvider{name: "sops"}
	got, err := provider.GetRef(config.CredentialRefConfig{
		Ref:         "expedient.password",
		UsernameRef: "expedient.username",
	})
	if err != nil {
		t.Fatalf("GetRef: %v", err)
	}
	if got == nil || got.Username != "cj" || revealTestSecret(t, got) != "secret" || got.Ref != "expedient.password" {
		t.Fatalf("record = %+v", got)
	}
	if len(client.reqs) != 1 {
		t.Fatalf("agent requests = %+v", client.reqs)
	}
	req := client.reqs[0]
	if req.Provider != "sops" || req.Action != "get" || req.Ref != "expedient.password" || req.UsernameRef != "expedient.username" {
		t.Fatalf("agent request = %+v", req)
	}
}

func TestSOPSAgeProviderMissingRefReturnsNil(t *testing.T) {
	stubProviderAgent(t, &agent.ProviderResponse{Found: false})
	provider := &sopsAgeProvider{name: "sops"}

	got, err := provider.GetRef(config.CredentialRefConfig{Ref: "expedient.missing"})
	if err != nil {
		t.Fatalf("GetRef: %v", err)
	}
	if got != nil {
		t.Fatalf("record = %+v, want nil", got)
	}
}

func TestSOPSAgeProviderEmptyRefDoesNotCallAgent(t *testing.T) {
	client := stubProviderAgent(t, &agent.ProviderResponse{Found: true})
	provider := &sopsAgeProvider{name: "sops"}

	got, err := provider.GetRef(config.CredentialRefConfig{})
	if err != nil {
		t.Fatalf("GetRef: %v", err)
	}
	if got != nil {
		t.Fatalf("record = %+v, want nil", got)
	}
	if len(client.reqs) != 0 {
		t.Fatalf("requests = %d, want 0", len(client.reqs))
	}
}

func TestNewRegistryBuildsSOPSAgeProvider(t *testing.T) {
	registry, err := NewRegistry(&config.Config{
		Credential: config.CredentialConfig{
			Provider: map[string]config.CredentialProviderConfig{
				"sops":       {Type: config.CredentialProviderSOPSAge, File: "/tmp/credentials.sops.yaml"},
				"op-network": {Type: config.CredentialProvider1Password, Config: config.CredentialProviderDetailConfig{Vault: "Network"}},
			},
		},
		Inventory: config.InventoryConfig{
			Provider: map[string]config.InventoryProviderConfig{
				"local": {Type: config.ProviderLocal, Group: map[string]config.GroupConfig{
					"default": {Auth: config.InventoryAuthConfig{CredentialProvider: "sops", PasswordRef: "groups.default.password"}},
					"prod":    {Auth: config.InventoryAuthConfig{CredentialProvider: "op-network", PasswordRef: "Network Shared Admin"}},
				}},
			},
		},
	})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	sops, ok := registry.Provider("sops").(*sopsAgeProvider)
	if !ok {
		t.Fatalf("sops = %T", registry.Provider("sops"))
	}
	if sops.groupRefs["local/default"].Ref != "groups.default.password" {
		t.Fatalf("sops default ref = %+v", sops.groupRefs["local/default"])
	}
	op, ok := registry.Provider("op-network").(*onePasswordProvider)
	if !ok {
		t.Fatalf("op-network = %T", registry.Provider("op-network"))
	}
	if op.groupRefs["local/prod"].Ref != "Network Shared Admin" {
		t.Fatalf("op-network prod ref = %+v", op.groupRefs["local/prod"])
	}
}

func TestCredentialRegistryRejectsPass(t *testing.T) {
	_, err := NewRegistry(&config.Config{
		Credential: config.CredentialConfig{
			Provider: map[string]config.CredentialProviderConfig{
				"pass": {Type: "pass"},
			},
		},
	})
	if err == nil {
		t.Fatal("expected pass rejection")
	}
}
