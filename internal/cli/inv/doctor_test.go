package inv

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/inventory"
	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
)

func TestVisitDoctorFindingsEmitsBeforeCheckingLaterHosts(t *testing.T) {
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
	dnsCheck := func(hostName string) doctorDNSResult {
		events = append(events, "dns:"+hostName)
		if hostName == "edge02.example.com" && !reflect.DeepEqual(events, []string{
			"dns:edge01.example.com",
			"emit:edge01",
			"dns:edge02.example.com",
		}) {
			t.Fatalf("first finding was not emitted before second DNS check: %v", events)
		}
		if hostName == "edge01.example.com" {
			return doctorDNSResult{status: "nxdomain"}
		}
		return doctorDNSResult{status: "ok"}
	}

	count := visitDoctorFindings(hosts, cfg, paths, nil, dnsCheck, func(finding doctorFinding) {
		events = append(events, "emit:"+finding.Host)
	})

	if count != 1 {
		t.Fatalf("finding count = %d, want 1", count)
	}
}

func TestApplyDoctorFixesRemovesSelectedLocalStaleHost(t *testing.T) {
	parser, cfg, paths, localFile := newDoctorFixture(t, ""+
		"Host stale01\n"+
		"  HostName stale01.example.com\n"+
		"\n"+
		"Host keep01\n"+
		"  HostName keep01.example.com\n")
	hosts, err := inventoryHosts(parser, cfg, paths)
	if err != nil {
		t.Fatalf("inventoryHosts: %v", err)
	}
	stale := findTestHost(t, hosts, "stale01")

	applied, err := applyDoctorFixes(parser, paths, []doctorFinding{{
		Host:   stale.Host,
		Group:  "lab",
		Issue:  "stale-dns",
		Detail: "nxdomain",
		host:   stale,
		fix:    doctorFix{kind: doctorFixRemoveHost, host: stale},
	}})
	if err != nil {
		t.Fatalf("applyDoctorFixes: %v", err)
	}
	if applied != 1 {
		t.Fatalf("applied = %d, want 1", applied)
	}

	content := readTestFile(t, localFile)
	if strings.Contains(content, "Host stale01") {
		t.Fatalf("stale host still present:\n%s", content)
	}
	if !strings.Contains(content, "Host keep01") {
		t.Fatalf("unselected host removed:\n%s", content)
	}
}

func TestApplyDoctorFixesRemovesOnlySelectedDuplicateBlock(t *testing.T) {
	parser, cfg, paths, localFile := newDoctorFixture(t, ""+
		"Host edge01\n"+
		"  HostName edge01-primary.example.com\n"+
		"\n"+
		"Host edge01\n"+
		"  HostName edge01-duplicate.example.com\n")
	hosts, err := inventoryHosts(parser, cfg, paths)
	if err != nil {
		t.Fatalf("inventoryHosts: %v", err)
	}
	if len(hosts) != 2 {
		t.Fatalf("hosts = %d, want 2", len(hosts))
	}
	duplicate := hosts[1]

	applied, err := applyDoctorFixes(parser, paths, []doctorFinding{{
		Host:   duplicate.Host,
		Group:  "lab",
		Issue:  "duplicate",
		Detail: "duplicate Host edge01",
		host:   duplicate,
		fix:    doctorFix{kind: doctorFixRemoveDuplicate, host: duplicate},
	}})
	if err != nil {
		t.Fatalf("applyDoctorFixes: %v", err)
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

func TestApplyDoctorFixesRenamesCNAMEAndPreservesAlias(t *testing.T) {
	parser, cfg, paths, localFile := newDoctorFixture(t, ""+
		"Host old01\n"+
		"  HostName old01.example.com\n"+
		"  User admin\n")
	hosts, err := inventoryHosts(parser, cfg, paths)
	if err != nil {
		t.Fatalf("inventoryHosts: %v", err)
	}
	old := findTestHost(t, hosts, "old01")

	applied, err := applyDoctorFixes(parser, paths, []doctorFinding{{
		Host:   old.Host,
		Group:  "lab",
		Issue:  "cname-rename",
		Detail: "old01.example.com -> new01.example.com",
		host:   old,
		fix: doctorFix{
			kind:        doctorFixRenameHost,
			host:        old,
			newID:       "new01",
			cnameTarget: "new01.example.com",
		},
	}})
	if err != nil {
		t.Fatalf("applyDoctorFixes: %v", err)
	}
	if applied != 1 {
		t.Fatalf("applied = %d, want 1", applied)
	}

	content := readTestFile(t, localFile)
	for _, want := range []string{"Host new01 old01", "HostName new01.example.com", "User admin"} {
		if !strings.Contains(content, want) {
			t.Fatalf("missing %q in:\n%s", want, content)
		}
	}
}

func TestApplyDoctorFixesMergesCNAMEIntoExistingLocalHost(t *testing.T) {
	parser, cfg, paths, localFile := newDoctorFixture(t, ""+
		"Host old01\n"+
		"  HostName old01.example.com\n"+
		"\n"+
		"Host new01\n"+
		"  HostName new01.example.com\n")
	hosts, err := inventoryHosts(parser, cfg, paths)
	if err != nil {
		t.Fatalf("inventoryHosts: %v", err)
	}
	old := findTestHost(t, hosts, "old01")
	target := findTestHost(t, hosts, "new01")

	applied, err := applyDoctorFixes(parser, paths, []doctorFinding{{
		Host:   old.Host,
		Group:  "lab",
		Issue:  "cname-rename",
		Detail: "old01.example.com -> new01.example.com",
		host:   old,
		fix: doctorFix{
			kind:   doctorFixMergeHost,
			host:   old,
			target: target,
			alias:  old.Host,
		},
	}})
	if err != nil {
		t.Fatalf("applyDoctorFixes: %v", err)
	}
	if applied != 1 {
		t.Fatalf("applied = %d, want 1", applied)
	}

	content := readTestFile(t, localFile)
	if strings.Contains(content, "HostName old01.example.com") {
		t.Fatalf("old CNAME block still present:\n%s", content)
	}
	if !strings.Contains(content, "Host new01 old01") {
		t.Fatalf("old alias not merged into target:\n%s", content)
	}
}

func TestVisitDoctorFindingsDoesNotEmitStaleFixForDuplicateBlock(t *testing.T) {
	parser, cfg, paths, _ := newDoctorFixture(t, ""+
		"Host edge01\n"+
		"  HostName edge01-primary.example.com\n"+
		"\n"+
		"Host edge01\n"+
		"  HostName edge01-duplicate.example.com\n")
	hosts, err := inventoryHosts(parser, cfg, paths)
	if err != nil {
		t.Fatalf("inventoryHosts: %v", err)
	}

	var findings []doctorFinding
	visitDoctorFindings(hosts, cfg, paths, nil, func(string) doctorDNSResult {
		return doctorDNSResult{status: "nxdomain"}
	}, func(finding doctorFinding) {
		findings = append(findings, finding)
	})

	duplicateFixes := 0
	staleFixes := 0
	for _, finding := range findings {
		switch finding.fix.kind {
		case doctorFixRemoveDuplicate:
			duplicateFixes++
		case doctorFixRemoveHost:
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

func newDoctorFixture(t *testing.T, localContent string) (*sshconfig.Parser, *config.Config, *config.Paths, string) {
	t.Helper()
	tmp := t.TempDir()
	sshDir := filepath.Join(tmp, ".ssh")
	nsshDir := filepath.Join(sshDir, "nssh.d")
	if err := os.MkdirAll(nsshDir, 0700); err != nil {
		t.Fatal(err)
	}
	mainConfig := filepath.Join(sshDir, "config")
	if err := os.WriteFile(mainConfig, []byte("Include nssh.d/*\n"), 0600); err != nil {
		t.Fatal(err)
	}
	localFile := filepath.Join(nsshDir, filepath.Base(inventory.LocalProviderIncludeFile()))
	if err := os.WriteFile(localFile, []byte(localContent), 0600); err != nil {
		t.Fatal(err)
	}
	parser := sshconfig.NewParserWithPaths(mainConfig, filepath.Join(tmp, "backups"), 5)
	cfg := &config.Config{Inventory: config.InventoryConfig{
		Group: map[string]config.GroupConfig{
			"lab": {},
		},
	}}
	paths := &config.Paths{SSHConfigDir: sshDir, BackupDir: filepath.Join(tmp, "backups")}
	return parser, cfg, paths, localFile
}

func findTestHost(t *testing.T, hosts []*sshconfig.HostEntry, name string) *sshconfig.HostEntry {
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
