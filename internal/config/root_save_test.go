package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveSparseWritesYAMLIncludesAndProviderHosts(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.yaml")
	cfg := DefaultConfig()
	cfg.Include = []string{"credentials/*.yaml", "inventory/*.yaml"}
	cfg.Inventory.Provider = nil
	cfg.Inventory.Providers = map[string]InventoryProviderConfig{
		ProviderLocal: {
			Type: ProviderLocal,
			Groups: map[string]GroupConfig{
				"homelab": {Auth: InventoryAuthConfig{Mode: AuthModeKey, Username: "cj"}},
			},
			Hosts: map[string]InventoryHostConfig{
				"rpi-a": {Group: "homelab", Hostname: "rpi-a.lan"},
			},
		},
	}
	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	text := string(data)
	for _, want := range []string{
		"include:",
		"providers:",
		"local:",
		"hosts:",
		"rpi-a:",
		"group: homelab",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("saved config missing %q:\n%s", want, text)
		}
	}
}
