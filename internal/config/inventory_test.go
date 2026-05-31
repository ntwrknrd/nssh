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

  [inventory.host.edge01.auth]
  credential_provider = "bw-lab"
  password_ref = "op://Network/Edge 01/password"
  username_ref = "op://Network/Edge 01/username"

  [inventory.provider.local]
  type = "local"

    [inventory.provider.local.group.lab]

    [inventory.provider.local.group.customer]
    domain_suffix = [".customer.local"]

      [inventory.provider.local.group.customer.auth]
      credential_provider = "op-network"
      password_ref = "Network Shared Admin"
      username = "netops"

  [inventory.provider.netbox-prod]
  type = "netbox"

    [inventory.provider.netbox-prod.config]
    base_url = "https://netbox.example.com"
    token_env = "NETBOX_TOKEN"

    [inventory.provider.netbox-prod.group.customer.match]
  manufacturer = ["Juniper", "Arista"]
  status = ["active"]

  [inventory.provider.nre-netlab01]
  type = "containerlab"

    [inventory.provider.nre-netlab01.config]
    jump_host = "nre-netlab01"
    sudo = true
    strict_host_key_checking = false

    [inventory.provider.nre-netlab01.group.lab.match]
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

	if cfg.Credential.Provider["pass-local"].Type != CredentialProviderPass {
		t.Fatalf("pass-local type = %q", cfg.Credential.Provider["pass-local"].Type)
	}
	if cfg.Credential.Provider["op-network"].Config.Vault != "Network" {
		t.Fatalf("op-network vault = %q", cfg.Credential.Provider["op-network"].Config.Vault)
	}
	if cfg.Credential.Provider["bw-lab"].Type != CredentialProviderBitwarden {
		t.Fatalf("bw-lab type = %q", cfg.Credential.Provider["bw-lab"].Type)
	}
	customer := cfg.Inventory.Provider["local"].Group["customer"]
	if customer.Auth.CredentialProvider != "op-network" {
		t.Fatalf("inventory.provider.local.group.customer.auth.credential_provider = %q", customer.Auth.CredentialProvider)
	}
	if customer.Auth.PasswordRef != "Network Shared Admin" {
		t.Fatalf("inventory.provider.local.group.customer.auth.password_ref = %q", customer.Auth.PasswordRef)
	}
	if cfg.Inventory.Host["edge01"].Auth.CredentialProvider != "bw-lab" {
		t.Fatalf("inventory.host.edge01.auth.credential_provider = %q", cfg.Inventory.Host["edge01"].Auth.CredentialProvider)
	}
	if cfg.Inventory.Host["edge01"].Auth.UsernameRef != "op://Network/Edge 01/username" {
		t.Fatalf("inventory.host.edge01.auth.username_ref = %q", cfg.Inventory.Host["edge01"].Auth.UsernameRef)
	}
	if got := customer.DomainSuffix; len(got) != 1 || got[0] != ".customer.local" {
		t.Fatalf("customer.domain_suffix = %v", got)
	}
	if customer.Auth.Username != "netops" {
		t.Fatalf("customer auth username = %q", customer.Auth.Username)
	}

	nb := cfg.Inventory.Provider["netbox-prod"]
	if nb.Type != ProviderNetBox {
		t.Fatalf("netbox type = %q", nb.Type)
	}
	if nb.Config.BaseURL != "https://netbox.example.com" {
		t.Fatalf("base_url = %q", nb.Config.BaseURL)
	}
	selectors := cfg.Inventory.ProviderSelectors("netbox-prod")
	if len(selectors) != 1 || selectors[0].Group != "netbox-prod/customer" {
		t.Fatalf("netbox selectors = %+v", selectors)
	}
	if got := selectors[0].Match["manufacturer"]; len(got) != 2 || got[0] != "Juniper" || got[1] != "Arista" {
		t.Fatalf("netbox selector match = %+v", selectors[0].Match)
	}
}

func TestLoadRejectsLegacyInventoryProviderRoutes(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.toml")
	if err := os.WriteFile(path, []byte(`
[inventory.provider.netbox-prod]
type = "netbox"

[[inventory.provider.netbox-prod.route]]
group = "customer"
`), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected legacy route config to be rejected")
	}
	if !strings.Contains(err.Error(), "unknown config key") || !strings.Contains(err.Error(), "inventory.provider.netbox-prod.route") {
		t.Fatalf("error %q does not identify legacy route config", err)
	}
}

func TestInventoryProviderGroupValidatesBareGroupName(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Inventory.Provider = map[string]InventoryProviderConfig{
		"netbox-prod": {Type: ProviderNetBox, Group: map[string]GroupConfig{
			"bad.name": {Match: InventoryMatch{"role": {"router"}}},
		}},
	}

	err := cfg.Inventory.Validate()
	if err == nil {
		t.Fatal("expected invalid group name error")
	}
	if !strings.Contains(err.Error(), "bare-key safe") {
		t.Fatalf("error %q does not identify invalid group name", err)
	}
}

func TestLoadRejectsLegacyInventoryAuthProviderRef(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.toml")
	if err := os.WriteFile(path, []byte(`
[inventory.provider.local.group.default.auth]
provider = "pass-local"
ref = "nssh/groups/default"
`), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected legacy provider/ref keys to be rejected")
	}
	if !strings.Contains(err.Error(), "unknown config key") || !strings.Contains(err.Error(), "inventory.provider.local.group.default.auth.provider") {
		t.Fatalf("error %q does not identify legacy auth keys", err)
	}
}

func TestInventoryAuthResolutionUsesLowestConfiguredField(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Inventory.Auth = InventoryAuthConfig{
		Username: "global-user",
		AuthMode: AuthModeKey,
	}
	cfg.Inventory.Provider = map[string]InventoryProviderConfig{
		"netbox-prod": {
			Type: ProviderNetBox,
			Auth: InventoryAuthConfig{
				Username: "provider-user",
			},
			Group: map[string]GroupConfig{
				"custcbb": {Auth: InventoryAuthConfig{
					CredentialProvider: "op-expedient",
					PasswordRef:        "op://Expedient/group/password",
					AuthMode:           AuthModePassword,
				}},
			},
		},
	}
	cfg.Inventory.Host = map[string]InventoryHostConfig{
		"edge01": {Auth: InventoryAuthConfig{UsernameRef: "op://Expedient/edge01/username"}},
	}

	got := cfg.ResolveInventoryAuth(InventoryAuthContext{
		Host:     "edge01",
		Group:    "netbox-prod/custcbb",
		Provider: "netbox-prod",
	})

	if got.UsernameRef != "op://Expedient/edge01/username" {
		t.Fatalf("username_ref = %q", got.UsernameRef)
	}
	if got.CredentialProvider != "op-expedient" || got.PasswordRef != "op://Expedient/group/password" {
		t.Fatalf("password binding = provider %q ref %q", got.CredentialProvider, got.PasswordRef)
	}
	if got.AuthMode != AuthModePassword {
		t.Fatalf("auth_mode = %q, want password", got.AuthMode)
	}
}

func TestLoadRejectsLegacyDefaultUser(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.toml")
	if err := os.WriteFile(path, []byte(`
[inventory.provider.local.group.custcbb]
default_user = "group-user"
domain_suffix = [".custcbb.local"]
`), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected legacy default_user to be rejected")
	}
	if !strings.Contains(err.Error(), "unknown config key") || !strings.Contains(err.Error(), "inventory.provider.local.group.custcbb.default_user") {
		t.Fatalf("error %q does not identify default_user", err)
	}
}

func TestDefaultCredentialConfigCreatesPassLocalProvider(t *testing.T) {
	cfg := CredentialConfig{}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
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
				Provider: map[string]CredentialProviderConfig{
					"pass-local": {Type: CredentialProviderPass},
				},
			},
		},
		{
			name: "1password provider",
			cfg: CredentialConfig{
				Provider: map[string]CredentialProviderConfig{
					"op-network": {Type: CredentialProvider1Password, Config: CredentialProviderDetailConfig{Vault: "Network"}},
				},
			},
		},
		{
			name: "bitwarden provider",
			cfg: CredentialConfig{
				Provider: map[string]CredentialProviderConfig{
					"bw-lab": {Type: CredentialProviderBitwarden},
				},
			},
		},
		{
			name: "provider name must be bare-key safe",
			cfg: CredentialConfig{
				Provider: map[string]CredentialProviderConfig{
					"bad.name": {Type: CredentialProviderPass},
				},
			},
			wantErr: "bare-key safe",
		},
		{
			name: "invalid provider-session policy",
			cfg: CredentialConfig{
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

func TestInventoryConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     InventoryConfig
		wantErr string
	}{
		{
			name: "default local group is valid",
			cfg: InventoryConfig{
				Provider: map[string]InventoryProviderConfig{
					"local": {Type: ProviderLocal, Group: map[string]GroupConfig{"default": {}}},
				},
			},
		},
		{
			name: "invalid group name",
			cfg: InventoryConfig{
				Provider: map[string]InventoryProviderConfig{
					"local": {Type: ProviderLocal, Group: map[string]GroupConfig{"bad.name": {}}},
				},
			},
			wantErr: "bare-key safe",
		},
		{
			name: "provider group selector is valid",
			cfg: InventoryConfig{
				Provider: map[string]InventoryProviderConfig{
					"netbox-prod": {
						Type:   ProviderNetBox,
						Config: InventoryProviderDetailConfig{BaseURL: "https://netbox.example.com"},
						Group:  map[string]GroupConfig{"default": {Match: InventoryMatch{"role": {"router"}}}},
					},
				},
			},
		},
		{
			name: "group auth requires password_ref",
			cfg: InventoryConfig{
				Provider: map[string]InventoryProviderConfig{
					"local": {Type: ProviderLocal, Group: map[string]GroupConfig{
						"default": {Auth: InventoryAuthConfig{CredentialProvider: "pass-local"}},
					}},
				},
			},
			wantErr: "password_ref or username_ref is required",
		},
		{
			name: "group auth requires credential_provider",
			cfg: InventoryConfig{
				Provider: map[string]InventoryProviderConfig{
					"local": {Type: ProviderLocal, Group: map[string]GroupConfig{
						"default": {Auth: InventoryAuthConfig{PasswordRef: "nssh/groups/default"}},
					}},
				},
			},
			wantErr: "credential_provider is required",
		},
		{
			name: "host auth username options conflict",
			cfg: InventoryConfig{
				Provider: map[string]InventoryProviderConfig{
					"local": {Type: ProviderLocal, Group: map[string]GroupConfig{"default": {}}},
				},
				Host: map[string]InventoryHostConfig{
					"edge01": {Auth: InventoryAuthConfig{CredentialProvider: "pass-local", PasswordRef: "nssh/hosts/edge01", Username: "admin", UsernameRef: "op://vault/item/username"}},
				},
			},
			wantErr: "username and username_ref are mutually exclusive",
		},
		{
			name: "host auth cannot be disabled and set",
			cfg: InventoryConfig{
				Provider: map[string]InventoryProviderConfig{
					"local": {Type: ProviderLocal, Group: map[string]GroupConfig{"default": {}}},
				},
				Host: map[string]InventoryHostConfig{
					"edge01": {
						AuthDisabled: true,
						Auth:         InventoryAuthConfig{CredentialProvider: "pass-local", PasswordRef: "nssh/hosts/edge01"},
					},
				},
			},
			wantErr: "cannot set auth and auth_disabled",
		},
		{
			name: "containerlab requires jump host",
			cfg: InventoryConfig{
				Provider: map[string]InventoryProviderConfig{
					"nre-netlab01": {
						Type:  ProviderContainerlab,
						Group: map[string]GroupConfig{"lab": {}},
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
	localProvider := cfg.Inventory.Provider["local"]
	localProvider.Group["default"] = GroupConfig{Auth: InventoryAuthConfig{CredentialProvider: "missing", PasswordRef: "nssh/groups/default"}}
	cfg.Inventory.Provider["local"] = localProvider

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected unknown provider error")
	}
	if !strings.Contains(err.Error(), "references unknown provider") {
		t.Fatalf("error %q does not contain unknown provider", err)
	}
}

func TestLoadInventoryGroupsDoNotRetainImplicitDefaultGroupWhenConfigDefinesGroups(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.toml")
	input := `
[inventory.provider.local]
type = "local"

[inventory.provider.local.group.corp]
`
	if err := os.WriteFile(path, []byte(input), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := cfg.Inventory.Provider["local"].Group["default"]; ok {
		t.Fatalf("unexpected default group retained: %+v", cfg.Inventory.Provider["local"].Group["default"])
	}
}
