package inv

import (
	"strings"
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/ui"
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

func TestRefreshResultStatusWarnsOnProviderErrors(t *testing.T) {
	got := refreshResultStatus(map[string]string{
		"netbox-prod": "error: api timeout",
	})
	if got != ui.StatusWarning {
		t.Fatalf("refreshResultStatus = %v, want warning", got)
	}
}

func TestRefreshResultStatusSucceedsWhenProvidersRefresh(t *testing.T) {
	got := refreshResultStatus(map[string]string{
		"netbox-prod": "ok (12 objects)",
	})
	if got != ui.StatusSuccess {
		t.Fatalf("refreshResultStatus = %v, want success", got)
	}
}
