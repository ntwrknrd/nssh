package inv

import (
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
)

func TestInventoryGroupSummariesCountHostsByProvider(t *testing.T) {
	cfg := &config.Config{Inventory: config.InventoryConfig{
		Group: map[string]config.GroupConfig{
			"custcbb":      {DomainSuffix: []string{".custcbb.local"}},
			"containerlab": {},
			"empty":        {},
		},
	}}
	hosts := []*sshconfig.HostEntry{
		{Host: "edge01"},
		{Host: "edge02"},
		{Host: "clab-core01"},
		{Host: "ungrouped"},
	}
	meta := map[*sshconfig.HostEntry]hostMetadata{
		hosts[0]: {Owner: "local", Group: "custcbb"},
		hosts[1]: {Owner: "netbox-prod", Group: "custcbb"},
		hosts[2]: {Owner: "nre-netlab01", Group: "containerlab"},
		hosts[3]: {Owner: "local", Group: "-"},
	}

	rows := inventoryGroupSummaries(cfg, hosts, func(host *sshconfig.HostEntry) hostMetadata {
		return meta[host]
	})

	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}
	assertGroupSummary(t, rows[0], "containerlab", "-", 1, []inventoryGroupSource{{Provider: "nre-netlab01", Hosts: 1}})
	assertGroupSummary(t, rows[1], "custcbb", ".custcbb.local", 2, []inventoryGroupSource{
		{Provider: "local", Hosts: 1},
		{Provider: "netbox-prod", Hosts: 1},
	})
	assertGroupSummary(t, rows[2], "empty", "-", 0, nil)
}

func TestInventoryGroupSelectOptionsIncludeHostCountsAndProviders(t *testing.T) {
	options := inventoryGroupSelectOptions([]inventoryGroupSummary{
		{Name: "custcbb", DomainSuffix: ".custcbb.local", Total: 1089, Sources: []inventoryGroupSource{
			{Provider: "netbox-prod", Hosts: 1088},
			{Provider: "local", Hosts: 1},
		}},
		{Name: "empty", DomainSuffix: "-", Total: 0},
	})

	if len(options) != 2 {
		t.Fatalf("options = %d, want 2", len(options))
	}
	if options[0].Value != "custcbb" {
		t.Fatalf("first option value = %q, want custcbb", options[0].Value)
	}
	if options[0].Label != "custcbb  1,089 hosts  2 sources  .custcbb.local" {
		t.Fatalf("first option label = %q", options[0].Label)
	}
	if options[1].Label != "empty  0 hosts" {
		t.Fatalf("second option label = %q", options[1].Label)
	}
}

func TestInventoryGroupTablesSplitSummaryAndProviderCounts(t *testing.T) {
	summaryHeaders, summaryRows, providerHeaders, providerRows := inventoryGroupTables([]inventoryGroupSummary{
		{Name: "custcbb", DomainSuffix: "-", Total: 1090, Sources: []inventoryGroupSource{
			{Provider: "netbox-prod", Hosts: 1089},
			{Provider: "local", Hosts: 1},
		}},
	})

	wantSummaryHeaders := []string{"Group", "Domain Suffix", "Total"}
	if len(summaryHeaders) != len(wantSummaryHeaders) {
		t.Fatalf("summary headers = %v, want %v", summaryHeaders, wantSummaryHeaders)
	}
	for i := range wantSummaryHeaders {
		if summaryHeaders[i] != wantSummaryHeaders[i] {
			t.Fatalf("summary headers = %v, want %v", summaryHeaders, wantSummaryHeaders)
		}
	}
	wantSummary := [][]string{{"custcbb", "-", "1,090"}}
	assertRows(t, summaryRows, wantSummary)

	wantProviderHeaders := []string{"local", "netbox-prod"}
	if len(providerHeaders) != len(wantProviderHeaders) {
		t.Fatalf("provider headers = %v, want %v", providerHeaders, wantProviderHeaders)
	}
	for i := range wantProviderHeaders {
		if providerHeaders[i] != wantProviderHeaders[i] {
			t.Fatalf("provider headers = %v, want %v", providerHeaders, wantProviderHeaders)
		}
	}
	wantProvider := [][]string{{"1", "1,089"}}
	assertRows(t, providerRows, wantProvider)
}

func TestInventoryGroupTablesUseDashProviderCellsForZeroCounts(t *testing.T) {
	_, _, _, providerRows := inventoryGroupTables([]inventoryGroupSummary{
		{Name: "cbb", Total: 208, Sources: []inventoryGroupSource{{Provider: "netbox-prod", Hosts: 208}}},
		{Name: "containerlab", Total: 19, Sources: []inventoryGroupSource{{Provider: "nre-netlab01", Hosts: 19}}},
	})

	want := [][]string{
		{"208", "-"},
		{"-", "19"},
	}
	assertRows(t, providerRows, want)
}

func assertRows(t *testing.T, rows, want [][]string) {
	t.Helper()
	if len(rows) != len(want) {
		t.Fatalf("rows = %d, want %d", len(rows), len(want))
	}
	for i := range want {
		for j := range want[i] {
			if rows[i][j] != want[i][j] {
				t.Fatalf("rows[%d][%d] = %q, want %q; rows=%v", i, j, rows[i][j], want[i][j], rows)
			}
		}
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
