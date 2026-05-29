package connect

import (
	"errors"
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/credential"
	"github.com/ntwrknrd/nssh/internal/secret"
)

type fakeCredentialProvider struct {
	hosts  map[string]*credential.Record
	groups map[string]*credential.Record
	err    error
	name   string
}

func (p fakeCredentialProvider) GetHost(host string) (*credential.Record, error) {
	if p.err != nil {
		return nil, p.err
	}
	return p.hosts[host], nil
}

func (p fakeCredentialProvider) GetGroup(group string) (*credential.Record, error) {
	if p.err != nil {
		return nil, p.err
	}
	return p.groups[group], nil
}

type fakeProviderRegistry struct {
	defaultName string
	providers   map[string]credential.Provider
}

func (r fakeProviderRegistry) Provider(name string) credential.Provider {
	return r.providers[name]
}

func (r fakeProviderRegistry) DefaultProviderName() string {
	return r.defaultName
}

func TestResolveBoundCredentialHostBindingWinsOverGroupBinding(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Credential.DefaultProvider = "host-provider"
	cfg.Credential.Provider = map[string]config.CredentialProviderConfig{
		"host-provider":  {Type: config.CredentialProviderPass},
		"group-provider": {Type: config.CredentialProviderPass},
	}
	cfg.Inventory.Host = map[string]config.InventoryHostConfig{
		"edge01": {Auth: config.InventoryAuthConfig{Provider: "host-provider", Ref: "nssh/hosts/edge01"}},
	}
	cfg.Inventory.Group = map[string]config.GroupConfig{
		"lab": {Auth: config.InventoryAuthConfig{Provider: "group-provider", Ref: "nssh/groups/lab"}},
	}
	registry := fakeProviderRegistry{defaultName: "host-provider", providers: map[string]credential.Provider{
		"host-provider":  fakeCredentialProvider{hosts: map[string]*credential.Record{"edge01": {Username: "hostuser", Secret: secret.NewFromString("hostpass")}}},
		"group-provider": fakeCredentialProvider{groups: map[string]*credential.Record{"lab": {Username: "groupuser", Secret: secret.NewFromString("grouppass")}}},
	}}

	cred, err := resolveBoundCredential(cfg, registry, "edge01", "lab", "")
	if err != nil {
		t.Fatalf("resolveBoundCredential: %v", err)
	}
	if cred == nil || cred.Source != CredSourceHost || cred.Username != "hostuser" {
		t.Fatalf("credential = %+v", cred)
	}
}

func TestResolveBoundCredentialSelectsDifferentGroupProviders(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Credential.DefaultProvider = "pass-local"
	cfg.Credential.Provider = map[string]config.CredentialProviderConfig{
		"pass-local": {Type: config.CredentialProviderPass},
		"op-network": {Type: config.CredentialProvider1Password, Config: config.CredentialProviderDetailConfig{Vault: "Network"}},
	}
	cfg.Inventory.Group = map[string]config.GroupConfig{
		"lab":  {Auth: config.InventoryAuthConfig{Provider: "pass-local", Ref: "nssh/groups/lab"}},
		"prod": {Auth: config.InventoryAuthConfig{Provider: "op-network", Ref: "Network Shared Admin"}},
	}
	registry := fakeProviderRegistry{defaultName: "pass-local", providers: map[string]credential.Provider{
		"pass-local": fakeCredentialProvider{groups: map[string]*credential.Record{"lab": {Username: "labuser", Secret: secret.NewFromString("labpass")}}},
		"op-network": fakeCredentialProvider{groups: map[string]*credential.Record{"prod": {Username: "produser", Secret: secret.NewFromString("prodpass")}}},
	}}

	lab, err := resolveBoundCredential(cfg, registry, "edge01", "lab", "")
	if err != nil {
		t.Fatalf("lab resolve: %v", err)
	}
	prod, err := resolveBoundCredential(cfg, registry, "edge02", "prod", "")
	if err != nil {
		t.Fatalf("prod resolve: %v", err)
	}
	if lab == nil || lab.Username != "labuser" || prod == nil || prod.Username != "produser" {
		t.Fatalf("lab=%+v prod=%+v", lab, prod)
	}
}

func TestResolveBoundCredentialSkipsProviderWhenNoBinding(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Inventory.Host = nil
	cfg.Inventory.Group = map[string]config.GroupConfig{
		"lab": {},
	}
	registry := fakeProviderRegistry{defaultName: "pass-local", providers: map[string]credential.Provider{
		"pass-local": fakeCredentialProvider{err: errors.New("provider unavailable")},
	}}

	cred, err := resolveBoundCredential(cfg, registry, "edge01", "lab", "")
	if err != nil {
		t.Fatalf("provider error should not matter without a binding: %v", err)
	}
	if cred != nil {
		t.Fatalf("credential = %+v, want nil", cred)
	}
}

func TestResolveBoundCredentialUsesDefaultProviderForAuthWithoutProvider(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Credential.DefaultProvider = "pass-local"
	cfg.Credential.Provider = map[string]config.CredentialProviderConfig{
		"pass-local": {Type: config.CredentialProviderPass},
	}
	cfg.Inventory.Group = map[string]config.GroupConfig{
		"lab": {Auth: config.InventoryAuthConfig{Ref: "nssh/groups/lab"}},
	}
	registry := fakeProviderRegistry{defaultName: "pass-local", providers: map[string]credential.Provider{
		"pass-local": fakeCredentialProvider{groups: map[string]*credential.Record{"lab": {Username: "labuser", Secret: secret.NewFromString("labpass")}}},
	}}

	cred, err := resolveBoundCredential(cfg, registry, "edge01", "lab", "")
	if err != nil {
		t.Fatalf("resolveBoundCredential: %v", err)
	}
	if cred == nil || cred.Source != CredSourceGroup || cred.Username != "labuser" {
		t.Fatalf("credential = %+v", cred)
	}
}

func TestResolveBoundCredentialUsesRequestedUsernameWhenRecordOmitsUsername(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Credential.DefaultProvider = "op-network"
	cfg.Credential.Provider = map[string]config.CredentialProviderConfig{
		"op-network": {Type: config.CredentialProvider1Password, Config: config.CredentialProviderDetailConfig{Vault: "Network"}},
	}
	cfg.Inventory.Group = map[string]config.GroupConfig{
		"custcbb": {Auth: config.InventoryAuthConfig{Provider: "op-network", Ref: "op://Network/Shared/password"}},
	}
	registry := fakeProviderRegistry{defaultName: "op-network", providers: map[string]credential.Provider{
		"op-network": fakeCredentialProvider{groups: map[string]*credential.Record{"custcbb": {Secret: secret.NewFromString("secret")}}},
	}}

	cred, err := resolveBoundCredential(cfg, registry, "edge01", "custcbb", "chris.jones")
	if err != nil {
		t.Fatalf("resolveBoundCredential: %v", err)
	}
	if cred == nil || cred.Source != CredSourceGroup || cred.Username != "chris.jones" {
		t.Fatalf("credential = %+v", cred)
	}
}
