package inv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/inventory"
)

func TestRenderStatusTreeShowsLocalProviderAndProviderGroups(t *testing.T) {
	t.Setenv("COLUMNS", "160")
	inventory.SetStateDir(t.TempDir())
	t.Cleanup(func() { inventory.SetStateDir("") })

	paths := &config.Paths{SSHConfigDir: filepath.Join(t.TempDir(), ".ssh")}
	localFile := filepath.Join(paths.SSHConfigDir, "nssh.d", "provider_local.conf")
	if err := os.MkdirAll(filepath.Dir(localFile), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(localFile, []byte(""+
		"Host local01\n"+
		"  # Group: local/lab\n"+
		"  HostName local01.example.com\n"+
		"\n"+
		"Host local02\n"+
		"  # Group: local/lab\n"+
		"  HostName local02.example.com\n"+
		"\n"+
		"Host local03\n"+
		"  # Group: edge\n"+
		"  HostName local03.example.com\n"), 0600); err != nil {
		t.Fatal(err)
	}
	configRoot := t.TempDir()
	inventoryFile := filepath.Join(configRoot, "inventory.toml")
	if err := os.WriteFile(inventoryFile, []byte(`
[provider.local]
type = "local"

[provider.local.group.lab.match]
domain_suffix = [".example.com"]

[provider.local.group.lab.auth]
credential_provider = "pass-local"
password_ref = "nssh/groups/lab"
username = "local-admin"

[provider.netbox-prod]
type = "netbox"

[provider.netbox-prod.group.customer.match]
provider = ["customer"]

[provider.netbox-prod.group.customer.auth]
credential_provider = "pass-local"
password_ref = "nssh/groups/customer"
username = "netbox-admin"
`), 0600); err != nil {
		t.Fatal(err)
	}
	configFile := filepath.Join(configRoot, "config.toml")
	if err := os.WriteFile(configFile, []byte(`
[inventory]
include = ["inventory.toml"]
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

	for _, want := range []string{
		"Provider",
		"Type",
		"Cache",
		"Hosts",
		"Groups",
		"Output",
		"local",
		"netbox-prod",
		"7m error",
		"provider_local.conf",
		"provider_netbox-prod.conf",
		"Group",
		"Config",
		"Auth",
		"edge",
		"local/lab",
		"inventory",
		"pass-local/local-admin",
		"netbox-prod/customer",
		"pass-local/netbox-admin",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("status tree missing %q:\n%s", want, got)
		}
	}
	for _, unexpected := range []string{
		filepath.Join(paths.SSHConfigDir, "nssh.d", "provider_local.conf"),
		inventoryFile,
		"password ref:",
		"nssh/groups/lab",
		"nssh/groups/customer",
		"Match",
		"suffix=.example.com",
		"provider=customer",
		"selectors",
		"  group membership",
		"  group selectors",
	} {
		if strings.Contains(got, unexpected) {
			t.Fatalf("status tree should fold %q into groups:\n%s", unexpected, got)
		}
	}
	widths := statusTableWidths(got)
	if len(widths) != 2 {
		t.Fatalf("status dashboard table count = %d, want 2:\n%s", len(widths), got)
	}
	if widths[0] != widths[1] {
		t.Fatalf("status dashboard table widths = %v, want equal:\n%s", widths, got)
	}
}

func statusTableWidths(rendered string) []int {
	widths := make([]int, 0, 2)
	for _, line := range strings.Split(rendered, "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "╭") {
			widths = append(widths, lipgloss.Width(line))
		}
	}
	return widths
}

func TestRenderStatusTreeShowsProviderDetailForNamedProvider(t *testing.T) {
	inventory.SetStateDir(t.TempDir())
	t.Cleanup(func() { inventory.SetStateDir("") })

	paths := &config.Paths{SSHConfigDir: filepath.Join(t.TempDir(), ".ssh")}
	configRoot := t.TempDir()
	inventoryFile := filepath.Join(configRoot, "inventory.toml")
	if err := os.WriteFile(inventoryFile, []byte(`
[provider.netbox-prod]
type = "netbox"

[provider.netbox-prod.group.customer.match]
provider = ["customer"]

[provider.netbox-prod.group.customer.auth]
credential_provider = "pass-local"
password_ref = "nssh/groups/customer"
username = "netbox-admin"
`), 0600); err != nil {
		t.Fatal(err)
	}
	configFile := filepath.Join(configRoot, "config.toml")
	if err := os.WriteFile(configFile, []byte(`
[inventory]
include = ["inventory.toml"]
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

	for _, want := range []string{
		"netbox-prod",
		"  type: netbox",
		"  cache: 7m old, 1 objects",
		"  output: " + filepath.Join(paths.SSHConfigDir, "nssh.d", "provider_netbox-prod.conf"),
		"  hosts: 1",
		"  groups",
		"    netbox-prod/customer",
		"      config: " + inventoryFile,
		"      output: " + filepath.Join(paths.SSHConfigDir, "nssh.d", "provider_netbox-prod.conf"),
		"      hosts: 1 host",
		"      match provider = customer",
		"      credential provider: pass-local",
		"      username: netbox-admin",
		"      password ref: nssh/groups/customer",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("status tree missing %q:\n%s", want, got)
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
	if strings.Contains(got, "provider_local.conf") || strings.Contains(got, "local\n") {
		t.Fatalf("missing local provider should not be shown:\n%s", got)
	}
	if !strings.Contains(got, "netbox-prod") {
		t.Fatalf("external provider missing:\n%s", got)
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
	for _, want := range []string{
		"local",
		"  type: local",
		"  hosts: 0",
		"  groups",
		"    local/lab",
		"      hosts: 0 hosts",
		"      match domain_suffix = .example.com",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("status tree missing %q:\n%s", want, got)
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
	if !strings.Contains(got, "  cache: stale, refresh required") {
		t.Fatalf("status tree missing stale cache message:\n%s", got)
	}
	if strings.Contains(got, "unsupported provider state version") {
		t.Fatalf("status tree leaked stale state as last error:\n%s", got)
	}
}

func TestRenderStatusTreeReportsLocalFindingsReadOnly(t *testing.T) {
	inventory.SetStateDir(t.TempDir())
	t.Cleanup(func() { inventory.SetStateDir("") })

	paths := &config.Paths{SSHConfigDir: filepath.Join(t.TempDir(), ".ssh")}
	localFile := filepath.Join(paths.SSHConfigDir, "nssh.d", "provider_local.conf")
	if err := os.MkdirAll(filepath.Dir(localFile), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(localFile, []byte(""+
		"Host edge01\n"+
		"  # Group: local/lab\n"+
		"  HostName 192.0.2.1\n"+
		"\n"+
		"Host edge01\n"+
		"  # Group: local/lab\n"+
		"  HostName 192.0.2.2\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Inventory: config.InventoryConfig{
		Group: map[string]config.GroupConfig{
			"lab": {},
		},
	}}

	got, err := renderStatusTree(cfg, paths, "local", time.Date(2026, 5, 29, 2, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("renderStatusTree: %v", err)
	}
	for _, want := range []string{
		"  findings",
		"    edge01 [local/lab] duplicate: pattern \"edge01\" also appears in " + localFile,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("status tree missing %q:\n%s", want, got)
		}
	}
	content, err := os.ReadFile(localFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(content), "Host edge01") != 2 {
		t.Fatalf("status should not mutate local inventory:\n%s", content)
	}
}
