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

func TestCatalogAllowsLocalHostWithoutGroup(t *testing.T) {
	cfg := config.DefaultConfig()
	enabled := true
	cfg.Inventory.Provider = nil
	cfg.Inventory.Providers = map[string]config.InventoryProviderConfig{
		config.ProviderLocal: {
			Type: config.ProviderLocal,
			Auth: config.InventoryAuthConfig{Mode: config.AuthModePassword, Username: "provider-user"},
			Groups: map[string]config.GroupConfig{
				"local": {
					Auth: config.InventoryAuthConfig{Username: "group-user"},
					SSH: config.SSHHostConfig{Options: config.SSHOptions{
						"ProxyJump": config.NewSSHOptionString("jump01"),
					}},
					Highlight: config.HighlightConfig{Enabled: &enabled, Profile: config.HighlightProfileJunos},
				},
			},
			Hosts: map[string]config.InventoryHostConfig{
				"rpi-a.lan": {
					Aliases: []string{"rpi-a"},
					Auth:    config.InventoryAuthConfig{Username: "host-user"},
					SSH: config.SSHHostConfig{Options: config.SSHOptions{
						"LogLevel": config.NewSSHOptionString("ERROR"),
					}},
				},
			},
		},
	}

	cat := buildCatalogForTest(t, cfg, nil)
	host, ok := cat.Find("rpi-a")
	if !ok {
		t.Fatalf("Find(rpi-a) failed")
	}
	if host.Group != "" {
		t.Fatalf("group = %q, want empty", host.Group)
	}
	if host.Username != "host-user" {
		t.Fatalf("username = %q, want host-user", host.Username)
	}
	if got := host.SSH.Options["LogLevel"].StringValue(); got != "ERROR" {
		t.Fatalf("LogLevel = %q, want ERROR", got)
	}
	if hasSSHOption(host.SSH.Options, "ProxyJump") {
		t.Fatalf("ungrouped host inherited group SSH options: %#v", host.SSH.Options)
	}
	if host.Highlight.Enabled != nil || host.Highlight.Profile != "" {
		t.Fatalf("ungrouped host inherited group highlight: %#v", host.Highlight)
	}
}

func TestCatalogResolvesLocalInventoryProxyAfterCatalogBuild(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Inventory.Provider = nil
	cfg.Inventory.Providers = map[string]config.InventoryProviderConfig{
		config.ProviderLocal: {
			Type: config.ProviderLocal,
			Groups: map[string]config.GroupConfig{
				"custcbb": {},
			},
			Hosts: map[string]config.InventoryHostConfig{
				"810-neteng01.custcbb.local": {
					Group:   "custcbb",
					Aliases: []string{"810-neteng01"},
					Auth:    config.InventoryAuthConfig{Mode: config.AuthModeKey, Username: "netops"},
					SSH: config.SSHHostConfig{Options: config.SSHOptions{
						"IdentityFile": config.NewSSHOptionString("~/.ssh/netops.pub"),
					}},
				},
				"pla-ts01.custcbb.local": {
					Group: "custcbb",
					SSH: config.SSHHostConfig{Options: config.SSHOptions{
						"ProxyJump": config.NewSSHOptionString("ops@810-neteng01:2200"),
					}},
				},
			},
		},
	}

	cat := buildCatalogForTest(t, cfg, nil)
	target, ok := cat.Find("pla-ts01")
	if !ok {
		t.Fatal("Find(pla-ts01) failed")
	}
	if target.ManagedProxy == nil {
		t.Fatal("target has no managed proxy")
	}
	if !hasSSHOption(target.SSH.Options, "ProxyJump") || hasSSHOption(target.SSH.Options, "ProxyCommand") {
		t.Fatalf("catalog should retain native ProxyJump until credential resolution: %#v", target.SSH.Options)
	}
	if target.ManagedProxy.Username != "ops" || target.ManagedProxy.Port != 2200 {
		t.Fatalf("managed proxy override = user %q port %d", target.ManagedProxy.Username, target.ManagedProxy.Port)
	}
	resolved, err := resolveCatalogHostForConnect("pla-ts01", "", cfg, target)
	if err != nil {
		t.Fatalf("resolveCatalogHostForConnect: %v", err)
	}
	if hasSSHOption(resolved.SSH.Options, "ProxyJump") {
		t.Fatalf("resolved target retained ProxyJump: %#v", resolved.SSH.Options)
	}
	command := resolved.SSH.Options["ProxyCommand"].StringValue()
	for _, want := range []string{"IdentityFile=~/.ssh/netops.pub", "-W %h:%p", "ops@810-neteng01.custcbb.local:2200"} {
		if !strings.Contains(command, want) {
			t.Fatalf("ProxyCommand = %q, want %q", command, want)
		}
	}

	proxy, ok := cat.Find("810-neteng01")
	if !ok {
		t.Fatal("Find(810-neteng01) failed")
	}
	if proxy.Username != "netops" || proxy.Port != 22 {
		t.Fatalf("direct proxy mutated by target override: user=%q port=%d", proxy.Username, proxy.Port)
	}
}

func TestCatalogManagedProxyResolutionIsProviderStateOrderIndependent(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Inventory.Provider = nil
	cfg.Inventory.Providers = map[string]config.InventoryProviderConfig{
		"targets": {Type: config.ProviderNetBox},
		"proxies": {Type: config.ProviderNetBox, Auth: config.InventoryAuthConfig{Mode: config.AuthModeKey, Username: "netops"}},
	}
	targetState := &inventory.ProviderState{
		Provider: "targets",
		Type:     config.ProviderNetBox,
		Objects: map[string]*inventory.ProviderHost{
			"edge01": {Host: "edge01.example", HostName: "edge01.example", Patterns: []string{"edge01"}, ProxyJump: "ops@jump01.example:2200"},
		},
	}
	proxyState := &inventory.ProviderState{
		Provider: "proxies",
		Type:     config.ProviderNetBox,
		Objects: map[string]*inventory.ProviderHost{
			"jump01": {Host: "jump01.example", HostName: "jump01.example", Patterns: []string{"jump01"}, Port: 22},
		},
	}

	for _, tc := range []struct {
		name   string
		states []*inventory.ProviderState
	}{
		{name: "target before proxy", states: []*inventory.ProviderState{targetState, proxyState}},
		{name: "proxy before target", states: []*inventory.ProviderState{proxyState, targetState}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cat := buildCatalogForTest(t, cfg, tc.states)
			target, ok := cat.Find("edge01")
			if !ok || target.ManagedProxy == nil {
				t.Fatalf("managed target = %#v found=%v", target, ok)
			}
			resolved, err := resolveCatalogHostForConnect("edge01", "", cfg, target)
			if err != nil {
				t.Fatalf("resolveCatalogHostForConnect: %v", err)
			}
			if command := resolved.SSH.Options["ProxyCommand"].StringValue(); !strings.Contains(command, "ops@jump01.example:2200") {
				t.Fatalf("ProxyCommand = %q", command)
			}
			proxy, ok := cat.Find("jump01")
			if !ok || proxy.Username != "netops" || proxy.Port != 22 {
				t.Fatalf("direct proxy mutated: %#v found=%v", proxy, ok)
			}
		})
	}
}

func TestCatalogLeavesUnmanagedAndNestedProxyJumpsNative(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Inventory.Provider = nil
	cfg.Inventory.Providers = map[string]config.InventoryProviderConfig{
		config.ProviderLocal: {
			Type:   config.ProviderLocal,
			Groups: map[string]config.GroupConfig{"lab": {}},
			Hosts: map[string]config.InventoryHostConfig{
				"nested-proxy": {
					Group: "lab",
					SSH: config.SSHHostConfig{Options: config.SSHOptions{
						"ProxyJump": config.NewSSHOptionString("outer-proxy"),
					}},
				},
				"nested-target": {
					Group: "lab",
					SSH: config.SSHHostConfig{Options: config.SSHOptions{
						"ProxyJump": config.NewSSHOptionString("nested-proxy"),
					}},
				},
				"multi-target": {
					Group: "lab",
					SSH: config.SSHHostConfig{Options: config.SSHOptions{
						"ProxyJump": config.NewSSHOptionString("jump-a,jump-b"),
					}},
				},
			},
		},
	}

	cat := buildCatalogForTest(t, cfg, nil)
	for host, want := range map[string]string{
		"nested-target": "nested-proxy",
		"multi-target":  "jump-a,jump-b",
	} {
		got, ok := cat.Find(host)
		if !ok {
			t.Fatalf("Find(%s) failed", host)
		}
		if got.ManagedProxy != nil {
			t.Fatalf("%s unexpectedly managed proxy %#v", host, got.ManagedProxy)
		}
		if value := got.SSH.Options["ProxyJump"].StringValue(); value != want {
			t.Fatalf("%s ProxyJump = %q, want %q", host, value, want)
		}
	}
}

func TestCatalogRejectsUnknownLocalHostGroup(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Inventory.Provider = nil
	cfg.Inventory.Providers = map[string]config.InventoryProviderConfig{
		config.ProviderLocal: {
			Type: config.ProviderLocal,
			Groups: map[string]config.GroupConfig{
				"lab": {},
			},
			Hosts: map[string]config.InventoryHostConfig{
				"rpi-a.lan": {Group: "missing"},
			},
		},
	}
	_, err := buildHostCatalog(cfg, nil)
	if err == nil {
		t.Fatalf("buildHostCatalog succeeded with unknown local host group")
	}
	if !strings.Contains(err.Error(), `group references unknown group "missing"`) {
		t.Fatalf("error = %v", err)
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

func TestCatalogPasswordAuthForcesPasswordOnlySSHOptions(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Inventory.Provider = nil
	cfg.Inventory.Providers = map[string]config.InventoryProviderConfig{
		config.ProviderLocal: {
			Type: config.ProviderLocal,
			Groups: map[string]config.GroupConfig{
				"lab": {
					Auth: config.InventoryAuthConfig{Mode: config.AuthModePassword},
					SSH: config.SSHHostConfig{Options: config.SSHOptions{
						"IdentityFile": config.NewSSHOptionItems("~/.ssh/lab.pub"),
					}},
				},
			},
			Hosts: map[string]config.InventoryHostConfig{
				"edge01.lab": {Group: "lab", Aliases: []string{"edge01"}},
			},
		},
	}

	cat := buildCatalogForTest(t, cfg, nil)
	host, ok := cat.Find("edge01")
	if !ok {
		t.Fatalf("Find(edge01) failed")
	}
	pubkey := host.SSH.Options["PubkeyAuthentication"]
	if pubkey.Bool == nil || *pubkey.Bool {
		t.Fatalf("PubkeyAuthentication = %#v, want false", pubkey)
	}
	if got := host.SSH.Options["PreferredAuthentications"].Scalar; got != "keyboard-interactive,password" {
		t.Fatalf("PreferredAuthentications = %q, want keyboard-interactive,password", got)
	}
	if got := host.SSH.Options["IdentityFile"].StringValue(); got != "~/.ssh/lab.pub" {
		t.Fatalf("IdentityFile = %q, want preserved key path", got)
	}
}

func TestCatalogKeyAuthDoesNotForcePasswordOnlySSHOptions(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Inventory.Provider = nil
	cfg.Inventory.Providers = map[string]config.InventoryProviderConfig{
		config.ProviderLocal: {
			Type: config.ProviderLocal,
			Groups: map[string]config.GroupConfig{
				"lab": {Auth: config.InventoryAuthConfig{Mode: config.AuthModeKey}},
			},
			Hosts: map[string]config.InventoryHostConfig{
				"edge01.lab": {Group: "lab", Aliases: []string{"edge01"}},
			},
		},
	}

	cat := buildCatalogForTest(t, cfg, nil)
	host, ok := cat.Find("edge01")
	if !ok {
		t.Fatalf("Find(edge01) failed")
	}
	if _, ok := host.SSH.Options["PubkeyAuthentication"]; ok {
		t.Fatalf("PubkeyAuthentication should not be forced for key auth: %#v", host.SSH.Options)
	}
	if _, ok := host.SSH.Options["PreferredAuthentications"]; ok {
		t.Fatalf("PreferredAuthentications should not be forced for key auth: %#v", host.SSH.Options)
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
		"IdentityAgent":            config.NewSSHOptionString("/tmp/agent.sock"),
		"LogLevel":                 config.NewSSHOptionString("DEBUG"),
		"MACs":                     config.NewSSHOptionItems("hmac-sha2-512-etm@openssh.com"),
		"PreferredAuthentications": config.NewSSHOptionString("keyboard-interactive,password"),
		"ProxyCommand":             config.NewSSHOptionString("ssh jump nc %h %p"),
		"PubkeyAuthentication":     config.NewSSHOptionBool(false),
		"TCPKeepAlive":             config.NewSSHOptionBool(true),
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
			Config: config.InventoryProviderDetailConfig{
				SSHDefaults: config.NewSSHDefaultsInheritanceMode("none"),
			},
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
	if !hasSSHOption(host.SSH.Options, "ProxyJump") || host.ManagedProxy == nil {
		t.Fatalf("catalog did not retain and resolve ProxyJump: %#v", host.SSH.Options)
	}
	resolved, err := resolveCatalogHostForConnect("clab-dfz-core01", "", cfg, host)
	if err != nil {
		t.Fatalf("resolveCatalogHostForConnect: %v", err)
	}
	got := resolved.SSH.Options["ProxyCommand"].StringValue()
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

func TestCatalogContainerlabCanInheritSelectedDefaultSSHOptions(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.SSH.Defaults = config.SSHHostConfig{Options: config.SSHOptions{
		"ControlPath": config.NewSSHOptionString("~/.ssh/sockets/%r@%h:%p"),
		"SetEnv": config.NewSSHOptionMap(map[string]string{
			"COLORTERM": "truecolor",
			"TERM":      "xterm-256color",
		}),
	}}
	cfg.Inventory.Provider = nil
	cfg.Inventory.Providers = map[string]config.InventoryProviderConfig{
		"nre-netlab01": {
			Type: config.ProviderContainerlab,
			Config: config.InventoryProviderDetailConfig{
				SSHDefaults: config.NewSSHDefaultsInheritanceOptions("SetEnv"),
			},
			Groups: map[string]config.GroupConfig{
				"vjunos": {},
			},
		},
	}
	state := &inventory.ProviderState{
		Provider: "nre-netlab01",
		Type:     config.ProviderContainerlab,
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
	if got := host.SSH.Options["SetEnv"].StringValue(); got != "COLORTERM=truecolor TERM=xterm-256color" {
		t.Fatalf("SetEnv = %q, want COLORTERM=truecolor TERM=xterm-256color", got)
	}
	if hasSSHOption(host.SSH.Options, "ControlPath") {
		t.Fatalf("containerlab target should not inherit unselected ControlPath: %#v", host.SSH.Options)
	}
}

func TestCatalogContainerlabInheritsAllSSHDefaultsByDefault(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.SSH.Defaults = config.SSHHostConfig{
		Compatibility: config.SSHCompatibility{Kex: "diffie-hellman-group14-sha1"},
		Options: config.SSHOptions{
			"ControlPath": config.NewSSHOptionString("~/.ssh/sockets/%r@%h:%p"),
			"SetEnv":      config.NewSSHOptionMap(map[string]string{"TERM": "xterm-256color"}),
		},
	}
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
		Provider: "nre-netlab01",
		Type:     config.ProviderContainerlab,
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
	if got := host.SSH.Compatibility.Kex; got != "diffie-hellman-group14-sha1" {
		t.Fatalf("Kex = %q, want diffie-hellman-group14-sha1", got)
	}
	for _, key := range []string{"ControlPath", "SetEnv"} {
		if !hasSSHOption(host.SSH.Options, key) {
			t.Fatalf("containerlab target should inherit %s by default: %#v", key, host.SSH.Options)
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
