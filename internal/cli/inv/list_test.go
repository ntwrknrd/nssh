package inv

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
)

func TestInventoryGroupSummariesCountHostsByProvider(t *testing.T) {
	cfg := &config.Config{Inventory: config.InventoryConfig{
		Provider: map[string]config.InventoryProviderConfig{
			"local":        {Type: config.ProviderLocal, Group: map[string]config.GroupConfig{"custcbb": {DomainSuffix: []string{".custcbb.local"}}, "empty": {}}},
			"netbox-prod":  {Type: config.ProviderNetBox, Group: map[string]config.GroupConfig{"custcbb": {DomainSuffix: []string{".custcbb.local"}}}},
			"nre-netlab01": {Type: config.ProviderContainerlab, Config: config.InventoryProviderDetailConfig{JumpHost: "nre-netlab01"}, Group: map[string]config.GroupConfig{"containerlab": {}}},
		},
	}}
	hosts := []*sshconfig.HostEntry{
		{Host: "edge01"},
		{Host: "edge02"},
		{Host: "clab-core01"},
		{Host: "ungrouped"},
	}
	meta := map[*sshconfig.HostEntry]hostMetadata{
		hosts[0]: {Owner: "local", Group: "local/custcbb"},
		hosts[1]: {Owner: "netbox-prod", Group: "netbox-prod/custcbb"},
		hosts[2]: {Owner: "nre-netlab01", Group: "nre-netlab01/containerlab"},
		hosts[3]: {Owner: "local", Group: "-"},
	}

	paths := &config.Paths{SSHConfigDir: filepath.Join(t.TempDir(), ".ssh")}
	rows := inventoryGroupSummaries(cfg, paths, hosts, func(host *sshconfig.HostEntry) hostMetadata {
		return meta[host]
	})

	if len(rows) != 4 {
		t.Fatalf("rows = %d, want 4", len(rows))
	}
	assertGroupSummary(t, rows[0], "local/custcbb", ".custcbb.local", 1, []inventoryGroupSource{
		{Provider: "local", Hosts: 1},
	})
	assertGroupSummary(t, rows[1], "local/empty", "-", 0, nil)
	assertGroupSummary(t, rows[2], "netbox-prod/custcbb", ".custcbb.local", 1, []inventoryGroupSource{{Provider: "netbox-prod", Hosts: 1}})
	assertGroupSummary(t, rows[3], "nre-netlab01/containerlab", "-", 1, []inventoryGroupSource{{Provider: "nre-netlab01", Hosts: 1}})
}

func TestInventoryGroupSelectOptionsIncludeHostCountsAndProviders(t *testing.T) {
	options := inventoryGroupSelectOptions([]inventoryGroupSummary{
		{Name: "local/custcbb", DomainSuffix: ".custcbb.local", Total: 1, Sources: []inventoryGroupSource{
			{Provider: "local", Hosts: 1},
		}},
		{Name: "netbox-prod/custcbb", DomainSuffix: ".custcbb.local", Total: 1088, Sources: []inventoryGroupSource{
			{Provider: "netbox-prod", Hosts: 1088},
		}},
		{Name: "empty", DomainSuffix: "-", Total: 0},
	}, "810-neteng01")

	if len(options) != 3 {
		t.Fatalf("options = %d, want 3", len(options))
	}
	if options[0].Value != "local/custcbb" {
		t.Fatalf("first option value = %q, want local/custcbb", options[0].Value)
	}
	if options[0].Label != "local/custcbb -> 810-neteng01.custcbb.local" {
		t.Fatalf("first option label = %q", options[0].Label)
	}
	if options[2].Label != "empty -> 810-neteng01" {
		t.Fatalf("second option label = %q", options[1].Label)
	}
}

func TestInventoryGroupSummariesIncludeConfigAndOutputFiles(t *testing.T) {
	tmp := t.TempDir()
	inventoryFile := filepath.Join(tmp, "inventory.yaml")
	if err := os.WriteFile(inventoryFile, []byte(`
inventory:
  providers:
    netbox-prod:
      type: netbox
      groups:
        custcbb:
          domain_suffix: [.custcbb.local]
`), 0600); err != nil {
		t.Fatal(err)
	}
	configFile := filepath.Join(tmp, "config.yaml")
	if err := os.WriteFile(configFile, []byte(`
include: [inventory.yaml]
`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configFile)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	paths := &config.Paths{SSHConfigDir: filepath.Join(tmp, ".ssh")}

	rows := inventoryGroupSummaries(cfg, paths, nil, func(*sshconfig.HostEntry) hostMetadata {
		return hostMetadata{}
	})

	var row inventoryGroupSummary
	for _, candidate := range rows {
		if candidate.Name == "netbox-prod/custcbb" {
			row = candidate
			break
		}
	}
	if row.Name == "" {
		t.Fatalf("missing netbox-prod/custcbb row: %+v", rows)
	}
	if row.ConfigFile != inventoryFile {
		t.Fatalf("config file = %q, want %q", row.ConfigFile, inventoryFile)
	}
	wantOutput := filepath.Join(paths.SSHConfigDir, "nssh.d", "provider_netbox-prod.conf")
	if row.OutputFile != wantOutput {
		t.Fatalf("output file = %q, want %q", row.OutputFile, wantOutput)
	}
}

func assertGroupSummary(t *testing.T, row inventoryGroupSummary, name, domainSuffix string, total int, sources []inventoryGroupSource) {
	t.Helper()
	if row.Name != name || row.DomainSuffix != domainSuffix || row.Total != total {
		t.Fatalf("row = %+v, want name=%q domainSuffix=%q total=%d", row, name, domainSuffix, total)
	}
	if len(row.Sources) != len(sources) {
		t.Fatalf("sources = %+v, want %+v", row.Sources, sources)
	}
	for i := range sources {
		if row.Sources[i] != sources[i] {
			t.Fatalf("sources = %+v, want %+v", row.Sources, sources)
		}
	}
}
