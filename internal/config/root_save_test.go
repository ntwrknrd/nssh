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
	cfg.Include = []string{"credential/*.yaml", "inventory/*.yaml"}
	cfg.Inventory.Provider = nil
	cfg.Inventory.Providers = map[string]InventoryProviderConfig{
		ProviderLocal: {
			Type: ProviderLocal,
			Groups: map[string]GroupConfig{
				"homelab": {
					Auth: InventoryAuthConfig{Mode: AuthModeKey, Username: "cj"},
					SSH:  SSHHostConfig{Options: SSHOptions{"ProxyJump": NewSSHOptionString("bastion")}},
				},
			},
			Hosts: map[string]InventoryHostConfig{
				"rpi-a.lan": {Group: "homelab", Aliases: []string{"rpi-a"}},
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
		"rpi-a.lan:",
		"- rpi-a",
		"group: homelab",
		"ProxyJump: bastion",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("saved config missing %q:\n%s", want, text)
		}
	}
}

func TestSaveInventoryProviderHostWritesProviderScopedHost(t *testing.T) {
	tmp := t.TempDir()
	rootPath := filepath.Join(tmp, "config.yaml")
	inventoryDir := filepath.Join(tmp, "inventory")
	providerPath := filepath.Join(inventoryDir, "netbox-prod.yaml")
	if err := os.MkdirAll(inventoryDir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeConfigFile(t, rootPath, `
include:
  - inventory/*.yaml
`)
	writeConfigFile(t, providerPath, `
inventory:
  providers:
    netbox-prod:
      type: netbox
      groups:
        cbb:
          match:
            domain_suffix: [.expedient.com]
      hosts:
        701-sw37r103c608.expedient.com:
          group: cbb
          aliases: [701-sw37]
`)
	cfg, err := Load(rootPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	provider := cfg.Inventory.Providers["netbox-prod"]
	host := provider.Hosts["701-sw37r103c608.expedient.com"]
	host.SSH.Compatibility.Kex = "diffie-hellman-group14-sha1"
	provider.Hosts["701-sw37r103c608.expedient.com"] = host
	cfg.Inventory.Providers["netbox-prod"] = provider
	cfg.Inventory.Provider["netbox-prod"] = provider

	if err := SaveInventoryProviderHost(rootPath, cfg, "netbox-prod", "701-sw37r103c608.expedient.com"); err != nil {
		t.Fatalf("SaveInventoryProviderHost: %v", err)
	}
	data, err := os.ReadFile(providerPath)
	if err != nil {
		t.Fatalf("read provider: %v", err)
	}
	text := string(data)
	for _, want := range []string{
		"hosts:",
		"701-sw37r103c608.expedient.com:",
		"group: cbb",
		"aliases:",
		"compatibility:",
		"kex: diffie-hellman-group14-sha1",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("saved provider missing %q:\n%s", want, text)
		}
	}
}
