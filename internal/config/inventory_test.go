package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestInventoryCredentialConfigDecode(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.yaml")
	writeConfigFile(t, path, `
credential:
  provider:
    sops:
      type: sops-age
      file: ~/.local/share/nssh/credentials.sops.yaml
      age_key_file: ~/.config/sops/age/keys.txt
    op-network:
      type: 1password
      account: ntwrknrd
      vault: Network
    bw-lab:
      type: bitwarden
inventory:
  providers:
    local:
      type: local
      groups:
        lab: {}
        customer:
          domain_suffix: [.customer.local]
          auth:
            credential_provider: op-network
            password_ref: Network Shared Admin
            username: netops
      hosts:
        edge01:
          auth:
            credential_provider: bw-lab
            password_ref: op://Network/Edge 01/password
            username_ref: op://Network/Edge 01/username
    netbox-prod:
      type: netbox
      config:
        base_url: https://netbox.example.com
        token_env: NETBOX_TOKEN
      groups:
        customer:
          match:
            manufacturer: [Juniper, Arista]
            status: [active]
    nre-netlab01:
      type: containerlab
      config:
        jump_host: nre-netlab01
        sudo: true
        strict_host_key_checking: false
      groups:
        lab:
          match:
            kind: [ceos, vjunos]
            state: [running]
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Credential.Provider["sops"].Type != CredentialProviderSOPSAge {
		t.Fatalf("sops type = %q", cfg.Credential.Provider["sops"].Type)
	}
	if cfg.Credential.Provider["sops"].File != "~/.local/share/nssh/credentials.sops.yaml" {
		t.Fatalf("sops file = %q", cfg.Credential.Provider["sops"].File)
	}
	if cfg.Credential.Provider["sops"].AgeKeyFile != "~/.config/sops/age/keys.txt" {
		t.Fatalf("sops age_key_file = %q", cfg.Credential.Provider["sops"].AgeKeyFile)
	}
	if cfg.Credential.Provider["op-network"].Vault != "Network" {
		t.Fatalf("op-network vault = %q", cfg.Credential.Provider["op-network"].Vault)
	}
	if cfg.Credential.Provider["bw-lab"].Type != CredentialProviderBitwarden {
		t.Fatalf("bw-lab type = %q", cfg.Credential.Provider["bw-lab"].Type)
	}
	customer := cfg.Inventory.Providers["local"].Groups["customer"]
	if customer.Auth.CredentialProvider != "op-network" {
		t.Fatalf("customer auth credential_provider = %q", customer.Auth.CredentialProvider)
	}
	if cfg.Inventory.Provider["local"].Hosts["edge01"].Auth.CredentialProvider != "bw-lab" {
		t.Fatalf("edge01 auth = %+v", cfg.Inventory.Provider["local"].Hosts["edge01"].Auth)
	}
	if got := customer.DomainSuffix; len(got) != 1 || got[0] != ".customer.local" {
		t.Fatalf("customer.domain_suffix = %v", got)
	}
	nb := cfg.Inventory.Providers["netbox-prod"]
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

func TestInventoryProviderSSHDefaultsDecode(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.yaml")
	writeConfigFile(t, path, `
inventory:
  providers:
    clab-all:
      type: containerlab
      config:
        jump_host: nre@nre-netlab01.example.com
        ssh_defaults: all
      groups:
        lab: {}
    clab-selected:
      type: containerlab
      config:
        jump_host: nre@nre-netlab02.example.com
        ssh_defaults: [SetEnv, ServerAliveInterval]
      groups:
        lab: {}
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Inventory.Providers["clab-all"].Config.SSHDefaults.Mode; got != "all" {
		t.Fatalf("scalar ssh_defaults mode = %q, want all", got)
	}
	if got := strings.Join(cfg.Inventory.Providers["clab-selected"].Config.SSHDefaults.Options, ","); got != "SetEnv,ServerAliveInterval" {
		t.Fatalf("list ssh_defaults options = %q, want SetEnv,ServerAliveInterval", got)
	}
}

func TestInventoryProviderGroupValidatesConfigKeySafeGroupName(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Inventory.Providers = map[string]InventoryProviderConfig{
		"netbox-prod": {Type: ProviderNetBox, Groups: map[string]GroupConfig{
			"bad.name": {Match: InventoryMatch{"role": {"router"}}},
		}},
	}

	err := cfg.Inventory.Validate()
	if err == nil {
		t.Fatal("expected invalid group name error")
	}
	if !strings.Contains(err.Error(), "must use only") {
		t.Fatalf("error %q does not identify invalid group name", err)
	}
}

func TestInventoryAuthResolutionUsesLowestConfiguredField(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Inventory.Auth = InventoryAuthConfig{
		Username: "global-user",
		Mode:     AuthModeKey,
	}
	cfg.Inventory.Providers = map[string]InventoryProviderConfig{
		"netbox-prod": {
			Type: ProviderNetBox,
			Auth: InventoryAuthConfig{
				Username: "provider-user",
			},
			Groups: map[string]GroupConfig{
				"custcbb": {Auth: InventoryAuthConfig{
					CredentialProvider: "op-expedient",
					PasswordRef:        "op://Expedient/group/password",
					Mode:               AuthModePassword,
				}},
			},
			Hosts: map[string]InventoryHostConfig{
				"edge01": {Auth: InventoryAuthConfig{UsernameRef: "op://Expedient/edge01/username"}},
			},
		},
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
		t.Fatalf("auth mode = %q, want password", got.AuthMode)
	}
}

func TestInventoryAuthResolutionPasswordModePreservesInheritedCredential(t *testing.T) {
	cfg := &Config{}
	cfg.Inventory.Providers = map[string]InventoryProviderConfig{
		"local": {
			Type: ProviderLocal,
			Groups: map[string]GroupConfig{
				"custcbb": {Auth: InventoryAuthConfig{
					CredentialProvider: "op-expedient",
					PasswordRef:        "op://Expedient/group/password",
					Username:           "chris.jones",
				}},
			},
			Hosts: map[string]InventoryHostConfig{
				"pla-ts01.custcbb.local": {
					Group: "custcbb",
					Auth:  InventoryAuthConfig{Mode: AuthModePassword},
				},
			},
		},
	}

	got := cfg.ResolveInventoryAuth(InventoryAuthContext{
		Host:     "pla-ts01.custcbb.local",
		Group:    "local/custcbb",
		Provider: "local",
	})

	if got.AuthMode != AuthModePassword {
		t.Fatalf("auth mode = %q, want password", got.AuthMode)
	}
	if got.CredentialProvider != "op-expedient" || got.PasswordRef != "op://Expedient/group/password" {
		t.Fatalf("password binding = provider %q ref %q", got.CredentialProvider, got.PasswordRef)
	}
	if got.PasswordSource != "group local/custcbb" {
		t.Fatalf("password source = %q, want group local/custcbb", got.PasswordSource)
	}
}

func TestLoadedInventoryPasswordHostPreservesGroupCredential(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.yaml")
	writeConfigFile(t, path, `
credential:
  provider:
    op-expedient:
      type: 1password
      vault: Network
inventory:
  providers:
    local:
      type: local
      groups:
        custcbb:
          auth:
            credential_provider: op-expedient
            password_ref: op://Network/custcbb/password
            username: chris.jones
      hosts:
        pla-ts01.custcbb.local:
          group: custcbb
          auth:
            mode: password
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := cfg.ResolveInventoryAuth(InventoryAuthContext{
		Host:     "pla-ts01.custcbb.local",
		Group:    "local/custcbb",
		Provider: "local",
	})
	if got.CredentialProvider != "op-expedient" || got.PasswordRef != "op://Network/custcbb/password" {
		t.Fatalf("loaded password binding = provider %q ref %q", got.CredentialProvider, got.PasswordRef)
	}
	if got.AuthMode != AuthModePassword || got.AuthModeSource != "host pla-ts01.custcbb.local" {
		t.Fatalf("loaded auth mode = %q source %q", got.AuthMode, got.AuthModeSource)
	}
}

func TestInventoryAuthResolutionKeyModeClearsInheritedCredential(t *testing.T) {
	cfg := &Config{}
	cfg.Inventory.Providers = map[string]InventoryProviderConfig{
		"local": {
			Type: ProviderLocal,
			Groups: map[string]GroupConfig{
				"custcbb": {Auth: InventoryAuthConfig{
					CredentialProvider: "op-expedient",
					PasswordRef:        "op://Expedient/group/password",
				}},
			},
			Hosts: map[string]InventoryHostConfig{
				"jump01.custcbb.local": {
					Group: "custcbb",
					Auth:  InventoryAuthConfig{Mode: AuthModeKey},
				},
			},
		},
	}

	got := cfg.ResolveInventoryAuth(InventoryAuthContext{
		Host:     "jump01.custcbb.local",
		Group:    "local/custcbb",
		Provider: "local",
	})

	if got.CredentialProvider != "" || got.PasswordRef != "" {
		t.Fatalf("key auth retained password binding: provider %q ref %q", got.CredentialProvider, got.PasswordRef)
	}
	if got.PasswordSource != "host jump01.custcbb.local" {
		t.Fatalf("password source = %q, want host jump01.custcbb.local", got.PasswordSource)
	}
}

func TestInventoryAuthResolutionProviderHostDisabledClearsInheritedCredential(t *testing.T) {
	cfg := &Config{}
	cfg.Inventory.Providers = map[string]InventoryProviderConfig{
		"netbox-prod": {
			Type: ProviderNetBox,
			Auth: InventoryAuthConfig{
				CredentialProvider: "op-expedient",
				PasswordRef:        "op://Expedient/provider/password",
			},
			Hosts: map[string]InventoryHostConfig{
				"edge01": {AuthDisabled: true},
			},
		},
	}

	got := cfg.ResolveInventoryAuth(InventoryAuthContext{Host: "edge01", Provider: "netbox-prod"})
	if !got.Disabled {
		t.Fatal("provider-scoped host auth was not disabled")
	}
	if got.CredentialProvider != "" || got.PasswordRef != "" || got.PasswordSource != "disabled" {
		t.Fatalf("disabled host retained credential: provider=%q ref=%q source=%q", got.CredentialProvider, got.PasswordRef, got.PasswordSource)
	}
}

func TestDefaultCredentialConfigCreatesSOPSAgeProvider(t *testing.T) {
	cfg := CredentialConfig{}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}

	provider := cfg.Provider["sops"]
	if provider.Type != CredentialProviderSOPSAge {
		t.Fatalf("sops type = %q", provider.Type)
	}
	if provider.File != "~/.local/share/nssh/credentials.sops.yaml" {
		t.Fatalf("sops file = %q", provider.File)
	}
}

func TestCredentialConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     CredentialConfig
		wantErr string
	}{
		{
			name: "sops-age provider",
			cfg: CredentialConfig{
				Provider: map[string]CredentialProviderConfig{
					"sops": {Type: CredentialProviderSOPSAge, File: "~/.local/share/nssh/credentials.sops.yaml"},
				},
			},
		},
		{
			name: "pass provider is unsupported",
			cfg: CredentialConfig{
				Provider: map[string]CredentialProviderConfig{
					"pass": {Type: "pass"},
				},
			},
			wantErr: "unsupported credential provider",
		},
		{
			name: "sops-age provider requires file",
			cfg: CredentialConfig{
				Provider: map[string]CredentialProviderConfig{
					"sops": {Type: CredentialProviderSOPSAge},
				},
			},
			wantErr: "config.file is required",
		},
		{
			name: "1password provider",
			cfg: CredentialConfig{
				Provider: map[string]CredentialProviderConfig{
					"op-network": {Type: CredentialProvider1Password, Vault: "Network"},
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
			name: "provider name must be config-key safe",
			cfg: CredentialConfig{
				Provider: map[string]CredentialProviderConfig{
					"bad.name": {Type: CredentialProviderSOPSAge, File: "~/.local/share/nssh/credentials.sops.yaml"},
				},
			},
			wantErr: "must use only",
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
				Providers: map[string]InventoryProviderConfig{
					"local": {Type: ProviderLocal, Groups: map[string]GroupConfig{"default": {}}},
				},
			},
		},
		{
			name: "invalid group name",
			cfg: InventoryConfig{
				Providers: map[string]InventoryProviderConfig{
					"local": {Type: ProviderLocal, Groups: map[string]GroupConfig{"bad.name": {}}},
				},
			},
			wantErr: "must use only",
		},
		{
			name: "provider group selector is valid",
			cfg: InventoryConfig{
				Providers: map[string]InventoryProviderConfig{
					"netbox-prod": {
						Type:   ProviderNetBox,
						Config: InventoryProviderDetailConfig{BaseURL: "https://netbox.example.com"},
						Groups: map[string]GroupConfig{"default": {Match: InventoryMatch{"role": {"router"}}}},
					},
				},
			},
		},
		{
			name: "group auth requires password_ref",
			cfg: InventoryConfig{
				Providers: map[string]InventoryProviderConfig{
					"local": {Type: ProviderLocal, Groups: map[string]GroupConfig{
						"default": {Auth: InventoryAuthConfig{CredentialProvider: "sops"}},
					}},
				},
			},
			wantErr: "password_ref or username_ref is required",
		},
		{
			name: "group auth requires credential_provider",
			cfg: InventoryConfig{
				Providers: map[string]InventoryProviderConfig{
					"local": {Type: ProviderLocal, Groups: map[string]GroupConfig{
						"default": {Auth: InventoryAuthConfig{PasswordRef: "groups.default.password"}},
					}},
				},
			},
			wantErr: "credential_provider is required",
		},
		{
			name: "host auth username options conflict",
			cfg: InventoryConfig{
				Providers: map[string]InventoryProviderConfig{
					"local": {
						Type:   ProviderLocal,
						Groups: map[string]GroupConfig{"default": {}},
						Hosts: map[string]InventoryHostConfig{
							"edge01": {Group: "default", Auth: InventoryAuthConfig{CredentialProvider: "sops", PasswordRef: "hosts.edge01.password", Username: "admin", UsernameRef: "op://vault/item/username"}},
						},
					},
				},
			},
			wantErr: "username and username_ref are mutually exclusive",
		},
		{
			name: "host auth cannot be disabled and set",
			cfg: InventoryConfig{
				Providers: map[string]InventoryProviderConfig{
					"local": {
						Type:   ProviderLocal,
						Groups: map[string]GroupConfig{"default": {}},
						Hosts: map[string]InventoryHostConfig{
							"edge01": {
								Group:        "default",
								AuthDisabled: true,
								Auth:         InventoryAuthConfig{CredentialProvider: "sops", PasswordRef: "hosts.edge01.password"},
							},
						},
					},
				},
			},
			wantErr: "cannot set auth and auth_disabled",
		},
		{
			name: "containerlab requires jump host",
			cfg: InventoryConfig{
				Providers: map[string]InventoryProviderConfig{
					"nre-netlab01": {
						Type:   ProviderContainerlab,
						Groups: map[string]GroupConfig{"lab": {}},
					},
				},
			},
			wantErr: "jump_host is required",
		},
		{
			name: "containerlab accepts selected ssh defaults",
			cfg: InventoryConfig{
				Providers: map[string]InventoryProviderConfig{
					"nre-netlab01": {
						Type: ProviderContainerlab,
						Config: InventoryProviderDetailConfig{
							JumpHost:    "nre@nre-netlab01.example.com",
							SSHDefaults: NewSSHDefaultsInheritanceOptions("SetEnv"),
						},
						Groups: map[string]GroupConfig{"lab": {}},
					},
				},
			},
		},
		{
			name: "containerlab accepts all ssh defaults",
			cfg: InventoryConfig{
				Providers: map[string]InventoryProviderConfig{
					"nre-netlab01": {
						Type: ProviderContainerlab,
						Config: InventoryProviderDetailConfig{
							JumpHost:    "nre@nre-netlab01.example.com",
							SSHDefaults: NewSSHDefaultsInheritanceMode("all"),
						},
						Groups: map[string]GroupConfig{"lab": {}},
					},
				},
			},
		},
		{
			name: "containerlab rejects unknown ssh defaults policy",
			cfg: InventoryConfig{
				Providers: map[string]InventoryProviderConfig{
					"nre-netlab01": {
						Type: ProviderContainerlab,
						Config: InventoryProviderDetailConfig{
							JumpHost:    "nre@nre-netlab01.example.com",
							SSHDefaults: NewSSHDefaultsInheritanceMode("selected"),
						},
						Groups: map[string]GroupConfig{"lab": {}},
					},
				},
			},
			wantErr: "ssh_defaults must be all, none, or a list",
		},
		{
			name: "containerlab rejects empty selected ssh default option",
			cfg: InventoryConfig{
				Providers: map[string]InventoryProviderConfig{
					"nre-netlab01": {
						Type: ProviderContainerlab,
						Config: InventoryProviderDetailConfig{
							JumpHost:    "nre@nre-netlab01.example.com",
							SSHDefaults: NewSSHDefaultsInheritanceOptions("SetEnv", ""),
						},
						Groups: map[string]GroupConfig{"lab": {}},
					},
				},
			},
			wantErr: "non-empty option names",
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
	cfg.Inventory.Provider = nil
	localProvider := cfg.Inventory.Providers["local"]
	localProvider.Groups["default"] = GroupConfig{Auth: InventoryAuthConfig{CredentialProvider: "missing", PasswordRef: "nssh/groups/default"}}
	cfg.Inventory.Providers["local"] = localProvider

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
	path := filepath.Join(tmp, "config.yaml")
	writeConfigFile(t, path, `
inventory:
  providers:
    local:
      type: local
      groups:
        corp: {}
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := cfg.Inventory.Providers["local"].Groups["default"]; ok {
		t.Fatalf("unexpected default group retained: %+v", cfg.Inventory.Providers["local"].Groups["default"])
	}
}
