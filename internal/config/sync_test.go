package config

import (
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestSyncConfigDecode(t *testing.T) {
	const input = `
[[sync.sources]]
name = "nre-netlab01"
provider = "containerlab"

  [sync.sources.containerlab]
  jump_host = "nre-netlab01"
  sudo = true
  strict_host_key_checking = false

  [[sync.sources.routes]]
  name = "clab-default"
  context = "lab"

    [sync.sources.routes.match]
    kind = ["ceos", "vjunos"]
    state = ["running"]

[[sync.sources]]
name = "netbox-prod"
provider = "netbox"

  [sync.sources.netbox]
  url_env = "NETBOX_URL"
  token_env = "NETBOX_TOKEN"
  env_file = "~/.env"

  [[sync.sources.routes]]
  name = "custcbb-juniper"
  context = "custcbb"

    [sync.sources.routes.match]
    manufacturer = ["Juniper"]
    status = ["active"]

  [[sync.sources.routes]]
  name = "expedient-arista"
  context = "expedient"

    [sync.sources.routes.match]
    manufacturer = ["Arista"]
`

	var cfg struct {
		Sync SyncConfig `toml:"sync"`
	}
	if _, err := toml.Decode(input, &cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(cfg.Sync.Sources) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(cfg.Sync.Sources))
	}

	// Containerlab source
	clab := cfg.Sync.Sources[0]
	if clab.Name != "nre-netlab01" {
		t.Errorf("source[0].Name = %q, want nre-netlab01", clab.Name)
	}
	if clab.Provider != "containerlab" {
		t.Errorf("source[0].Provider = %q, want containerlab", clab.Provider)
	}
	if clab.Containerlab == nil {
		t.Fatal("source[0].Containerlab is nil")
	}
	if clab.Containerlab.JumpHost != "nre-netlab01" {
		t.Errorf("jump_host = %q, want nre-netlab01", clab.Containerlab.JumpHost)
	}
	if !clab.Containerlab.Sudo {
		t.Error("sudo should be true")
	}
	if len(clab.Routes) != 1 {
		t.Fatalf("source[0] routes: got %d, want 1", len(clab.Routes))
	}
	if clab.Routes[0].Context != "lab" {
		t.Errorf("route context = %q, want lab", clab.Routes[0].Context)
	}
	kinds := clab.Routes[0].Match["kind"]
	if len(kinds) != 2 || kinds[0] != "ceos" || kinds[1] != "vjunos" {
		t.Errorf("match.kind = %v", kinds)
	}

	// NetBox source
	nb := cfg.Sync.Sources[1]
	if nb.Provider != "netbox" {
		t.Errorf("source[1].Provider = %q", nb.Provider)
	}
	if nb.NetBox == nil {
		t.Fatal("source[1].NetBox is nil")
	}
	if nb.NetBox.BaseURL != "" {
		t.Errorf("base_url = %q", nb.NetBox.BaseURL)
	}
	if nb.NetBox.URLEnv != "NETBOX_URL" {
		t.Errorf("url_env = %q", nb.NetBox.URLEnv)
	}
	if nb.NetBox.EnvFile != "~/.env" {
		t.Errorf("env_file = %q", nb.NetBox.EnvFile)
	}
	if len(nb.Routes) != 2 {
		t.Fatalf("source[1] routes: got %d, want 2", len(nb.Routes))
	}
}

func TestSyncConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     SyncConfig
		wantErr string
	}{
		{
			name: "empty sources ok",
			cfg:  SyncConfig{},
		},
		{
			name: "valid containerlab",
			cfg: SyncConfig{Sources: []SyncSourceConfig{{
				Name:         "lab1",
				Provider:     "containerlab",
				Containerlab: &ContainerlabConfig{JumpHost: "jumpbox"},
				Routes:       []SyncRouteConfig{{Context: "lab"}},
			}}},
		},
		{
			name: "valid netbox",
			cfg: SyncConfig{Sources: []SyncSourceConfig{{
				Name:     "nb1",
				Provider: "netbox",
				NetBox:   &NetBoxConfig{BaseURL: "https://nb.local"},
				Routes:   []SyncRouteConfig{{Context: "prod"}},
			}}},
		},
		{
			name: "missing name",
			cfg: SyncConfig{Sources: []SyncSourceConfig{{
				Provider:     "containerlab",
				Containerlab: &ContainerlabConfig{JumpHost: "j"},
				Routes:       []SyncRouteConfig{{Context: "c"}},
			}}},
			wantErr: "name is required",
		},
		{
			name: "name with path separator",
			cfg: SyncConfig{Sources: []SyncSourceConfig{{
				Name:         "../../etc/evil",
				Provider:     "containerlab",
				Containerlab: &ContainerlabConfig{JumpHost: "j"},
				Routes:       []SyncRouteConfig{{Context: "c"}},
			}}},
			wantErr: "must not contain",
		},
		{
			name: "duplicate name",
			cfg: SyncConfig{Sources: []SyncSourceConfig{
				{Name: "dup", Provider: "containerlab", Containerlab: &ContainerlabConfig{JumpHost: "j"}, Routes: []SyncRouteConfig{{Context: "c"}}},
				{Name: "dup", Provider: "containerlab", Containerlab: &ContainerlabConfig{JumpHost: "j"}, Routes: []SyncRouteConfig{{Context: "c"}}},
			}},
			wantErr: "duplicate source name",
		},
		{
			name: "unsupported provider",
			cfg: SyncConfig{Sources: []SyncSourceConfig{{
				Name:     "s1",
				Provider: "ansible",
				Routes:   []SyncRouteConfig{{Context: "c"}},
			}}},
			wantErr: "unsupported provider",
		},
		{
			name: "missing provider block",
			cfg: SyncConfig{Sources: []SyncSourceConfig{{
				Name:     "s1",
				Provider: "containerlab",
				Routes:   []SyncRouteConfig{{Context: "c"}},
			}}},
			wantErr: "containerlab config block is missing",
		},
		{
			name: "mixed provider blocks",
			cfg: SyncConfig{Sources: []SyncSourceConfig{{
				Name:         "s1",
				Provider:     "containerlab",
				Containerlab: &ContainerlabConfig{JumpHost: "j"},
				NetBox:       &NetBoxConfig{BaseURL: "u", TokenEnv: "t"},
				Routes:       []SyncRouteConfig{{Context: "c"}},
			}}},
			wantErr: "netbox config block is also present",
		},
		{
			name: "no routes",
			cfg: SyncConfig{Sources: []SyncSourceConfig{{
				Name:         "s1",
				Provider:     "containerlab",
				Containerlab: &ContainerlabConfig{JumpHost: "j"},
			}}},
			wantErr: "at least one route",
		},
		{
			name: "route missing context",
			cfg: SyncConfig{Sources: []SyncSourceConfig{{
				Name:         "s1",
				Provider:     "containerlab",
				Containerlab: &ContainerlabConfig{JumpHost: "j"},
				Routes:       []SyncRouteConfig{{Name: "r1"}},
			}}},
			wantErr: "context is required",
		},
		{
			name: "missing jump_host",
			cfg: SyncConfig{Sources: []SyncSourceConfig{{
				Name:         "s1",
				Provider:     "containerlab",
				Containerlab: &ContainerlabConfig{},
				Routes:       []SyncRouteConfig{{Context: "c"}},
			}}},
			wantErr: "jump_host is required",
		},
		{
			name: "netbox missing base_url",
			cfg: SyncConfig{Sources: []SyncSourceConfig{{
				Name:     "s1",
				Provider: "netbox",
				NetBox:   &NetBoxConfig{TokenEnv: "t"},
				Routes:   []SyncRouteConfig{{Context: "c"}},
			}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not contain %q", err, tt.wantErr)
			}
		})
	}
}
