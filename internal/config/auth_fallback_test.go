package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAuthFallbackInheritanceAndSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	text := `inventory:
  auth:
    fallback: fail
    lookup_timeout: 3s
  providers:
    local:
      type: local
      groups:
        lab:
          auth:
            lookup_timeout: 2s
      hosts:
        edge:
          group: lab
          auth:
            fallback: manual
`
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	auth := cfg.ResolveInventoryAuth(InventoryAuthContext{Host: "edge", Provider: "local", Group: "lab"})
	if auth.Fallback != AuthFallbackManual || auth.LookupTimeout.Duration() != 2*time.Second {
		t.Fatalf("auth=%+v", auth)
	}
	other := cfg.ResolveInventoryAuth(InventoryAuthContext{Host: "other", Provider: "local"})
	if other.Fallback != AuthFallbackFail || other.LookupTimeout.Duration() != 3*time.Second {
		t.Fatalf("auth=%+v", other)
	}
	// Inventory edits and a full save must retain inherited and host policy.
	provider := cfg.Inventory.Provider[ProviderLocal]
	host := provider.Hosts["edge"]
	host.Auth.LookupTimeout = Duration(time.Second)
	provider.Hosts["edge"] = host
	cfg.Inventory.Provider[ProviderLocal] = provider
	if err := SaveInventoryProviderHost(path, cfg, "local", "edge"); err != nil {
		t.Fatal(err)
	}
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	actual := reloaded.ResolveInventoryAuth(InventoryAuthContext{Host: "edge", Provider: "local", Group: "lab"})
	if actual.Fallback != AuthFallbackManual || actual.LookupTimeout.Duration() != time.Second {
		t.Fatalf("saved auth=%+v", actual)
	}
	inherited := reloaded.ResolveInventoryAuth(InventoryAuthContext{Host: "other", Provider: "local"})
	if inherited.Fallback != AuthFallbackFail || inherited.LookupTimeout.Duration() != 3*time.Second {
		t.Fatalf("saved global auth=%+v", inherited)
	}
}

func TestAuthFallbackDefaultsAndValidation(t *testing.T) {
	cfg := DefaultConfig()
	auth := cfg.ResolveInventoryAuth(InventoryAuthContext{})
	if auth.Fallback != AuthFallbackManual || auth.LookupTimeout.Duration() != DefaultAuthLookupTimeout {
		t.Fatalf("auth=%+v", auth)
	}
	for _, invalid := range []InventoryAuthConfig{{Fallback: "prompt-forever"}, {LookupTimeout: Duration(-time.Second)}} {
		if err := invalid.Validate("inventory.auth"); err == nil {
			t.Fatalf("accepted %+v", invalid)
		}
	}
	valid := InventoryAuthConfig{Fallback: "FAIL"}
	if err := valid.Validate("inventory.auth"); err != nil {
		t.Fatal(err)
	}
}
