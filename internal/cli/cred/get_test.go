package cred

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/credential"
	"github.com/ntwrknrd/nssh/internal/secret"
	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
)

type fakeCredentialProvider struct {
	hosts  map[string]*credential.Record
	groups map[string]*credential.Record
}

func (p fakeCredentialProvider) GetHost(host string) (*credential.Record, error) {
	return p.hosts[host], nil
}

func (p fakeCredentialProvider) SetHost(string, *credential.Record) error {
	return nil
}

func (p fakeCredentialProvider) RemoveHost(string) (bool, error) {
	return false, nil
}

func (p fakeCredentialProvider) GetGroup(group string) (*credential.Record, error) {
	return p.groups[group], nil
}

func (p fakeCredentialProvider) SetGroup(string, *credential.Record) error {
	return nil
}

func (p fakeCredentialProvider) RemoveGroup(string) (bool, error) {
	return false, nil
}

func (p fakeCredentialProvider) Status() credential.Status {
	return credential.Status{Type: "fake", Available: true}
}

func TestCredentialScopeArgUsesPositionalHostByDefault(t *testing.T) {
	scope, err := credentialScopeArg([]string{"edge01"}, "")
	if err != nil {
		t.Fatalf("credentialScopeArg: %v", err)
	}
	if scope.Host != "edge01" || scope.Group != "" {
		t.Fatalf("scope = %+v, want host edge01", scope)
	}
}

func TestCredentialScopeArgUsesGroupFlag(t *testing.T) {
	scope, err := credentialScopeArg(nil, "lab")
	if err != nil {
		t.Fatalf("credentialScopeArg group: %v", err)
	}
	if scope.Host != "" || scope.Group != "lab" {
		t.Fatalf("scope = %+v, want group lab", scope)
	}
}

func TestCredentialScopeArgRejectsHostAndGroup(t *testing.T) {
	_, err := credentialScopeArg([]string{"edge01"}, "lab")
	if err == nil || !strings.Contains(err.Error(), "host and --group are mutually exclusive") {
		t.Fatalf("credentialScopeArg error = %v, want mutual exclusion", err)
	}
}

func TestHostCredentialViewFallsBackToGroupCredential(t *testing.T) {
	cfg, parser := testCredentialInventory(t, "lab", "edge01")
	provider := fakeCredentialProvider{
		hosts: map[string]*credential.Record{},
		groups: map[string]*credential.Record{
			"lab": {Username: "groupuser", Secret: secret.NewFromString("grouppass")},
		},
	}

	view, err := hostCredentialView(provider, cfg, parser, "edge01")
	if err != nil {
		t.Fatalf("hostCredentialView: %v", err)
	}
	if view.HostOverride != "-" {
		t.Fatalf("HostOverride = %q, want -", view.HostOverride)
	}
	if view.Group != "lab" {
		t.Fatalf("Group = %q, want lab", view.Group)
	}
	if view.EffectiveSource != "group lab" {
		t.Fatalf("EffectiveSource = %q, want group lab", view.EffectiveSource)
	}
	if view.Record == nil || view.Record.Username != "groupuser" {
		t.Fatalf("Record = %+v, want group credential", view.Record)
	}
}

func TestHostCredentialViewPrefersHostOverride(t *testing.T) {
	cfg, parser := testCredentialInventory(t, "lab", "edge01")
	provider := fakeCredentialProvider{
		hosts: map[string]*credential.Record{
			"edge01": {Username: "hostuser", Secret: secret.NewFromString("hostpass")},
		},
		groups: map[string]*credential.Record{
			"lab": {Username: "groupuser", Secret: secret.NewFromString("grouppass")},
		},
	}

	view, err := hostCredentialView(provider, cfg, parser, "edge01")
	if err != nil {
		t.Fatalf("hostCredentialView: %v", err)
	}
	if view.HostOverride != "set" {
		t.Fatalf("HostOverride = %q, want set", view.HostOverride)
	}
	if view.EffectiveSource != "host edge01" {
		t.Fatalf("EffectiveSource = %q, want host edge01", view.EffectiveSource)
	}
	if view.Record == nil || view.Record.Username != "hostuser" {
		t.Fatalf("Record = %+v, want host credential", view.Record)
	}
}

func testCredentialInventory(t *testing.T, group, host string) (*config.Config, *sshconfig.Parser) {
	t.Helper()
	tmpDir := t.TempDir()
	sshDir := filepath.Join(tmpDir, ".ssh")
	includeDir := filepath.Join(sshDir, "nssh.d")
	if err := os.MkdirAll(includeDir, 0700); err != nil {
		t.Fatal(err)
	}
	configFile := filepath.Join(sshDir, "config")
	groupFile := filepath.Join(includeDir, "local_"+group+".conf")
	if err := os.WriteFile(configFile, []byte("Include "+groupFile+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(groupFile, []byte("Host "+host+"\n  HostName "+host+".example.com\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Inventory: config.InventoryConfig{
		DefaultGroup: group,
		Group: map[string]config.GroupConfig{
			group: {LocalFile: filepath.Base(groupFile)},
		},
	}}
	parser := sshconfig.NewParserWithPaths(configFile, filepath.Join(tmpDir, "backups"), 5)
	return cfg, parser
}
