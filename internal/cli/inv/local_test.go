package inv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
)

func TestUpsertLocalHostWritesSingleLocalProviderFile(t *testing.T) {
	tmp := t.TempDir()
	sshDir := filepath.Join(tmp, ".ssh")
	if err := os.MkdirAll(filepath.Join(sshDir, "nssh.d"), 0700); err != nil {
		t.Fatal(err)
	}
	mainConfig := filepath.Join(sshDir, "config")
	if err := os.WriteFile(mainConfig, []byte("Include nssh.d/*\n"), 0600); err != nil {
		t.Fatal(err)
	}

	parser := sshconfig.NewParserWithPaths(mainConfig, filepath.Join(tmp, "backups"), 5)
	cfg := &config.Config{Inventory: config.InventoryConfig{
		Group: map[string]config.GroupConfig{
			"lab": {},
		},
	}}
	paths := &config.Paths{SSHConfigDir: sshDir, BackupDir: filepath.Join(tmp, "backups")}

	err := upsertLocalHost(parser, cfg, paths, hostPatch{
		Host:     "edge01",
		Group:    "lab",
		HostName: "edge01.lab.local",
		User:     "admin",
		Port:     2222,
		PortSet:  true,
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(sshDir, "nssh.d", "provider_local.conf"))
	if err != nil {
		t.Fatalf("read local file: %v", err)
	}
	got := string(content)
	for _, want := range []string{"# Group: lab", "Host edge01", "HostName edge01.lab.local", "User admin", "Port 2222"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestUpsertLocalHostRequiresGroupForNewHost(t *testing.T) {
	tmp := t.TempDir()
	sshDir := filepath.Join(tmp, ".ssh")
	if err := os.MkdirAll(filepath.Join(sshDir, "nssh.d"), 0700); err != nil {
		t.Fatal(err)
	}
	mainConfig := filepath.Join(sshDir, "config")
	if err := os.WriteFile(mainConfig, []byte("Include nssh.d/*\n"), 0600); err != nil {
		t.Fatal(err)
	}

	parser := sshconfig.NewParserWithPaths(mainConfig, filepath.Join(tmp, "backups"), 5)
	cfg := &config.Config{Inventory: config.InventoryConfig{
		Group: map[string]config.GroupConfig{"lab": {}},
	}}
	paths := &config.Paths{SSHConfigDir: sshDir, BackupDir: filepath.Join(tmp, "backups")}

	err := upsertLocalHost(parser, cfg, paths, hostPatch{Host: "edge01"})
	if err == nil {
		t.Fatal("expected missing group error")
	}
	if !strings.Contains(err.Error(), "group is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUpsertLocalHostPreservesExistingGroupWhenGroupOmitted(t *testing.T) {
	tmp := t.TempDir()
	sshDir := filepath.Join(tmp, ".ssh")
	if err := os.MkdirAll(filepath.Join(sshDir, "nssh.d"), 0700); err != nil {
		t.Fatal(err)
	}
	mainConfig := filepath.Join(sshDir, "config")
	if err := os.WriteFile(mainConfig, []byte("Include nssh.d/*\n"), 0600); err != nil {
		t.Fatal(err)
	}
	localFile := filepath.Join(sshDir, "nssh.d", "provider_local.conf")
	if err := os.WriteFile(localFile, []byte("Host edge01\n  # Group: lab\n  HostName old.lab.local\n"), 0600); err != nil {
		t.Fatal(err)
	}

	parser := sshconfig.NewParserWithPaths(mainConfig, filepath.Join(tmp, "backups"), 5)
	cfg := &config.Config{Inventory: config.InventoryConfig{
		Group: map[string]config.GroupConfig{"lab": {}},
	}}
	paths := &config.Paths{SSHConfigDir: sshDir, BackupDir: filepath.Join(tmp, "backups")}

	if err := upsertLocalHost(parser, cfg, paths, hostPatch{Host: "edge01", HostName: "new.lab.local"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	content, err := os.ReadFile(localFile)
	if err != nil {
		t.Fatal(err)
	}
	got := string(content)
	for _, want := range []string{"# Group: lab", "HostName new.lab.local"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestUpsertLocalHostRefusesProviderOwnedHost(t *testing.T) {
	tmp := t.TempDir()
	sshDir := filepath.Join(tmp, ".ssh")
	if err := os.MkdirAll(filepath.Join(sshDir, "nssh.d"), 0700); err != nil {
		t.Fatal(err)
	}
	mainConfig := filepath.Join(sshDir, "config")
	if err := os.WriteFile(mainConfig, []byte("Include nssh.d/*\n"), 0600); err != nil {
		t.Fatal(err)
	}
	providerFile := filepath.Join(sshDir, "nssh.d", "provider_netbox-prod.conf")
	if err := os.WriteFile(providerFile, []byte("Host edge01\n  HostName edge01.example.com\n"), 0600); err != nil {
		t.Fatal(err)
	}

	parser := sshconfig.NewParserWithPaths(mainConfig, filepath.Join(tmp, "backups"), 5)
	cfg := &config.Config{Inventory: config.InventoryConfig{
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
	paths := &config.Paths{SSHConfigDir: sshDir, BackupDir: filepath.Join(tmp, "backups")}

	err := upsertLocalHost(parser, cfg, paths, hostPatch{Host: "edge01", Group: "lab", User: "admin"})
	if err == nil {
		t.Fatal("expected provider-owned mutation refusal")
	}
	if !strings.Contains(err.Error(), "provider-owned") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRemoveLocalHostRemovesOnlyLocalHosts(t *testing.T) {
	tmp := t.TempDir()
	sshDir := filepath.Join(tmp, ".ssh")
	if err := os.MkdirAll(filepath.Join(sshDir, "nssh.d"), 0700); err != nil {
		t.Fatal(err)
	}
	mainConfig := filepath.Join(sshDir, "config")
	if err := os.WriteFile(mainConfig, []byte("Include nssh.d/*\n"), 0600); err != nil {
		t.Fatal(err)
	}
	localFile := filepath.Join(sshDir, "nssh.d", "provider_local.conf")
	if err := os.WriteFile(localFile, []byte("Host edge01\n  HostName edge01.lab.local\n\nHost edge02\n  HostName edge02.lab.local\n"), 0600); err != nil {
		t.Fatal(err)
	}

	parser := sshconfig.NewParserWithPaths(mainConfig, filepath.Join(tmp, "backups"), 5)
	cfg := &config.Config{Inventory: config.InventoryConfig{
		Group: map[string]config.GroupConfig{
			"lab": {},
		},
	}}
	paths := &config.Paths{SSHConfigDir: sshDir, BackupDir: filepath.Join(tmp, "backups")}

	removed, err := removeLocalHost(parser, cfg, paths, "edge01")
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if !removed {
		t.Fatal("expected host removal")
	}
	content, err := os.ReadFile(localFile)
	if err != nil {
		t.Fatal(err)
	}
	got := string(content)
	if strings.Contains(got, "Host edge01") {
		t.Fatalf("removed host still present:\n%s", got)
	}
	if !strings.Contains(got, "Host edge02") {
		t.Fatalf("other host missing:\n%s", got)
	}
}

func TestInventoryHostsIgnoresNonNsshIncludes(t *testing.T) {
	tmp := t.TempDir()
	sshDir := filepath.Join(tmp, ".ssh")
	if err := os.MkdirAll(filepath.Join(sshDir, "nssh.d"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(sshDir, "conf.d"), 0700); err != nil {
		t.Fatal(err)
	}
	mainConfig := filepath.Join(sshDir, "config")
	if err := os.WriteFile(mainConfig, []byte("Include nssh.d/*\nInclude conf.d/*\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "nssh.d", "provider_local.conf"), []byte("Host managed\n  HostName managed.example.com\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "conf.d", "external_hosts"), []byte("Host unmanaged\n  HostName unmanaged.example.com\n"), 0600); err != nil {
		t.Fatal(err)
	}

	parser := sshconfig.NewParserWithPaths(mainConfig, filepath.Join(tmp, "backups"), 5)
	cfg := &config.Config{Inventory: config.InventoryConfig{
		Group: map[string]config.GroupConfig{
			"lab": {},
		},
	}}
	paths := &config.Paths{SSHConfigDir: sshDir, BackupDir: filepath.Join(tmp, "backups")}

	hosts, err := inventoryHosts(parser, cfg, paths)
	if err != nil {
		t.Fatalf("inventoryHosts: %v", err)
	}
	if len(hosts) != 1 {
		t.Fatalf("hosts = %d, want 1", len(hosts))
	}
	if hosts[0].Host != "managed" {
		t.Fatalf("host = %q", hosts[0].Host)
	}
}

func TestFilterInventoryHostsBySelectPattern(t *testing.T) {
	hosts := []*sshconfig.HostEntry{
		{
			Host:       "151-core1",
			HostName:   "151-core1.customer.local",
			Properties: map[string]string{"user": "netops"},
		},
		{
			Host:       "lab-router",
			HostName:   "lab-router.example.net",
			Properties: map[string]string{"user": "admin"},
		},
		{
			Host:       "router.example.com",
			HostName:   "192.0.2.1",
			Properties: map[string]string{"user": "ops"},
		},
	}
	meta := map[*sshconfig.HostEntry]hostMetadata{
		hosts[0]: {Owner: "local", Group: "customer"},
		hosts[1]: {Owner: "netbox-prod", Group: "lab"},
		hosts[2]: {Owner: "local", Group: "corp"},
	}

	tests := []struct {
		name    string
		pattern string
		want    string
	}{
		{name: "host alias", pattern: "151-core", want: "151-core1"},
		{name: "hostname", pattern: "example.net", want: "lab-router"},
		{name: "derived host id", pattern: "id:router", want: "router.example.com"},
		{name: "user", pattern: "netops", want: "151-core1"},
		{name: "field provider", pattern: "provider:netbox-prod", want: "lab-router"},
		{name: "field group", pattern: "group:customer", want: "151-core1"},
		{name: "multiple terms", pattern: "group:corp user:ops", want: "router.example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filtered, err := filterInventoryHosts(hosts, tt.pattern, func(host *sshconfig.HostEntry) hostMetadata {
				return meta[host]
			})
			if err != nil {
				t.Fatalf("filterInventoryHosts: %v", err)
			}
			if len(filtered) != 1 {
				t.Fatalf("filtered len = %d, want 1", len(filtered))
			}
			if filtered[0].Host != tt.want {
				t.Fatalf("host = %q, want %q", filtered[0].Host, tt.want)
			}
		})
	}
	filtered, err := filterInventoryHosts(hosts, "group:corp", func(host *sshconfig.HostEntry) hostMetadata {
		return meta[host]
	})
	if err != nil {
		t.Fatalf("filterInventoryHosts: %v", err)
	}
	if len(filtered) != 1 || filtered[0].Host != "router.example.com" {
		t.Fatalf("group:corp filtered = %+v", filtered)
	}

	_, err = filterInventoryHosts(hosts, "[", func(host *sshconfig.HostEntry) hostMetadata {
		return meta[host]
	})
	if err == nil {
		t.Fatal("expected invalid regex error")
	}
}

func TestRemoveLocalHostIgnoresNonInventoryIncludes(t *testing.T) {
	tmp := t.TempDir()
	sshDir := filepath.Join(tmp, ".ssh")
	if err := os.MkdirAll(filepath.Join(sshDir, "conf.d"), 0700); err != nil {
		t.Fatal(err)
	}
	mainConfig := filepath.Join(sshDir, "config")
	if err := os.WriteFile(mainConfig, []byte("Include conf.d/*\n"), 0600); err != nil {
		t.Fatal(err)
	}
	externalFile := filepath.Join(sshDir, "conf.d", "external_hosts")
	if err := os.WriteFile(externalFile, []byte("Host unmanaged\n  HostName unmanaged.example.com\n"), 0600); err != nil {
		t.Fatal(err)
	}

	parser := sshconfig.NewParserWithPaths(mainConfig, filepath.Join(tmp, "backups"), 5)
	cfg := &config.Config{Inventory: config.InventoryConfig{
		Group: map[string]config.GroupConfig{
			"lab": {},
		},
	}}
	paths := &config.Paths{SSHConfigDir: sshDir, BackupDir: filepath.Join(tmp, "backups")}

	removed, err := removeLocalHost(parser, cfg, paths, "unmanaged")
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if removed {
		t.Fatal("expected non-inventory host to be ignored")
	}
	content, err := os.ReadFile(externalFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "Host unmanaged") {
		t.Fatalf("external include was modified:\n%s", content)
	}
}

func TestImportLocalCSVAddsHostsToLocalProviderFile(t *testing.T) {
	tmp := t.TempDir()
	sshDir := filepath.Join(tmp, ".ssh")
	if err := os.MkdirAll(filepath.Join(sshDir, "nssh.d"), 0700); err != nil {
		t.Fatal(err)
	}
	mainConfig := filepath.Join(sshDir, "config")
	if err := os.WriteFile(mainConfig, []byte("Include nssh.d/*\n"), 0600); err != nil {
		t.Fatal(err)
	}
	csvPath := filepath.Join(tmp, "hosts.csv")
	if err := os.WriteFile(csvPath, []byte("host,hostname,user,port\nedge02,edge02.lab.local,admin,2222\nedge01,edge01.lab.local,netops,\n"), 0600); err != nil {
		t.Fatal(err)
	}

	parser := sshconfig.NewParserWithPaths(mainConfig, filepath.Join(tmp, "backups"), 5)
	cfg := &config.Config{Inventory: config.InventoryConfig{
		Group: map[string]config.GroupConfig{
			"lab": {},
		},
	}}
	paths := &config.Paths{SSHConfigDir: sshDir, BackupDir: filepath.Join(tmp, "backups")}

	result, err := importLocalCSV(parser, cfg, paths, csvPath, "lab")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.Added != 2 || result.Skipped != 0 || result.Failed != 0 {
		t.Fatalf("result = %+v", result)
	}
	content, err := os.ReadFile(filepath.Join(sshDir, "nssh.d", "provider_local.conf"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(content)
	if strings.Index(got, "Host edge01") > strings.Index(got, "Host edge02") {
		t.Fatalf("hosts not sorted:\n%s", got)
	}
	for _, want := range []string{"# Group: lab", "Host edge01", "HostName edge01.lab.local", "User netops", "Host edge02", "Port 2222"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestImportLocalCSVRequiresExplicitGroup(t *testing.T) {
	tmp := t.TempDir()
	sshDir := filepath.Join(tmp, ".ssh")
	if err := os.MkdirAll(filepath.Join(sshDir, "nssh.d"), 0700); err != nil {
		t.Fatal(err)
	}
	mainConfig := filepath.Join(sshDir, "config")
	if err := os.WriteFile(mainConfig, []byte("Include nssh.d/*\n"), 0600); err != nil {
		t.Fatal(err)
	}
	csvPath := filepath.Join(tmp, "hosts.csv")
	if err := os.WriteFile(csvPath, []byte("host,hostname\nedge01,edge01.lab.local\n"), 0600); err != nil {
		t.Fatal(err)
	}

	parser := sshconfig.NewParserWithPaths(mainConfig, filepath.Join(tmp, "backups"), 5)
	cfg := &config.Config{Inventory: config.InventoryConfig{
		Group: map[string]config.GroupConfig{"lab": {}},
	}}
	paths := &config.Paths{SSHConfigDir: sshDir, BackupDir: filepath.Join(tmp, "backups")}

	result, err := importLocalCSV(parser, cfg, paths, csvPath, "")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.Added != 0 || result.Failed != 1 {
		t.Fatalf("result = %+v", result)
	}
	if len(result.Errors) != 1 || !strings.Contains(result.Errors[0], "group is required") {
		t.Fatalf("errors = %+v", result.Errors)
	}
}

func TestEnsureGroupCreatesMetadataOnlyGroup(t *testing.T) {
	cfg := &config.Config{Inventory: config.InventoryConfig{
		Group: map[string]config.GroupConfig{
			"default": {},
		},
	}}
	created := ensureGroup(cfg, "lab")
	if !created {
		t.Fatal("expected group creation")
	}
	if len(cfg.Inventory.Group["lab"].DomainSuffix) != 0 {
		t.Fatalf("group config = %+v, want metadata-only group", cfg.Inventory.Group["lab"])
	}
	if ensureGroup(cfg, "lab") {
		t.Fatal("expected existing group to be preserved")
	}
}
