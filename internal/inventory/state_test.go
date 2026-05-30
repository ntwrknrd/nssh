package inventory

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func setupTestStateDir(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	SetStateDir(tmp)
	t.Cleanup(func() { SetStateDir("") })
	return tmp
}

func TestProviderStateRoundTripUsesInventoryPath(t *testing.T) {
	tmp := setupTestStateDir(t)

	state := &ProviderState{
		Version:     StateVersion,
		Provider:    "netbox-prod",
		Type:        "netbox",
		LastRefresh: time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC),
		IncludeFile: ProviderIncludeFile("netbox-prod"),
		Objects: map[string]*ProviderHost{
			"device:1": {
				ObjectID: "device:1",
				Host:     "edge01",
				Patterns: []string{"edge01"},
				Group:    "customer",
				HostName: "edge01.customer.local",
			},
		},
	}
	if err := SaveProviderState(state); err != nil {
		t.Fatalf("save: %v", err)
	}

	wantPath := filepath.Join(tmp, "inventory", "providers", "netbox-prod.json")
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("expected state at %s: %v", wantPath, err)
	}

	loaded, err := LoadProviderState("netbox-prod")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Provider != "netbox-prod" {
		t.Fatalf("provider = %q", loaded.Provider)
	}
	if loaded.Objects["device:1"].Group != "customer" {
		t.Fatalf("group = %q", loaded.Objects["device:1"].Group)
	}
}

func TestProviderIncludeFileUsesNSSHDirectory(t *testing.T) {
	if got := ProviderIncludeFile("netbox-prod"); got != filepath.Join("nssh.d", "provider_netbox-prod.conf") {
		t.Fatalf("include file = %q", got)
	}
}
