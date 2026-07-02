package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSaveSparseWritesYAMLIncludesAndProviderHosts(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.yaml")
	cfg := DefaultConfig()
	highlightEnabled := true
	highlightDisabled := false
	cfg.Highlight = HighlightConfig{Profile: HighlightProfileNone}
	cfg.Include = []string{"credential/*.yaml", "inventory/*.yaml"}
	cfg.Inventory.Provider = nil
	cfg.Inventory.Providers = map[string]InventoryProviderConfig{
		ProviderLocal: {
			Type: ProviderLocal,
			Groups: map[string]GroupConfig{
				"homelab": {
					Auth:      InventoryAuthConfig{Mode: AuthModeKey, Username: "cj"},
					SSH:       SSHHostConfig{Options: SSHOptions{"ProxyJump": NewSSHOptionString("bastion")}},
					Highlight: HighlightConfig{Enabled: &highlightEnabled, Profile: HighlightProfileJunos},
				},
			},
			Hosts: map[string]InventoryHostConfig{
				"rpi-a.lan": {Group: "homelab", Aliases: []string{"rpi-a"}, Highlight: HighlightConfig{Enabled: &highlightDisabled}},
			},
		},
		"nre-netlab01": {
			Type: ProviderContainerlab,
			Config: InventoryProviderDetailConfig{
				JumpHost:    "nre@nre-netlab01.example.com",
				SSHDefaults: NewSSHDefaultsInheritanceOptions("SetEnv"),
			},
			Groups: map[string]GroupConfig{"vjunos": {}},
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
		"highlight:",
		"profile: none",
		"profile: junos",
		"enabled: false",
		"ssh_defaults:",
		"- SetEnv",
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

func TestMarshalSparseWritesArchiveTimeoutAndOmitsSchedulerFields(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Logging.Session.Archive.Timeout = Duration(45 * time.Second)

	got, err := MarshalSparse(cfg)
	if err != nil {
		t.Fatalf("MarshalSparse: %v", err)
	}
	if !strings.Contains(got, "timeout: 45s") {
		t.Fatalf("sparse config missing archive timeout:\n%s", got)
	}
	for _, reject := range []string{"archive:\n        enabled:", "archive:\n        jitter:", "archive:\n        min_interval:", "jitter:", "min_interval:"} {
		if strings.Contains(got, reject) {
			t.Fatalf("sparse config should omit obsolete archive field %q:\n%s", reject, got)
		}
	}
}

func TestMarshalSparseWritesProviderRequestTimeout(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Agent.ProviderRequestTimeout = Duration(90 * time.Second)

	got, err := MarshalSparse(cfg)
	if err != nil {
		t.Fatalf("MarshalSparse: %v", err)
	}
	if !strings.Contains(got, "provider_request_timeout: 1m30s") {
		t.Fatalf("sparse config missing provider_request_timeout:\n%s", got)
	}
}
