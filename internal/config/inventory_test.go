package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestInventoryCredentialConfigDecode(t *testing.T) {
	const input = `
[credential]
default_provider = "pass-local"

  [credential.provider.pass-local]
  type = "pass"

    [credential.provider.pass-local.config]
    command = "pass"
    prefix = "nssh"

  [credential.provider.op-network]
  type = "1password"

    [credential.provider.op-network.config]
    account = "ntwrknrd"
    vault = "Network"
    session = "agent"

  [credential.provider.bw-lab]
  type = "bitwarden"

    [credential.provider.bw-lab.config]
    session = "external"

[inventory]
default_group = "lab"

  [inventory.group.lab]

  [inventory.group.custcbb]
  domain_suffix = [".custcbb.local"]
  default_user = "chris.jones"

    [inventory.group.custcbb.auth]
    provider = "op-network"
    ref = "Network Shared Admin"

  [inventory.host.edge01.auth]
  provider = "bw-lab"
  ref = "op://Network/Edge 01/password"
  username_ref = "op://Network/Edge 01/username"

  [inventory.provider.netbox-prod]
  type = "netbox"

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

	if cfg.Credential.DefaultProvider != "pass-local" {
		t.Fatalf("credential.default_provider = %q", cfg.Credential.DefaultProvider)
	}
	if cfg.Credential.Provider["pass-local"].Type != CredentialProviderPass {
		t.Fatalf("pass-local type = %q", cfg.Credential.Provider["pass-local"].Type)
	}
	if cfg.Credential.Provider["op-network"].Config.Vault != "Network" {
		t.Fatalf("op-network vault = %q", cfg.Credential.Provider["op-network"].Config.Vault)
	}
	if cfg.Credential.Provider["bw-lab"].Type != CredentialProviderBitwarden {
		t.Fatalf("bw-lab type = %q", cfg.Credential.Provider["bw-lab"].Type)
	}
	if cfg.Inventory.Group["custcbb"].Auth.Provider != "op-network" {
		t.Fatalf("inventory.group.custcbb.auth.provider = %q", cfg.Inventory.Group["custcbb"].Auth.Provider)
	}
	if cfg.Inventory.Group["custcbb"].Auth.Ref != "Network Shared Admin" {
		t.Fatalf("inventory.group.custcbb.auth.ref = %q", cfg.Inventory.Group["custcbb"].Auth.Ref)
	}
	if cfg.Inventory.Host["edge01"].Auth.Provider != "bw-lab" {
		t.Fatalf("inventory.host.edge01.auth.provider = %q", cfg.Inventory.Host["edge01"].Auth.Provider)
	}
	if cfg.Inventory.Host["edge01"].Auth.UsernameRef != "op://Network/Edge 01/username" {
		t.Fatalf("inventory.host.edge01.auth.username_ref = %q", cfg.Inventory.Host["edge01"].Auth.UsernameRef)
	}
	if cfg.Inventory.DefaultGroup != "lab" {
		t.Fatalf("inventory.default_group = %q", cfg.Inventory.DefaultGroup)
	}
	if got := cfg.Inventory.Group["custcbb"].DomainSuffix; len(got) != 1 || got[0] != ".custcbb.local" {
		t.Fatalf("custcbb.domain_suffix = %v", got)
	}
	if cfg.Inventory.Group["custcbb"].DefaultUser != "chris.jones" {
		t.Fatalf("custcbb.default_user = %q", cfg.Inventory.Group["custcbb"].DefaultUser)
	}

	nb := cfg.Inventory.Provider["netbox-prod"]
	if nb.Type != ProviderNetBox {
		t.Fatalf("netbox type = %q", nb.Type)
	}
	if nb.Config.BaseURL != "https://netbox.example.com" {
		t.Fatalf("base_url = %q", nb.Config.BaseURL)
	}
	if len(nb.Route) != 1 || nb.Route[0].Group != "custcbb" {
		t.Fatalf("netbox routes = %+v", nb.Route)
	}
}

func TestDefaultCredentialConfigCreatesPassLocal(t *testing.T) {
	cfg := CredentialConfig{}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}

	if cfg.DefaultProvider != "pass-local" {
		t.Fatalf("default_provider = %q", cfg.DefaultProvider)
	}
	provider := cfg.Provider["pass-local"]
	if provider.Type != CredentialProviderPass {
		t.Fatalf("pass-local type = %q", provider.Type)
	}
	if provider.Config.Command != "pass" {
		t.Fatalf("pass-local command = %q", provider.Config.Command)
	}
	if provider.Config.Prefix != "nssh" {
		t.Fatalf("pass-local prefix = %q", provider.Config.Prefix)
	}
}

func TestCredentialConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     CredentialConfig
		wantErr string
	}{
		{
			name: "pass provider",
			cfg: CredentialConfig{
				DefaultProvider: "pass-local",
				Provider: map[string]CredentialProviderConfig{
					"pass-local": {Type: CredentialProviderPass},
				},
			},
		},
		{
			name: "1password provider",
			cfg: CredentialConfig{
				DefaultProvider: "op-network",
				Provider: map[string]CredentialProviderConfig{
					"op-network": {Type: CredentialProvider1Password, Config: CredentialProviderDetailConfig{Vault: "Network"}},
				},
			},
		},
		{
			name: "bitwarden provider",
			cfg: CredentialConfig{
				DefaultProvider: "bw-lab",
				Provider: map[string]CredentialProviderConfig{
					"bw-lab": {Type: CredentialProviderBitwarden},
				},
			},
		},
		{
			name: "age provider is invalid",
			cfg: CredentialConfig{
				DefaultProvider: "local-age",
				Provider: map[string]CredentialProviderConfig{
					"local-age": {Type: "age"},
				},
			},
			wantErr: "unsupported credential provider",
		},
		{
			name:    "legacy global type is invalid",
			cfg:     CredentialConfig{Type: "age"},
			wantErr: "credential.type is no longer supported",
		},
		{
			name: "provider name must be bare-key safe",
			cfg: CredentialConfig{
				DefaultProvider: "bad.name",
				Provider: map[string]CredentialProviderConfig{
					"bad.name": {Type: CredentialProviderPass},
				},
			},
			wantErr: "bare-key safe",
		},
		{
			name: "default provider must exist",
			cfg: CredentialConfig{
				DefaultProvider: "missing",
				Provider: map[string]CredentialProviderConfig{
					"pass-local": {Type: CredentialProviderPass},
				},
			},
			wantErr: "default_provider references unknown provider",
		},
		{
			name: "legacy host binding is invalid",
			cfg: CredentialConfig{
				DefaultProvider: "pass-local",
				Provider: map[string]CredentialProviderConfig{
					"pass-local": {Type: CredentialProviderPass},
				},
				Host: map[string]CredentialRefConfig{
					"edge01": {Provider: "pass-local", Ref: "nssh/hosts/edge01"},
				},
			},
			wantErr: "credential.host is no longer supported",
		},
		{
			name: "legacy group binding is invalid",
			cfg: CredentialConfig{
				DefaultProvider: "pass-local",
				Provider: map[string]CredentialProviderConfig{
					"pass-local": {Type: CredentialProviderPass},
				},
				Group: map[string]CredentialRefConfig{
					"default": {Ref: "nssh/groups/default"},
				},
			},
			wantErr: "credential.group is no longer supported",
		},
		{
			name: "invalid provider-session policy",
			cfg: CredentialConfig{
				DefaultProvider: "op-network",
				Provider: map[string]CredentialProviderConfig{
					"op-network": {Type: CredentialProvider1Password, Config: CredentialProviderDetailConfig{Vault: "Network", Session: "forever"}},
				},
			},
			wantErr: "invalid provider session policy",
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

func TestLegacyInventoryCredentialConfigDecode(t *testing.T) {
	const input = `
[credential]
type = "1password"

  [credential.config]
  account = "ntwrknrd"
  vault = "Network"

  [credential.group.custcbb]
  ref = "Network Shared Admin"

  [credential.host.edge01]
  ref = "op://Network/Edge 01/password"
  username_ref = "op://Network/Edge 01/username"

[inventory]
default_group = "lab"

  [inventory.group.lab]

  [inventory.group.custcbb]
  domain_suffix = [".custcbb.local"]

  [inventory.provider.netbox-prod]
  type = "netbox"

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
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "credential.type is no longer supported") {
		t.Fatalf("validate error = %v, want credential.type rejection", err)
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
			name: "group auth requires ref",
			cfg: InventoryConfig{
				DefaultGroup: "default",
				Group: map[string]GroupConfig{
					"default": {Auth: InventoryAuthConfig{Provider: "pass-local"}},
				},
			},
			wantErr: "ref is required",
		},
		{
			name: "host auth username options conflict",
			cfg: InventoryConfig{
				DefaultGroup: "default",
				Group: map[string]GroupConfig{
					"default": {},
				},
				Host: map[string]InventoryHostConfig{
					"edge01": {Auth: InventoryAuthConfig{Provider: "pass-local", Ref: "nssh/hosts/edge01", Username: "admin", UsernameRef: "op://vault/item/username"}},
				},
			},
			wantErr: "username and username_ref are mutually exclusive",
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

func TestConfigValidationRejectsUnknownInventoryAuthProvider(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Inventory.Group["default"] = GroupConfig{
		Auth: InventoryAuthConfig{Provider: "missing", Ref: "nssh/groups/default"},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected unknown provider error")
	}
	if !strings.Contains(err.Error(), "references unknown provider") {
		t.Fatalf("error %q does not contain unknown provider", err)
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

func TestSaveDoesNotEmitLegacySyncTable(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.toml")
	cfg := DefaultConfig()

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
