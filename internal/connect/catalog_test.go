package connect

import (
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/inventory"
)

func TestCatalogUsesProviderHostsAsOverlays(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Inventory.Provider = nil
	cfg.Inventory.Providers = map[string]config.InventoryProviderConfig{
		"netbox-prod": {
			Type: config.ProviderNetBox,
			Groups: map[string]config.GroupConfig{
				"cbb": {Auth: config.InventoryAuthConfig{Mode: config.AuthModePassword, Username: "chris.jones"}},
			},
			Hosts: map[string]config.InventoryHostConfig{
				"edge01.example.com": {Group: "cbb", Aliases: []string{"edge01"}},
			},
		},
	}
	state := &inventory.ProviderState{
		Provider: "netbox-prod",
		Type:     config.ProviderNetBox,
		Objects: map[string]*inventory.ProviderHost{
			"1": {ObjectID: "1", Host: "edge01.example.com", HostName: "edge01.example.com", Group: "netbox-prod/cbb"},
		},
	}
	cat := buildCatalogForTest(t, cfg, []*inventory.ProviderState{state})
	host, ok := cat.Find("edge01")
	if !ok {
		t.Fatalf("Find(edge01) failed")
	}
	if host.Provider != "netbox-prod" || host.Group != "cbb" || host.Username != "chris.jones" {
		t.Fatalf("host = %#v", host)
	}
}

func TestCatalogRequiresLocalHostGroup(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Inventory.Provider = nil
	cfg.Inventory.Providers = map[string]config.InventoryProviderConfig{
		config.ProviderLocal: {
			Type: config.ProviderLocal,
			Hosts: map[string]config.InventoryHostConfig{
				"rpi-a": {Hostname: "rpi-a.lan"},
			},
		},
	}
	_, err := BuildHostCatalog(cfg)
	if err == nil {
		t.Fatalf("BuildHostCatalog succeeded without local host group")
	}
}

func buildCatalogForTest(t *testing.T, cfg *config.Config, states []*inventory.ProviderState) *HostCatalog {
	t.Helper()
	cat, err := buildHostCatalog(cfg, states)
	if err != nil {
		t.Fatalf("buildHostCatalog: %v", err)
	}
	return cat
}
