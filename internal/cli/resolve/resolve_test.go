package resolve

import (
	"testing"

	"github.com/ntwrknrd/nssh/internal/credential"
	"github.com/ntwrknrd/nssh/internal/secret"
	"github.com/ntwrknrd/nssh/internal/vault"
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

func TestResolveTargetCredentialHostOverridesGroup(t *testing.T) {
	provider := fakeCredentialProvider{
		hosts: map[string]*credential.Record{
			"edge01": {Username: "hostuser", Secret: secret.NewFromString("hostpass")},
		},
		groups: map[string]*credential.Record{
			"lab": {Username: "groupuser", Secret: secret.NewFromString("grouppass")},
		},
	}

	cred, err := resolveTargetCredential(provider, "edge01", "lab", "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if cred == nil {
		t.Fatal("expected credential")
	}
	if cred.Source != vault.CredSourceHost {
		t.Fatalf("source = %q, want host", cred.Source)
	}
	if cred.Username != "hostuser" {
		t.Fatalf("username = %q", cred.Username)
	}
}

func TestResolveTargetCredentialFallsBackToGroup(t *testing.T) {
	provider := fakeCredentialProvider{
		hosts: map[string]*credential.Record{},
		groups: map[string]*credential.Record{
			"lab": {Username: "groupuser", Secret: secret.NewFromString("grouppass")},
		},
	}

	cred, err := resolveTargetCredential(provider, "edge01", "lab", "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if cred == nil {
		t.Fatal("expected credential")
	}
	if cred.Source != vault.CredSourceGroup {
		t.Fatalf("source = %q, want group", cred.Source)
	}
	if cred.Username != "groupuser" {
		t.Fatalf("username = %q", cred.Username)
	}
}

func TestResolveTargetCredentialRespectsExplicitUsername(t *testing.T) {
	provider := fakeCredentialProvider{
		hosts: map[string]*credential.Record{
			"edge01": {Username: "hostuser", Secret: secret.NewFromString("hostpass")},
		},
		groups: map[string]*credential.Record{
			"lab": {Username: "requested", Secret: secret.NewFromString("grouppass")},
		},
	}

	cred, err := resolveTargetCredential(provider, "edge01", "lab", "requested")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if cred == nil {
		t.Fatal("expected credential")
	}
	if cred.Source != vault.CredSourceGroup {
		t.Fatalf("source = %q, want group", cred.Source)
	}
	if cred.Username != "requested" {
		t.Fatalf("username = %q", cred.Username)
	}
}
