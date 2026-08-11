package inv

import (
	"strings"
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
)

func TestRefreshTargetRejectsUnknownProvider(t *testing.T) {
	cfg := &config.Config{Inventory: config.InventoryConfig{
		Provider: map[string]config.InventoryProviderConfig{
			"netbox-prod": {Type: config.ProviderNetBox},
		},
	}}

	err := validateRefreshTarget(cfg, "missing-provider")
	if err == nil {
		t.Fatal("missing provider target succeeded unexpectedly")
	}
	if !strings.Contains(err.Error(), `refresh target "missing-provider" is not "local" or a configured provider`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRefreshTargetAcceptsLocalAndConfiguredProvider(t *testing.T) {
	cfg := &config.Config{Inventory: config.InventoryConfig{
		Provider: map[string]config.InventoryProviderConfig{
			"netbox-prod": {Type: config.ProviderNetBox},
		},
	}}

	for _, target := range []string{"", "local", "netbox-prod"} {
		if err := validateRefreshTarget(cfg, target); err != nil {
			t.Fatalf("validateRefreshTarget(%q): %v", target, err)
		}
	}
}

func TestRefreshProviderCachesReportsConfiguredProviderNames(t *testing.T) {
	cfg := &config.Config{Inventory: config.InventoryConfig{
		Provider: map[string]config.InventoryProviderConfig{
			"local":       {Type: config.ProviderLocal},
			"netbox-prod": {Type: "unsupported"},
		},
	}}

	var statuses []string
	results := refreshProviderCachesWithProgress(cfg, "", func(status string) {
		statuses = append(statuses, status)
	})

	if got := statuses; len(got) != 1 || got[0] != "Refreshing netbox-prod" {
		t.Fatalf("statuses = %#v, want %#v", got, []string{"Refreshing netbox-prod"})
	}
	if result := results["netbox-prod"]; !strings.HasPrefix(result, "error: unsupported provider") {
		t.Fatalf("netbox-prod result = %q, want unsupported provider error", result)
	}
}
