package connect

import (
	"reflect"
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/inventory"
)

func TestCatalogUsesProviderHostsAsOverlays(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Inventory.Provider = nil
	cfg.Inventory.Providers = map[string]config.InventoryProviderConfig{
		"netbox-prod": {
			Type: config.ProviderNetBox,
			Groups: map[string]config.GroupConfig{
				"cbb": {Auth: config.InventoryAuthConfig{Mode: config.AuthModePassword, Username: "chris.jones"}},
			},
			Hosts: map[string]config.InventoryHostConfig{
				"edge01.example.com": {Group: "cbb", Aliases: []string{"edge01"}},
			},
		},
	}
	state := &inventory.ProviderState{
		Provider: "netbox-prod",
		Type:     config.ProviderNetBox,
		Objects: map[string]*inventory.ProviderHost{
			"1": {ObjectID: "1", Host: "edge01.example.com", HostName: "edge01.example.com", Group: "netbox-prod/cbb"},
		},
	}
	cat := buildCatalogForTest(t, cfg, []*inventory.ProviderState{state})
	host, ok := cat.Find("edge01")
	if !ok {
		t.Fatalf("Find(edge01) failed")
	}
	if host.Provider != "netbox-prod" || host.Group != "cbb" || host.Username != "chris.jones" {
		t.Fatalf("host = %#v", host)
	}
}

func TestCatalogRequiresLocalHostGroup(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Inventory.Provider = nil
	cfg.Inventory.Providers = map[string]config.InventoryProviderConfig{
		config.ProviderLocal: {
			Type: config.ProviderLocal,
			Hosts: map[string]config.InventoryHostConfig{
				"rpi-a.lan": {},
			},
		},
	}
	_, err := BuildHostCatalog(cfg)
	if err == nil {
		t.Fatalf("BuildHostCatalog succeeded without local host group")
	}
}

func TestCatalogMergesSSHDefaultsGroupAndLocalHost(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.SSH.Defaults = config.SSHHostConfig{
		Options: config.SSHOptions{
			"IdentitiesOnly": config.NewSSHOptionBool(true),
			"IdentityAgent":  config.NewSSHOptionString("/tmp/agent.sock"),
			"IdentityFile":   config.NewSSHOptionItems("~/.ssh/default"),
			"SetEnv":         config.NewSSHOptionMap(map[string]string{"TERM": "xterm-256color"}),
			"KexAlgorithms":  config.NewSSHOptionItems("curve25519-sha256"),
		},
	}
	cfg.Inventory.Provider = nil
	cfg.Inventory.Providers = map[string]config.InventoryProviderConfig{
		config.ProviderLocal: {
			Type: config.ProviderLocal,
			Groups: map[string]config.GroupConfig{
				"lab": {
					SSH: config.SSHHostConfig{
						Options: config.SSHOptions{
							"ProxyJump":           config.NewSSHOptionString("jump01"),
							"SetEnv":              config.NewSSHOptionMap(map[string]string{"COLORTERM": "truecolor"}),
							"ServerAliveCountMax": config.NewSSHOptionString("2"),
						},
					},
				},
			},
			Hosts: map[string]config.InventoryHostConfig{
				"rpi-a.lan": {
					Group:   "lab",
					Aliases: []string{"rpi-a"},
					SSH: config.SSHHostConfig{
						Options: config.SSHOptions{
							"IdentityFile":  config.NewSSHOptionItems("~/.ssh/rpi-a"),
							"KexAlgorithms": config.NewSSHOptionItems("diffie-hellman-group14-sha1"),
							"LogLevel":      config.NewSSHOptionString("ERROR"),
						},
					},
				},
			},
		},
	}

	cat := buildCatalogForTest(t, cfg, nil)
	host, ok := cat.Find("rpi-a")
	if !ok {
		t.Fatalf("Find(rpi-a) failed")
	}
	want := config.SSHOptions{
		"IdentitiesOnly":      config.NewSSHOptionBool(true),
		"IdentityAgent":       config.NewSSHOptionString("/tmp/agent.sock"),
		"IdentityFile":        config.NewSSHOptionItems("~/.ssh/default", "~/.ssh/rpi-a"),
		"KexAlgorithms":       config.NewSSHOptionItems("diffie-hellman-group14-sha1"),
		"LogLevel":            config.NewSSHOptionString("ERROR"),
		"ProxyJump":           config.NewSSHOptionString("jump01"),
		"ServerAliveCountMax": config.NewSSHOptionString("2"),
		"SetEnv":              config.NewSSHOptionMap(map[string]string{"TERM": "xterm-256color", "COLORTERM": "truecolor"}),
	}
	if !reflect.DeepEqual(host.SSH.Options, want) {
		t.Fatalf("Options = %#v", host.SSH.Options)
	}
}

func TestCatalogUsesHostKeyAsResolvedHostname(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Inventory.Provider = nil
	cfg.Inventory.Providers = map[string]config.InventoryProviderConfig{
		config.ProviderLocal: {
			Type:   config.ProviderLocal,
			Groups: map[string]config.GroupConfig{"lab": {}},
			Hosts: map[string]config.InventoryHostConfig{
				"edge01.example.com": {Group: "lab", Aliases: []string{"edge01"}},
			},
		},
	}

	cat := buildCatalogForTest(t, cfg, nil)
	host, ok := cat.Find("edge01")
	if !ok {
		t.Fatalf("Find(edge01) failed")
	}
	if host.Hostname != "edge01.example.com" {
		t.Fatalf("hostname = %q, want edge01.example.com", host.Hostname)
	}
}

func TestCatalogLeavesHostNameOverrideInSSHOptions(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Inventory.Provider = nil
	cfg.Inventory.Providers = map[string]config.InventoryProviderConfig{
		config.ProviderLocal: {
			Type:   config.ProviderLocal,
			Groups: map[string]config.GroupConfig{"lab": {}},
			Hosts: map[string]config.InventoryHostConfig{
				"console-sw1": {
					Group: "lab",
					SSH: config.SSHHostConfig{Options: config.SSHOptions{
						"HostName": config.NewSSHOptionString("192.0.2.10"),
					}},
				},
			},
		},
	}

	cat := buildCatalogForTest(t, cfg, nil)
	host, ok := cat.Find("console-sw1")
	if !ok {
		t.Fatalf("Find(console-sw1) failed")
	}
	if host.Hostname != "console-sw1" {
		t.Fatalf("hostname = %q, want console-sw1", host.Hostname)
	}
	if got := host.SSH.Options["HostName"].Scalar; got != "192.0.2.10" {
		t.Fatalf("HostName option = %q, want 192.0.2.10", got)
	}
}

func TestCatalogMergesSSHDefaultsGroupAndProviderOverlay(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.SSH.Defaults = config.SSHHostConfig{
		Options: config.SSHOptions{
			"IdentityAgent": config.NewSSHOptionString("/tmp/agent.sock"),
			"TCPKeepAlive":  config.NewSSHOptionBool(true),
		},
	}
	cfg.Inventory.Provider = nil
	cfg.Inventory.Providers = map[string]config.InventoryProviderConfig{
		"netbox-prod": {
			Type: config.ProviderNetBox,
			Groups: map[string]config.GroupConfig{
				"cbb": {
					Auth: config.InventoryAuthConfig{Mode: config.AuthModePassword, Username: "chris.jones"},
					SSH: config.SSHHostConfig{
						Options: config.SSHOptions{
							"MACs":     config.NewSSHOptionItems("hmac-sha2-512-etm@openssh.com"),
							"LogLevel": config.NewSSHOptionString("ERROR"),
						},
					},
				},
			},
			Hosts: map[string]config.InventoryHostConfig{
				"edge01.example.com": {
					Group:   "cbb",
					Aliases: []string{"edge01"},
					SSH: config.SSHHostConfig{
						Options: config.SSHOptions{
							"ProxyCommand": config.NewSSHOptionString("ssh jump nc %h %p"),
							"LogLevel":     config.NewSSHOptionString("DEBUG"),
						},
					},
				},
			},
		},
	}
	state := &inventory.ProviderState{
		Provider: "netbox-prod",
		Type:     config.ProviderNetBox,
		Objects: map[string]*inventory.ProviderHost{
			"1": {ObjectID: "1", Host: "edge01.example.com", HostName: "edge01.example.com", Group: "netbox-prod/cbb"},
		},
	}

	cat := buildCatalogForTest(t, cfg, []*inventory.ProviderState{state})
	host, ok := cat.Find("edge01")
	if !ok {
		t.Fatalf("Find(edge01) failed")
	}
	want := config.SSHOptions{
		"IdentityAgent": config.NewSSHOptionString("/tmp/agent.sock"),
		"LogLevel":      config.NewSSHOptionString("DEBUG"),
		"MACs":          config.NewSSHOptionItems("hmac-sha2-512-etm@openssh.com"),
		"ProxyCommand":  config.NewSSHOptionString("ssh jump nc %h %p"),
		"TCPKeepAlive":  config.NewSSHOptionBool(true),
	}
	if !reflect.DeepEqual(host.SSH.Options, want) {
		t.Fatalf("Options = %#v", host.SSH.Options)
	}
}

func TestCatalogCarriesProviderStateProxyJump(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Inventory.Provider = nil
	cfg.Inventory.Providers = map[string]config.InventoryProviderConfig{
		config.ProviderLocal: {
			Type: config.ProviderLocal,
			Groups: map[string]config.GroupConfig{
				"custcbb": {},
			},
			Hosts: map[string]config.InventoryHostConfig{
				"nre-netlab01.custcbb.local": {
					Group:   "custcbb",
					Aliases: []string{"nre-netlab01"},
					Auth: config.InventoryAuthConfig{
						Mode:     config.AuthModeKey,
						Username: "nre",
					},
				},
			},
		},
		"nre-netlab01": {
			Type: config.ProviderContainerlab,
			Groups: map[string]config.GroupConfig{
				"juniper-crpd": {Auth: config.InventoryAuthConfig{Mode: config.AuthModePassword, Username: "admin"}},
			},
		},
	}
	state := &inventory.ProviderState{
		Provider: "nre-netlab01",
		Type:     config.ProviderContainerlab,
		Objects: map[string]*inventory.ProviderHost{
			"dfz/core01": {
				ObjectID:  "dfz/core01",
				Host:      "clab-dfz-core01",
				HostName:  "172.20.20.13",
				Patterns:  []string{"clab-dfz-core01"},
				Group:     "nre-netlab01/juniper-crpd",
				ProxyJump: "nre-netlab01",
			},
		},
	}

	cat := buildCatalogForTest(t, cfg, []*inventory.ProviderState{state})
	host, ok := cat.Find("clab-dfz-core01")
	if !ok {
		t.Fatalf("Find(clab-dfz-core01) failed")
	}
	if got := host.SSH.Options["ProxyJump"].StringValue(); got != "nre@nre-netlab01.custcbb.local" {
		t.Fatalf("ProxyJump = %q, want nre@nre-netlab01.custcbb.local", got)
	}
}

func TestCatalogUsesProviderHostAsCanonicalAndSearchesAliases(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Inventory.Provider = nil
	cfg.Inventory.Providers = map[string]config.InventoryProviderConfig{
		"nre-netlab01": {
			Type: config.ProviderContainerlab,
			Groups: map[string]config.GroupConfig{
				"juniper-crpd": {},
			},
			Hosts: map[string]config.InventoryHostConfig{
				"clab-dfz-core01": {
					Group:   "juniper-crpd",
					Aliases: []string{"dfz-core"},
				},
			},
		},
	}
	state := &inventory.ProviderState{
		Provider: "nre-netlab01",
		Type:     config.ProviderContainerlab,
		Objects: map[string]*inventory.ProviderHost{
			"dfz/core01": {
				ObjectID: "dfz/core01",
				Host:     "clab-dfz-core01",
				HostName: "172.20.20.13",
				Patterns: []string{"clab-dfz-core01", "dfz-core01"},
				Group:    "nre-netlab01/juniper-crpd",
			},
		},
	}

	cat := buildCatalogForTest(t, cfg, []*inventory.ProviderState{state})
	host, ok := cat.Find("clab-dfz-core01")
	if !ok {
		t.Fatalf("Find(clab-dfz-core01) failed")
	}
	if host.Canonical != "clab-dfz-core01" {
		t.Fatalf("canonical = %q, want clab-dfz-core01", host.Canonical)
	}
	if host.Hostname != "172.20.20.13" {
		t.Fatalf("hostname = %q, want 172.20.20.13", host.Hostname)
	}

	addressMatch, ok := cat.Find("172.20.20.13")
	if !ok {
		t.Fatalf("Find(172.20.20.13) failed")
	}
	if addressMatch.Canonical != "clab-dfz-core01" {
		t.Fatalf("address canonical = %q, want clab-dfz-core01", addressMatch.Canonical)
	}

	for query, want := range map[string][]string{
		"clab":   {"clab-dfz-core01"},
		"172.20": {"clab-dfz-core01"},
		"dfz":    {"clab-dfz-core01"},
	} {
		if got := cat.Suggestions(query); !reflect.DeepEqual(got, want) {
			t.Fatalf("Suggestions(%q) = %#v, want %#v", query, got, want)
		}
	}
}

func buildCatalogForTest(t *testing.T, cfg *config.Config, states []*inventory.ProviderState) *HostCatalog {
	t.Helper()
	cat, err := buildHostCatalog(cfg, states)
	if err != nil {
		t.Fatalf("buildHostCatalog: %v", err)
	}
	return cat
}
