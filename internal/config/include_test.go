package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadYAMLIncludesInOrder(t *testing.T) {
	tmp := t.TempDir()
	writeConfigFile(t, filepath.Join(tmp, "credentials", "op.yaml"), `
credentials:
  op-expedient:
    type: 1password
    session: agent
    vault: Expedient
`)
	writeConfigFile(t, filepath.Join(tmp, "inventory", "local.yaml"), `
inventory:
  providers:
    local:
      type: local
      groups:
        homelab:
          auth:
            mode: key
            username: cj
      hosts:
        rpi-a.lan:
          group: homelab
          aliases:
            - rpi-a
`)
	root := filepath.Join(tmp, "config.yaml")
	writeConfigFile(t, root, `include: [credentials/*.yaml, inventory/*.yaml]`)

	cfg, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := cfg.Credentials["op-expedient"]; !ok {
		t.Fatalf("missing credential provider")
	}
	if got := cfg.Inventory.Providers["local"].Hosts["rpi-a.lan"].Group; got != "homelab" {
		t.Fatalf("local host group = %q, want homelab", got)
	}
}

func TestLoadYAMLRejectsUnknownKeys(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "config.yaml")
	writeConfigFile(t, root, `
ssh:
  defaults:
    not_a_real_key: true
`)
	_, err := Load(root)
	if err == nil || !strings.Contains(err.Error(), "not_a_real_key") {
		t.Fatalf("Load error = %v, want unknown key error", err)
	}
}

func TestLoadYAMLIncludeCycle(t *testing.T) {
	tmp := t.TempDir()
	writeConfigFile(t, filepath.Join(tmp, "a.yaml"), `include: [b.yaml]`)
	writeConfigFile(t, filepath.Join(tmp, "b.yaml"), `include: [a.yaml]`)
	_, err := Load(filepath.Join(tmp, "a.yaml"))
	if err == nil || !strings.Contains(err.Error(), "include cycle") {
		t.Fatalf("Load error = %v, want include cycle", err)
	}
}

func TestLoadYAMLIncludeGlobOrderAndArrayReplacement(t *testing.T) {
	tmp := t.TempDir()
	writeConfigFile(t, filepath.Join(tmp, "conf.d", "01-base.yaml"), `
inventory:
  providers:
    netbox-prod:
      type: netbox
      groups:
        default:
          match:
            role: [router]
`)
	writeConfigFile(t, filepath.Join(tmp, "conf.d", "02-override.yaml"), `
inventory:
  providers:
    netbox-prod:
      groups:
        default:
          match:
            role: [switch]
`)
	mainPath := filepath.Join(tmp, "config.yaml")
	writeConfigFile(t, mainPath, `include: [conf.d/*.yaml]`)

	cfg, err := Load(mainPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	selectors := cfg.Inventory.ProviderSelectors("netbox-prod")
	if len(selectors) != 1 {
		t.Fatalf("selector count = %d, want one selector: %+v", len(selectors), selectors)
	}
	if selectors[0].Group != "netbox-prod/default" || selectors[0].Match["role"][0] != "switch" {
		t.Fatalf("selector = %+v", selectors[0])
	}
}

func TestMarshalSparseWritesYAML(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Include = []string{"credentials/*.yaml", "inventory/*.yaml"}
	cfg.Agent.IdleTimeout = Duration(4 * time.Hour)
	cfg.Agent.ActivityIncrement = Duration(30 * time.Minute)
	cfg.Agent.MaxLifetime = Duration(8 * time.Hour)
	cfg.Logging.Session.Enabled = boolPtr(true)
	cfg.Logging.Session.WindowSize = "145x30"
	cfg.Logging.Session.AutoExportTxt = true
	cfg.SSH.Security.HostKeyPolicy = "tofu"

	got, err := MarshalSparse(cfg)
	if err != nil {
		t.Fatalf("MarshalSparse: %v", err)
	}
	for _, want := range []string{
		"include:",
		"credentials:",
		"inventory:",
		"providers:",
		"idle_timeout: 4h0m0s",
		"window_size: 145x30",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("sparse config missing %q:\n%s", want, got)
		}
	}
	for _, reject := range []string{"[agent]", "credential:", "\nprovider:", "auth_mode:"} {
		if strings.Contains(got, reject) {
			t.Fatalf("sparse config should omit %q:\n%s", reject, got)
		}
	}
}

func boolPtr(v bool) *bool {
	return &v
}

func writeConfigFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
