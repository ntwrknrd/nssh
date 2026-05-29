package inv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/inventory"
)

func TestRenderStatusTreeShowsLocalProviderAndProviderRoutes(t *testing.T) {
	inventory.SetStateDir(t.TempDir())
	t.Cleanup(func() { inventory.SetStateDir("") })

	paths := &config.Paths{SSHConfigDir: filepath.Join(t.TempDir(), ".ssh")}
	localFile := filepath.Join(paths.SSHConfigDir, "nssh.d", "provider_local.conf")
	if err := os.MkdirAll(filepath.Dir(localFile), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(localFile, []byte("Host local01\n  # Group: lab\n  HostName local01.example.com\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Inventory: config.InventoryConfig{
		DefaultGroup: "lab",
		Group: map[string]config.GroupConfig{
			"lab":     {},
			"custcbb": {},
		},
		Provider: map[string]config.InventoryProviderConfig{
			"netbox-prod": {
				Type: config.ProviderNetBox,
				Route: []config.InventoryRouteConfig{{
					Group: "custcbb",
					Match: config.InventoryRouteMatch{"provider": []string{"custcbb"}},
				}},
			},
		},
	}}
	now := time.Date(2026, 5, 29, 2, 30, 0, 0, time.UTC)
	state := &inventory.ProviderState{
		Version:     inventory.StateVersion,
		Provider:    "netbox-prod",
		Type:        config.ProviderNetBox,
		LastRefresh: now.Add(-7 * time.Minute),
		IncludeFile: inventory.ProviderIncludeFile("netbox-prod"),
		LastError:   "api down",
		Objects: map[string]*inventory.ProviderHost{
			"device:1": {ObjectID: "device:1", Host: "edge01", HostName: "edge01.example.com", Group: "custcbb"},
			"device:2": {ObjectID: "device:2", Host: "edge02", HostName: "edge02.example.com", Group: "custcbb"},
		},
	}
	if err := inventory.SaveProviderState(state); err != nil {
		t.Fatal(err)
	}

	got, err := renderStatusTree(cfg, paths, "", now, nil)
	if err != nil {
		t.Fatalf("renderStatusTree: %v", err)
	}

	for _, want := range []string{
		"local",
		"  type: local",
		"  output: " + filepath.Join(paths.SSHConfigDir, "nssh.d", "provider_local.conf"),
		"  groups: lab",
		"netbox-prod",
		"  type: netbox",
		"  cache: 7m old, 2 objects",
		"  output: " + filepath.Join(paths.SSHConfigDir, "nssh.d", "provider_netbox-prod.conf"),
		"  last error: api down",
		"  routes",
		"    [0] group custcbb -> " + filepath.Join(paths.SSHConfigDir, "nssh.d", "provider_netbox-prod.conf"),
		"        match provider = custcbb",
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
		DefaultGroup: "lab",
		Group: map[string]config.GroupConfig{
			"lab":     {},
			"custcbb": {},
		},
		Provider: map[string]config.InventoryProviderConfig{
			"netbox-prod": {
				Type:  config.ProviderNetBox,
				Route: []config.InventoryRouteConfig{{Group: "custcbb"}},
			},
		},
	}}

	got, err := renderStatusTree(cfg, paths, "", time.Date(2026, 5, 29, 2, 30, 0, 0, time.UTC), nil)
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

func TestRenderStatusTreeShowsRefreshResultsForExternalProviders(t *testing.T) {
	inventory.SetStateDir(t.TempDir())
	t.Cleanup(func() { inventory.SetStateDir("") })

	paths := &config.Paths{SSHConfigDir: filepath.Join(t.TempDir(), ".ssh")}
	cfg := &config.Config{Inventory: config.InventoryConfig{
		DefaultGroup: "lab",
		Group: map[string]config.GroupConfig{
			"lab": {},
		},
		Provider: map[string]config.InventoryProviderConfig{
			"netbox-prod": {
				Type:  config.ProviderNetBox,
				Route: []config.InventoryRouteConfig{{Group: "lab"}},
			},
		},
	}}

	got, err := renderStatusTree(cfg, paths, "", time.Date(2026, 5, 29, 2, 30, 0, 0, time.UTC), map[string]string{
		"netbox-prod": "ok (1297 objects)",
	})
	if err != nil {
		t.Fatalf("renderStatusTree: %v", err)
	}
	if strings.Contains(got, "local\n  live refresh:") {
		t.Fatalf("local provider should not show refresh result:\n%s", got)
	}
	if !strings.Contains(got, "  refresh: ok (1297 objects)") {
		t.Fatalf("external provider missing refresh result:\n%s", got)
	}
}
