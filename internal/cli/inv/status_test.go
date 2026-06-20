package inv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/inventory"
	"github.com/ntwrknrd/nssh/internal/ui"
)

func TestRenderStatusTreeShowsLocalProviderAndProviderGroups(t *testing.T) {
	t.Setenv("COLUMNS", "160")
	inventory.SetStateDir(t.TempDir())
	t.Cleanup(func() { inventory.SetStateDir("") })

	home := t.TempDir()
	t.Setenv("HOME", home)
	paths := &config.Paths{SSHConfigDir: filepath.Join(home, ".ssh")}
	configRoot := filepath.Join(home, ".config", "nssh")
	if err := os.MkdirAll(configRoot, 0700); err != nil {
		t.Fatal(err)
	}
	inventoryFile := filepath.Join(configRoot, "inventory.yaml")
	if err := os.WriteFile(inventoryFile, []byte(`
inventory:
  providers:
    local:
      type: local
      groups:
        edge: {}
        lab:
          match:
            domain_suffix: [.example.com]
          auth:
            credential_provider: pass
            password_ref: nssh/groups/lab
            username: local-admin
      hosts:
        local01.example.com:
          group: lab
          aliases: [local01]
        local02.example.com:
          group: lab
          aliases: [local02]
        local03.example.com:
          group: edge
          aliases: [local03]
    netbox-prod:
      type: netbox
      groups:
        customer:
          match:
            provider: [customer]
          auth:
            credential_provider: pass
            password_ref: nssh/groups/customer
            username: netbox-admin
`), 0600); err != nil {
		t.Fatal(err)
	}
	configFile := filepath.Join(configRoot, "config.yaml")
	if err := os.WriteFile(configFile, []byte(`
include: [inventory.yaml]
`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configFile)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	localProvider := cfg.Inventory.Providers[config.ProviderLocal]
	localProvider.Hosts = map[string]config.InventoryHostConfig{
		"local01.example.com": {Group: "lab", Aliases: []string{"local01"}},
		"local02.example.com": {Group: "lab", Aliases: []string{"local02"}},
		"local03.example.com": {Group: "edge", Aliases: []string{"local03"}},
	}
	cfg.Inventory.Providers[config.ProviderLocal] = localProvider
	cfg.Inventory.Provider = cfg.Inventory.Providers
	now := time.Date(2026, 5, 29, 2, 30, 0, 0, time.UTC)
	state := &inventory.ProviderState{
		Version:     inventory.StateVersion,
		Provider:    "netbox-prod",
		Type:        config.ProviderNetBox,
		LastRefresh: now.Add(-7 * time.Minute),
		IncludeFile: inventory.ProviderIncludeFile("netbox-prod"),
		LastError:   "api down",
		Objects: map[string]*inventory.ProviderHost{
			"device:1": {ObjectID: "device:1", Host: "edge01", HostName: "edge01.example.com", Group: "netbox-prod/customer"},
			"device:2": {ObjectID: "device:2", Host: "edge02", HostName: "edge02.example.com", Group: "netbox-prod/customer"},
		},
	}
	if err := inventory.SaveProviderState(state); err != nil {
		t.Fatal(err)
	}

	got, err := renderStatusTree(cfg, paths, "", now)
	if err != nil {
		t.Fatalf("renderStatusTree: %v", err)
	}
	text := ui.StripANSI(got)

	for _, want := range []string{
		"local (local)",
		"  output: ~/.config/nssh/inventory.yaml",
		"  hosts: 3 hosts",
		"  groups:",
		"    local/edge",
		"      hosts: 1 host",
		"      config: ~/.config/nssh/inventory.yaml",
		"      auth:",
		"        username: -",
		"        username ref: -",
		"        password ref: -",
		"    local/lab",
		"      hosts: 2 hosts",
		"      config: ~/.config/nssh/inventory.yaml",
		"        username: local-admin",
		"        username ref: -",
		"        password ref: nssh/groups/lab",
		"netbox-prod (netbox)",
		"  cache: 7m error",
		"  output: ~/.ssh/nssh.d/provider_netbox-prod.conf",
		"  hosts: 2 hosts",
		"    netbox-prod/customer",
		"      hosts: 2 hosts",
		"      config: ~/.config/nssh/inventory.yaml",
		"        username: netbox-admin",
		"        username ref: -",
		"        password ref: nssh/groups/customer",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("status tree missing %q:\n%s", want, text)
		}
	}
	for _, unexpected := range []string{
		filepath.Join(paths.SSHConfigDir, "nssh.d", "provider_local.conf"),
		inventoryFile,
		"        password: ",
		"        credential provider:",
		"Match",
		"suffix=.example.com",
		"provider=customer",
		"selectors",
		"  group membership",
		"  group selectors",
		"╭",
		"│",
		"╰",
	} {
		if strings.Contains(text, unexpected) {
			t.Fatalf("status tree should fold %q into groups:\n%s", unexpected, text)
		}
	}
}

func TestRenderStatusTreeShowsProviderDetailForNamedProvider(t *testing.T) {
	inventory.SetStateDir(t.TempDir())
	t.Cleanup(func() { inventory.SetStateDir("") })

	home := t.TempDir()
	t.Setenv("HOME", home)
	paths := &config.Paths{SSHConfigDir: filepath.Join(home, ".ssh")}
	configRoot := filepath.Join(home, ".config", "nssh")
	if err := os.MkdirAll(configRoot, 0700); err != nil {
		t.Fatal(err)
	}
	inventoryFile := filepath.Join(configRoot, "inventory.yaml")
	if err := os.WriteFile(inventoryFile, []byte(`
inventory:
  providers:
    netbox-prod:
      type: netbox
      groups:
        customer:
          match:
            provider: [customer]
          auth:
            credential_provider: pass
            password_ref: nssh/groups/customer
            username: netbox-admin
`), 0600); err != nil {
		t.Fatal(err)
	}
	configFile := filepath.Join(configRoot, "config.yaml")
	if err := os.WriteFile(configFile, []byte(`
include: [inventory.yaml]
`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configFile)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	now := time.Date(2026, 5, 29, 2, 30, 0, 0, time.UTC)
	state := &inventory.ProviderState{
		Version:     inventory.StateVersion,
		Provider:    "netbox-prod",
		Type:        config.ProviderNetBox,
		LastRefresh: now.Add(-7 * time.Minute),
		IncludeFile: inventory.ProviderIncludeFile("netbox-prod"),
		Objects: map[string]*inventory.ProviderHost{
			"device:1": {ObjectID: "device:1", Host: "edge01", HostName: "edge01.example.com", Group: "netbox-prod/customer"},
		},
	}
	if err := inventory.SaveProviderState(state); err != nil {
		t.Fatal(err)
	}

	got, err := renderStatusTree(cfg, paths, "netbox-prod", now)
	if err != nil {
		t.Fatalf("renderStatusTree: %v", err)
	}
	text := ui.StripANSI(got)

	for _, want := range []string{
		"netbox-prod (netbox)",
		"  cache: 7m old, 1 objects",
		"  output: ~/.ssh/nssh.d/provider_netbox-prod.conf",
		"  last error: -",
		"  hosts: 1 host",
		"  groups:",
		"    netbox-prod/customer",
		"      hosts: 1 host",
		"      config: ~/.config/nssh/inventory.yaml",
		"      output: ~/.ssh/nssh.d/provider_netbox-prod.conf",
		"      match:",
		"        provider: customer",
		"      auth:",
		"        mode: -",
		"        credential provider: pass",
		"        username: netbox-admin",
		"        username ref: -",
		"        password ref: nssh/groups/customer",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("status tree missing %q:\n%s", want, text)
		}
	}
	for _, unexpected := range []string{
		"  type: netbox",
		"  groups\n",
		"      match provider = customer",
		filepath.Join(paths.SSHConfigDir, "nssh.d", "provider_netbox-prod.conf"),
		inventoryFile,
	} {
		if strings.Contains(text, unexpected) {
			t.Fatalf("status tree should not contain old detail shape %q:\n%s", unexpected, text)
		}
	}
}

func TestRenderStatusTreeAddsSubtleANSIStylesWithoutChangingText(t *testing.T) {
	oldProfile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(oldProfile) })

	var b strings.Builder
	writeStatusProvider(&b, statusProviderSnapshot{
		Name:       "netbox-prod",
		Type:       config.ProviderNetBox,
		Cache:      "7m old, 1 objects",
		LastError:  "-",
		OutputFile: "/tmp/provider_netbox-prod.conf",
		Hosts:      1,
		Groups: []statusProviderGroup{{
			Name:       "netbox-prod/customer",
			ConfigFile: "/tmp/netbox-prod.yaml",
			OutputFile: "/tmp/provider_netbox-prod.conf",
			Hosts:      1,
			Match:      config.InventoryMatch{"provider": []string{"customer"}},
			Auth: inventoryAuthView{
				AuthMode:           "-",
				CredentialProvider: "pass",
				Username:           "netbox-admin",
				UsernameRef:        "-",
				PasswordRef:        "nssh/groups/customer",
			},
		}},
	}, true)

	got := b.String()
	if !strings.Contains(got, "\x1b[") {
		t.Fatalf("status tree should include ANSI styling:\n%q", got)
	}
	if styled := statusProviderName("netbox-prod"); !strings.Contains(styled, "\x1b[") || strings.Contains(styled, "\x1b[1") {
		t.Fatalf("provider name should be colored but not bold: %q", styled)
	}
	if styled := statusGroupName("netbox-prod/customer"); !strings.Contains(styled, "\x1b[") || strings.Contains(styled, "\x1b[1") {
		t.Fatalf("group name should be colored but not bold: %q", styled)
	}
	if styled := statusPath("~/.ssh/nssh.d/provider_netbox-prod.conf"); styled != "~/.ssh/nssh.d/provider_netbox-prod.conf" {
		t.Fatalf("paths should stay readable/plain, got %q", styled)
	}
	text := ui.StripANSI(got)
	for _, want := range []string{
		"netbox-prod (netbox)",
		"  cache: 7m old, 1 objects",
		"  groups:",
		"    netbox-prod/customer",
		"      match:",
		"      auth:",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("stripped status tree missing %q:\n%s", want, text)
		}
	}
}

func TestRenderStatusTreeOmitsMissingLocalProvider(t *testing.T) {
	inventory.SetStateDir(t.TempDir())
	t.Cleanup(func() { inventory.SetStateDir("") })

	paths := &config.Paths{SSHConfigDir: filepath.Join(t.TempDir(), ".ssh")}
	cfg := &config.Config{Inventory: config.InventoryConfig{
		Provider: map[string]config.InventoryProviderConfig{
			"netbox-prod": {
				Type:  config.ProviderNetBox,
				Group: map[string]config.GroupConfig{"customer": {}},
			},
		},
	}}

	got, err := renderStatusTree(cfg, paths, "", time.Date(2026, 5, 29, 2, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("renderStatusTree: %v", err)
	}
	text := ui.StripANSI(got)
	if strings.Contains(text, "provider_local.conf") || strings.Contains(text, "local\n") {
		t.Fatalf("missing local provider should not be shown:\n%s", text)
	}
	if !strings.Contains(text, "netbox-prod") {
		t.Fatalf("external provider missing:\n%s", text)
	}
}

func TestRenderStatusTreeShowsConfiguredLocalGroupsWithoutHosts(t *testing.T) {
	inventory.SetStateDir(t.TempDir())
	t.Cleanup(func() { inventory.SetStateDir("") })

	paths := &config.Paths{SSHConfigDir: filepath.Join(t.TempDir(), ".ssh")}
	cfg := &config.Config{Inventory: config.InventoryConfig{
		Provider: map[string]config.InventoryProviderConfig{
			config.ProviderLocal: {
				Type: config.ProviderLocal,
				Group: map[string]config.GroupConfig{
					"lab": {Match: config.InventoryMatch{"domain_suffix": []string{".example.com"}}},
				},
			},
		},
	}}

	got, err := renderStatusTree(cfg, paths, "local", time.Date(2026, 5, 29, 2, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("renderStatusTree: %v", err)
	}
	text := ui.StripANSI(got)
	for _, want := range []string{
		"local (local)",
		"  hosts: 0 hosts",
		"  groups:",
		"    local/lab",
		"      hosts: 0 hosts",
		"      match:",
		"        domain_suffix: .example.com",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("status tree missing %q:\n%s", want, text)
		}
	}
}

func TestRenderStatusTreeReportsUnsupportedStateVersionAsStaleCache(t *testing.T) {
	tmpState := t.TempDir()
	inventory.SetStateDir(tmpState)
	t.Cleanup(func() { inventory.SetStateDir("") })
	// Write directly to the test state dir so status sees an old cache schema.
	stateDir := filepath.Join(tmpState, "inventory", "providers")
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "netbox-prod.json"), []byte(`{"version":1,"provider":"netbox-prod","objects":{}}`), 0600); err != nil {
		t.Fatal(err)
	}

	paths := &config.Paths{SSHConfigDir: filepath.Join(t.TempDir(), ".ssh")}
	cfg := &config.Config{Inventory: config.InventoryConfig{
		Provider: map[string]config.InventoryProviderConfig{
			"netbox-prod": {Type: config.ProviderNetBox, Group: map[string]config.GroupConfig{"customer": {}}},
		},
	}}

	got, err := renderStatusTree(cfg, paths, "netbox-prod", time.Date(2026, 5, 29, 2, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("renderStatusTree: %v", err)
	}
	text := ui.StripANSI(got)
	if !strings.Contains(text, "  cache: stale, refresh required") {
		t.Fatalf("status tree missing stale cache message:\n%s", text)
	}
	if strings.Contains(text, "unsupported provider state version") {
		t.Fatalf("status tree leaked stale state as last error:\n%s", text)
	}
}

func TestRenderStatusTreeReportsLocalFindingsReadOnly(t *testing.T) {
	inventory.SetStateDir(t.TempDir())
	t.Cleanup(func() { inventory.SetStateDir("") })

	tmp := t.TempDir()
	paths := &config.Paths{ConfigDir: filepath.Join(tmp, "nssh"), ConfigFile: filepath.Join(tmp, "nssh", "config.yaml"), SSHConfigDir: filepath.Join(tmp, ".ssh")}
	cfg := &config.Config{Inventory: config.InventoryConfig{
		Providers: map[string]config.InventoryProviderConfig{
			config.ProviderLocal: {
				Type:   config.ProviderLocal,
				Groups: map[string]config.GroupConfig{"lab": {}},
				Hosts: map[string]config.InventoryHostConfig{
					"192.0.2.1": {Group: "lab", Aliases: []string{"edge01", "edge01-a"}},
					"192.0.2.2": {Group: "lab", Aliases: []string{"edge01", "edge01-b"}},
				},
			},
		},
	}}
	if err := saveLocalProviderInventory(cfg, paths); err != nil {
		t.Fatal(err)
	}
	localFile := localProviderYAMLPath(cfg, paths)
	before := readTestFile(t, localFile)

	got, err := renderStatusTree(cfg, paths, "local", time.Date(2026, 5, 29, 2, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("renderStatusTree: %v", err)
	}
	text := ui.StripANSI(got)
	for _, want := range []string{
		"  findings",
		"duplicate: pattern \"edge01\" also appears in " + localFile,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("status tree missing %q:\n%s", want, text)
		}
	}
	after := readTestFile(t, localFile)
	if after != before {
		t.Fatalf("status should not mutate local inventory:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}
