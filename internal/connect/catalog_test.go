package connect

import (
	"reflect"
	"strings"
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

func TestCatalogMergesHighlightDefaultsGroupAndProviderOverlay(t *testing.T) {
	enabled := true
	disabled := false
	cfg := config.DefaultConfig()
	cfg.Highlight = config.HighlightConfig{Profile: config.HighlightProfileNone}
	cfg.Inventory.Provider = nil
	cfg.Inventory.Providers = map[string]config.InventoryProviderConfig{
		"netbox-prod": {
			Type: config.ProviderNetBox,
			Groups: map[string]config.GroupConfig{
				"core": {
					Highlight: config.HighlightConfig{Enabled: &enabled, Profile: config.HighlightProfileJunos},
				},
			},
			Hosts: map[string]config.InventoryHostConfig{
				"edge01.example.com": {
					Group: "core",
				},
				"edge02.example.com": {
					Group:     "core",
					Highlight: config.HighlightConfig{Enabled: &disabled},
				},
			},
		},
	}
	state := &inventory.ProviderState{
		Provider: "netbox-prod",
		Type:     config.ProviderNetBox,
		Objects: map[string]*inventory.ProviderHost{
			"1": {ObjectID: "1", Host: "edge01.example.com", HostName: "edge01.example.com", Group: "netbox-prod/core"},
			"2": {ObjectID: "2", Host: "edge02.example.com", HostName: "edge02.example.com", Group: "netbox-prod/core"},
		},
	}

	cat := buildCatalogForTest(t, cfg, []*inventory.ProviderState{state})
	inherited, ok := cat.Find("edge01.example.com")
	if !ok {
		t.Fatalf("Find(edge01.example.com) failed")
	}
	if inherited.Highlight.Enabled == nil || !*inherited.Highlight.Enabled || inherited.Highlight.Profile != config.HighlightProfileJunos {
		t.Fatalf("inherited highlight = %+v, want enabled junos", inherited.Highlight)
	}
	overridden, ok := cat.Find("edge02.example.com")
	if !ok {
		t.Fatalf("Find(edge02.example.com) failed")
	}
	if overridden.Highlight.Enabled == nil || *overridden.Highlight.Enabled {
		t.Fatalf("overridden highlight enabled = %v, want explicit false", overridden.Highlight.Enabled)
	}
	if overridden.Highlight.Profile != config.HighlightProfileJunos {
		t.Fatalf("overridden highlight profile = %q, want inherited junos", overridden.Highlight.Profile)
	}
}

func TestCatalogCarriesProviderStateProxyJump(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.SSH.Defaults = config.SSHHostConfig{Options: config.SSHOptions{
		"ControlPath":  config.NewSSHOptionString("~/.ssh/sockets/%r@%h:%p"),
		"IdentityFile": config.NewSSHOptionItems("~/.ssh/ed25519-1Password-Expedient.pub"),
	}}
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
				"juniper-crpd": {
					Auth: config.InventoryAuthConfig{Mode: config.AuthModePassword, Username: "admin"},
					SSH: config.SSHHostConfig{Options: config.SSHOptions{
						"LogLevel": config.NewSSHOptionString("DEBUG"),
					}},
				},
			},
		},
	}
	state := &inventory.ProviderState{
		Provider: "nre-netlab01",
		Type:     config.ProviderContainerlab,
		// Containerlab targets are ephemeral; the provider policy should be
		// rendered into the SSH options used for the final target.
		StrictHostKeyChecking: false,
		Objects: map[string]*inventory.ProviderHost{
			"dfz/core01": {
				ObjectID:  "dfz/core01",
				Host:      "clab-dfz-core01",
				HostName:  "172.20.20.13",
				Patterns:  []string{"clab-dfz-core01"},
				Group:     "nre-netlab01/juniper-crpd",
				ProxyJump: "nre@nre-netlab01.custcbb.local",
			},
		},
	}

	cat := buildCatalogForTest(t, cfg, []*inventory.ProviderState{state})
	host, ok := cat.Find("clab-dfz-core01")
	if !ok {
		t.Fatalf("Find(clab-dfz-core01) failed")
	}
	if hasSSHOption(host.SSH.Options, "ProxyJump") {
		t.Fatalf("ProxyJump should be replaced by managed ProxyCommand: %#v", host.SSH.Options)
	}
	got := host.SSH.Options["ProxyCommand"].StringValue()
	for _, want := range []string{
		"ssh -F none",
		"ControlPath=~/.ssh/sockets/%%r@%%h:%%p",
		"IdentityFile=~/.ssh/ed25519-1Password-Expedient.pub",
		"-W %h:%p",
		"nre@nre-netlab01.custcbb.local",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("ProxyCommand = %q, want substring %q", got, want)
		}
	}
	if got := host.SSH.Options["StrictHostKeyChecking"].StringValue(); got != "no" {
		t.Fatalf("StrictHostKeyChecking = %q, want no", got)
	}
	if got := host.SSH.Options["UserKnownHostsFile"].StringValue(); got != "/dev/null" {
		t.Fatalf("UserKnownHostsFile = %q, want /dev/null", got)
	}
	if got := host.SSH.Options["GlobalKnownHostsFile"].StringValue(); got != "/dev/null" {
		t.Fatalf("GlobalKnownHostsFile = %q, want /dev/null", got)
	}
	if got := host.SSH.Options["WarnWeakCrypto"].StringValue(); got != "no-pq-kex" {
		t.Fatalf("WarnWeakCrypto = %q, want no-pq-kex", got)
	}
	if got := host.SSH.Options["LogLevel"].StringValue(); got != "DEBUG" {
		t.Fatalf("LogLevel = %q, want DEBUG", got)
	}
	for _, targetOnly := range []string{"ControlPath", "IdentityFile"} {
		if hasSSHOption(host.SSH.Options, targetOnly) {
			t.Fatalf("containerlab target should not inherit global %s: %#v", targetOnly, host.SSH.Options)
		}
	}
}

func TestSplitProxyJumpTarget(t *testing.T) {
	tests := []struct {
		name     string
		target   string
		wantUser string
		wantHost string
		wantPort int
	}{
		{name: "host", target: "nre-netlab01", wantHost: "nre-netlab01"},
		{name: "user host", target: "nre@nre-netlab01.custcbb.local", wantUser: "nre", wantHost: "nre-netlab01.custcbb.local"},
		{name: "user host port", target: "nre@nre-netlab01.custcbb.local:2222", wantUser: "nre", wantHost: "nre-netlab01.custcbb.local", wantPort: 2222},
		{name: "ipv6 host port", target: "nre@[2001:db8::1]:2222", wantUser: "nre", wantHost: "2001:db8::1", wantPort: 2222},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotUser, gotHost, gotPort := splitProxyJumpTarget(tt.target)
			if gotUser != tt.wantUser || gotHost != tt.wantHost || gotPort != tt.wantPort {
				t.Fatalf("splitProxyJumpTarget(%q) = (%q, %q, %d), want (%q, %q, %d)", tt.target, gotUser, gotHost, gotPort, tt.wantUser, tt.wantHost, tt.wantPort)
			}
		})
	}
}

func TestCatalogContainerlabStrictFalseAppliesQuietDisposableHostOptions(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Inventory.Provider = nil
	cfg.Inventory.Providers = map[string]config.InventoryProviderConfig{
		"nre-netlab01": {
			Type: config.ProviderContainerlab,
			Groups: map[string]config.GroupConfig{
				"vjunos": {},
			},
		},
	}
	state := &inventory.ProviderState{
		Provider:              "nre-netlab01",
		Type:                  config.ProviderContainerlab,
		StrictHostKeyChecking: false,
		Objects: map[string]*inventory.ProviderHost{
			"isis/r1": {
				ObjectID: "isis/r1",
				Host:     "clab-isis-r1",
				HostName: "192.168.123.101",
				Patterns: []string{"isis"},
				Group:    "nre-netlab01/vjunos",
			},
		},
	}

	cat := buildCatalogForTest(t, cfg, []*inventory.ProviderState{state})
	host, ok := cat.Find("isis")
	if !ok {
		t.Fatalf("Find(isis) failed")
	}
	for key, want := range map[string]string{
		"StrictHostKeyChecking": "no",
		"UserKnownHostsFile":    "/dev/null",
		"GlobalKnownHostsFile":  "/dev/null",
		"LogLevel":              "ERROR",
		"WarnWeakCrypto":        "no-pq-kex",
	} {
		if got := host.SSH.Options[key].StringValue(); got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
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
