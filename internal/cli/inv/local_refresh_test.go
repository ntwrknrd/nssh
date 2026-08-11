package inv

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
)

func TestVisitLocalRefreshFindingsEmitsBeforeCheckingLaterHosts(t *testing.T) {
	hosts := []*sshconfig.HostEntry{
		{
			Host:       "edge01",
			HostName:   "edge01.example.com",
			SourceFile: filepath.Join("nssh.d", "provider_local.conf"),
		},
		{
			Host:       "edge02",
			HostName:   "edge02.example.com",
			SourceFile: filepath.Join("nssh.d", "provider_local.conf"),
		},
	}
	cfg := &config.Config{Inventory: config.InventoryConfig{
		Group: map[string]config.GroupConfig{
			"lab": {},
		},
	}}
	paths := &config.Paths{SSHConfigDir: t.TempDir()}

	var events []string
	dnsCheck := func(hostName string) localRefreshDNSResult {
		events = append(events, "dns:"+hostName)
		if hostName == "edge02.example.com" && !reflect.DeepEqual(events, []string{
			"dns:edge01.example.com",
			"emit:edge01",
			"dns:edge02.example.com",
		}) {
			t.Fatalf("first finding was not emitted before second DNS check: %v", events)
		}
		if hostName == "edge01.example.com" {
			return localRefreshDNSResult{status: "nxdomain"}
		}
		return localRefreshDNSResult{status: "ok"}
	}

	count := visitLocalRefreshFindings(hosts, cfg, paths, nil, dnsCheck, func(finding localRefreshFinding) {
		events = append(events, "emit:"+finding.Host)
	})

	if count != 1 {
		t.Fatalf("finding count = %d, want 1", count)
	}
}

func TestApplyLocalRefreshFixesRemovesSelectedLocalStaleHost(t *testing.T) {
	cfg, paths, localFile := newLocalRefreshFixture(t, ""+
		"Host stale01\n"+
		"  HostName stale01.example.com\n"+
		"\n"+
		"Host keep01\n"+
		"  HostName keep01.example.com\n")
	hosts, err := inventoryHosts(cfg, paths)
	if err != nil {
		t.Fatalf("inventoryHosts: %v", err)
	}
	stale := findLocalRefreshTestHost(t, hosts, "stale01")

	applied, err := applyLocalRefreshFixes(paths, []localRefreshFinding{{
		Host:   stale.Host,
		Group:  "local/lab",
		Issue:  "stale-dns",
		Detail: "nxdomain",
		host:   stale,
		fix:    localRefreshFix{kind: localRefreshFixRemoveHost, host: stale},
	}})
	if err != nil {
		t.Fatalf("applyLocalRefreshFixes: %v", err)
	}
	if applied != 1 {
		t.Fatalf("applied = %d, want 1", applied)
	}

	content := readTestFile(t, localFile)
	if strings.Contains(content, "stale01:") {
		t.Fatalf("stale host still present:\n%s", content)
	}
	if !strings.Contains(content, "keep01.example.com:") {
		t.Fatalf("unselected host removed:\n%s", content)
	}
}

func TestApplyLocalRefreshFixesRemovesOnlySelectedDuplicateBlock(t *testing.T) {
	cfg, paths, localFile := newLocalRefreshFixture(t, ""+
		"Host edge01\n"+
		"  HostName edge01-primary.example.com\n"+
		"\n"+
		"Host edge01\n"+
		"  HostName edge01-duplicate.example.com\n")
	hosts, err := inventoryHosts(cfg, paths)
	if err != nil {
		t.Fatalf("inventoryHosts: %v", err)
	}
	if len(hosts) != 2 {
		t.Fatalf("hosts = %d, want 2", len(hosts))
	}
	var duplicate *sshconfig.HostEntry
	for _, host := range hosts {
		if host.Host == "edge01-duplicate.example.com" {
			duplicate = host
			break
		}
	}
	if duplicate == nil {
		t.Fatalf("duplicate host not found: %+v", hosts)
	}

	applied, err := applyLocalRefreshFixes(paths, []localRefreshFinding{{
		Host:   duplicate.Host,
		Group:  "local/lab",
		Issue:  "duplicate",
		Detail: "duplicate Host edge01",
		host:   duplicate,
		fix:    localRefreshFix{kind: localRefreshFixRemoveDuplicate, host: duplicate},
	}})
	if err != nil {
		t.Fatalf("applyLocalRefreshFixes: %v", err)
	}
	if applied != 1 {
		t.Fatalf("applied = %d, want 1", applied)
	}

	content := readTestFile(t, localFile)
	if !strings.Contains(content, "edge01-primary.example.com") {
		t.Fatalf("canonical duplicate was removed:\n%s", content)
	}
	if strings.Contains(content, "edge01-duplicate.example.com") {
		t.Fatalf("selected duplicate still present:\n%s", content)
	}
}

func TestApplyLocalRefreshFixesRenamesCNAMEAndPreservesAlias(t *testing.T) {
	cfg, paths, localFile := newLocalRefreshFixture(t, ""+
		"Host old01\n"+
		"  HostName old01.example.com\n"+
		"  User admin\n")
	hosts, err := inventoryHosts(cfg, paths)
	if err != nil {
		t.Fatalf("inventoryHosts: %v", err)
	}
	old := findLocalRefreshTestHost(t, hosts, "old01")

	applied, err := applyLocalRefreshFixes(paths, []localRefreshFinding{{
		Host:   old.Host,
		Group:  "local/lab",
		Issue:  "cname-rename",
		Detail: "old01.example.com -> new01.example.com",
		host:   old,
		fix: localRefreshFix{
			kind:        localRefreshFixRenameHost,
			host:        old,
			newID:       "new01",
			cnameTarget: "new01.example.com",
		},
	}})
	if err != nil {
		t.Fatalf("applyLocalRefreshFixes: %v", err)
	}
	if applied != 1 {
		t.Fatalf("applied = %d, want 1", applied)
	}

	content := readTestFile(t, localFile)
	for _, want := range []string{"new01:", "- old01"} {
		if !strings.Contains(content, want) {
			t.Fatalf("missing %q in:\n%s", want, content)
		}
	}
}

func TestApplyLocalRefreshFixesMergesCNAMEIntoExistingLocalHost(t *testing.T) {
	cfg, paths, localFile := newLocalRefreshFixture(t, ""+
		"Host old01\n"+
		"  HostName old01.example.com\n"+
		"\n"+
		"Host new01\n"+
		"  HostName new01.example.com\n")
	hosts, err := inventoryHosts(cfg, paths)
	if err != nil {
		t.Fatalf("inventoryHosts: %v", err)
	}
	old := findLocalRefreshTestHost(t, hosts, "old01")
	target := findLocalRefreshTestHost(t, hosts, "new01")

	applied, err := applyLocalRefreshFixes(paths, []localRefreshFinding{{
		Host:   old.Host,
		Group:  "local/lab",
		Issue:  "cname-rename",
		Detail: "old01.example.com -> new01.example.com",
		host:   old,
		fix: localRefreshFix{
			kind:   localRefreshFixMergeHost,
			host:   old,
			target: target,
			alias:  old.Host,
		},
	}})
	if err != nil {
		t.Fatalf("applyLocalRefreshFixes: %v", err)
	}
	if applied != 1 {
		t.Fatalf("applied = %d, want 1", applied)
	}

	content := readTestFile(t, localFile)
	if strings.Contains(content, "hostname: old01.example.com") {
		t.Fatalf("old CNAME block still present:\n%s", content)
	}
	if !strings.Contains(content, "- old01") {
		t.Fatalf("old alias not merged into target:\n%s", content)
	}
}

func TestVisitLocalRefreshFindingsDoesNotEmitStaleFixForDuplicateBlock(t *testing.T) {
	cfg, paths, _ := newLocalRefreshFixture(t, ""+
		"Host edge01\n"+
		"  HostName edge01-primary.example.com\n"+
		"\n"+
		"Host edge01\n"+
		"  HostName edge01-duplicate.example.com\n")
	hosts, err := inventoryHosts(cfg, paths)
	if err != nil {
		t.Fatalf("inventoryHosts: %v", err)
	}

	var findings []localRefreshFinding
	visitLocalRefreshFindings(hosts, cfg, paths, nil, func(string) localRefreshDNSResult {
		return localRefreshDNSResult{status: "nxdomain"}
	}, func(finding localRefreshFinding) {
		findings = append(findings, finding)
	})

	duplicateFixes := 0
	staleFixes := 0
	for _, finding := range findings {
		switch finding.fix.kind {
		case localRefreshFixRemoveDuplicate:
			duplicateFixes++
		case localRefreshFixRemoveHost:
			staleFixes++
		}
	}
	if duplicateFixes != 1 {
		t.Fatalf("duplicate fixes = %d, want 1; findings=%+v", duplicateFixes, findings)
	}
	if staleFixes != 1 {
		t.Fatalf("stale fixes = %d, want only canonical stale fix; findings=%+v", staleFixes, findings)
	}
}

func newLocalRefreshFixture(t *testing.T, localContent string) (*config.Config, *config.Paths, string) {
	t.Helper()
	tmp := t.TempDir()
	sshDir := filepath.Join(tmp, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	mainConfig := filepath.Join(sshDir, "config")
	sourceFile := filepath.Join(tmp, "source.conf")
	if err := os.WriteFile(sourceFile, []byte(localContent), 0600); err != nil {
		t.Fatal(err)
	}
	parser := sshconfig.NewParserWithPaths(mainConfig, filepath.Join(tmp, "backups"), 5)
	parsed, err := parser.ParseFile(sourceFile)
	if err != nil {
		t.Fatal(err)
	}
	hosts := make(map[string]config.InventoryHostConfig)
	seen := make(map[string]int)
	for _, host := range parsed.Hosts {
		name := host.HostName
		if strings.TrimSpace(name) == "" {
			name = host.Host
		}
		if seen[name] > 0 {
			name += "-duplicate"
		}
		seen[name]++
		hostCfg := config.InventoryHostConfig{Group: "lab"}
		if user := host.User(); user != "" {
			hostCfg.Auth = config.InventoryAuthConfig{Username: user, Mode: config.AuthModeKey}
		}
		for _, pattern := range host.Patterns {
			if pattern != name {
				hostCfg.Aliases = append(hostCfg.Aliases, pattern)
			}
		}
		if name != host.Host && !slices.Contains(hostCfg.Aliases, host.Host) {
			hostCfg.Aliases = append(hostCfg.Aliases, host.Host)
		}
		hosts[name] = hostCfg
	}
	cfg := &config.Config{Include: []string{"inventory/*.yaml"}, Inventory: config.InventoryConfig{
		Providers: map[string]config.InventoryProviderConfig{
			config.ProviderLocal: {
				Type:   config.ProviderLocal,
				Groups: map[string]config.GroupConfig{"lab": {}},
				Hosts:  hosts,
			},
		},
	}}
	paths := &config.Paths{
		ConfigDir:     filepath.Join(tmp, "nssh"),
		ConfigFile:    filepath.Join(tmp, "nssh", "config.yaml"),
		SSHConfigDir:  sshDir,
		SSHConfigFile: mainConfig,
		BackupDir:     filepath.Join(tmp, "backups"),
	}
	if err := os.MkdirAll(paths.ConfigDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.ConfigFile, []byte("include: [inventory/*.yaml]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := saveLocalProviderInventory(cfg, paths); err != nil {
		t.Fatal(err)
	}
	localFile := localProviderYAMLPath(cfg, paths)
	return cfg, paths, localFile
}

func findLocalRefreshTestHost(t *testing.T, hosts []*sshconfig.HostEntry, name string) *sshconfig.HostEntry {
	t.Helper()
	host := sshconfig.FindHostByPattern(hosts, name)
	if host == nil {
		t.Fatalf("host %q not found", name)
	}
	return host
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}
