package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
)

func TestInventoryCredentialConfigDecode(t *testing.T) {
	const input = `
[credential]
type = "1password"

  [credential.config]
  account = "ntwrknrd"
  vault = "Network"

[inventory]
default_group = "lab"

  [inventory.group.lab]
  local_file = "local_lab.conf"

  [inventory.group.custcbb]
  domain_suffix = [".custcbb.local"]
  local_file = "local_custcbb.conf"

  [inventory.provider.netbox-prod]
  type = "netbox"
  refresh_interval = "15m"

    [inventory.provider.netbox-prod.config]
    base_url = "https://netbox.example.com"
    token_env = "NETBOX_TOKEN"

    [[inventory.provider.netbox-prod.route]]
    group = "custcbb"

      [inventory.provider.netbox-prod.route.match]
      manufacturer = ["Juniper", "Arista"]
      status = ["active"]

  [inventory.provider.nre-netlab01]
  type = "containerlab"
  refresh_interval = "1m"

    [inventory.provider.nre-netlab01.config]
    jump_host = "nre-netlab01"
    sudo = true
    strict_host_key_checking = false

    [[inventory.provider.nre-netlab01.route]]
    group = "lab"

      [inventory.provider.nre-netlab01.route.match]
      kind = ["ceos", "vjunos"]
      state = ["running"]
`

	cfg := DefaultConfig()
	if _, err := toml.Decode(input, cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}

	if cfg.Credential.Type != CredentialProvider1Password {
		t.Fatalf("credential.type = %q", cfg.Credential.Type)
	}
	if cfg.Credential.Config.Vault != "Network" {
		t.Fatalf("credential.config.vault = %q", cfg.Credential.Config.Vault)
	}
	if cfg.Inventory.DefaultGroup != "lab" {
		t.Fatalf("inventory.default_group = %q", cfg.Inventory.DefaultGroup)
	}
	if got := cfg.Inventory.Group["custcbb"].LocalFile; got != "local_custcbb.conf" {
		t.Fatalf("custcbb.local_file = %q", got)
	}

	nb := cfg.Inventory.Provider["netbox-prod"]
	if nb.Type != ProviderNetBox {
		t.Fatalf("netbox type = %q", nb.Type)
	}
	if nb.RefreshInterval.Duration() != 15*time.Minute {
		t.Fatalf("refresh interval = %v", nb.RefreshInterval.Duration())
	}
	if nb.Config.BaseURL != "https://netbox.example.com" {
		t.Fatalf("base_url = %q", nb.Config.BaseURL)
	}
	if len(nb.Route) != 1 || nb.Route[0].Group != "custcbb" {
		t.Fatalf("netbox routes = %+v", nb.Route)
	}
}

func TestInventoryConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     InventoryConfig
		wantErr string
	}{
		{
			name: "default local group is valid",
			cfg: InventoryConfig{
				DefaultGroup: "default",
				Group: map[string]GroupConfig{
					"default": {},
				},
			},
		},
		{
			name: "invalid group name",
			cfg: InventoryConfig{
				DefaultGroup: "bad.name",
				Group: map[string]GroupConfig{
					"bad.name": {},
				},
			},
			wantErr: "bare-key safe",
		},
		{
			name: "provider route group must exist",
			cfg: InventoryConfig{
				DefaultGroup: "default",
				Group: map[string]GroupConfig{
					"default": {},
				},
				Provider: map[string]InventoryProviderConfig{
					"netbox-prod": {
						Type:   ProviderNetBox,
						Config: InventoryProviderDetailConfig{BaseURL: "https://netbox.example.com"},
						Route:  []InventoryRouteConfig{{Group: "missing"}},
					},
				},
			},
			wantErr: "unknown group",
		},
		{
			name: "unsupported credential backend is separate concern",
			cfg: InventoryConfig{
				DefaultGroup: "default",
				Group: map[string]GroupConfig{
					"default": {},
				},
			},
		},
		{
			name: "local file must stay inside nssh include dir",
			cfg: InventoryConfig{
				DefaultGroup: "cbb",
				Group: map[string]GroupConfig{
					"cbb": {LocalFile: "conf.d/cbb_hosts"},
				},
			},
			wantErr: "local_file must be a filename",
		},
		{
			name: "containerlab requires jump host",
			cfg: InventoryConfig{
				DefaultGroup: "lab",
				Group: map[string]GroupConfig{
					"lab": {},
				},
				Provider: map[string]InventoryProviderConfig{
					"nre-netlab01": {
						Type:  ProviderContainerlab,
						Route: []InventoryRouteConfig{{Group: "lab"}},
					},
				},
			},
			wantErr: "jump_host is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error %q does not contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestCredentialConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     CredentialConfig
		wantErr string
	}{
		{name: "default age", cfg: CredentialConfig{Type: CredentialProviderAge}},
		{name: "1password with vault", cfg: CredentialConfig{Type: CredentialProvider1Password, Config: CredentialProviderDetailConfig{Vault: "Network"}}},
		{name: "1password requires vault", cfg: CredentialConfig{Type: CredentialProvider1Password}, wantErr: "vault is required"},
		{name: "unsupported", cfg: CredentialConfig{Type: "vaultwarden"}, wantErr: "unsupported credential provider"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error %q does not contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestLoadRejectsLegacySyncSources(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.toml")
	input := `
[[sync.sources]]
name = "lab"
provider = "containerlab"

  [sync.sources.containerlab]
  jump_host = "jumpbox"

  [[sync.sources.routes]]
  context = "lab"
`
	if err := os.WriteFile(path, []byte(input), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected legacy sync config rejection")
	}
	if !strings.Contains(err.Error(), "sync.sources is no longer supported") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSaveDoesNotEmitLegacySyncConfig(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.toml")
	cfg := DefaultConfig()
	cfg.Sync.Sources = []SyncSourceConfig{{Name: "legacy"}}

	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	if strings.Contains(string(data), "[sync]") || strings.Contains(string(data), "[[sync.sources]]") {
		t.Fatalf("saved config contains legacy sync table:\n%s", data)
	}
}

func TestLoadInventoryGroupsDoNotRetainDefaultGroupWhenConfigDefinesGroups(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.toml")
	input := `
[inventory]
default_group = "cbb"

  [inventory.group.cbb]
  local_file = "local_cbb.conf"
`
	if err := os.WriteFile(path, []byte(input), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Inventory.DefaultGroup != "cbb" {
		t.Fatalf("default_group = %q", cfg.Inventory.DefaultGroup)
	}
	if _, ok := cfg.Inventory.Group["default"]; ok {
		t.Fatalf("unexpected default group retained: %+v", cfg.Inventory.Group["default"])
	}
}
