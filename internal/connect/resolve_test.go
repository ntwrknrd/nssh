package connect

import (
	"context"
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

func (p fakeCredentialProvider) GetRef(ref config.CredentialRefConfig) (*credential.Record, error) {
	if p.err != nil {
		return nil, p.err
	}
	for _, record := range p.hosts {
		return record, nil
	}
	for _, record := range p.groups {
		return record, nil
	}
	return &credential.Record{Username: ref.Username, Secret: secret.NewFromString("secret"), Ref: ref.Ref}, nil
}

type fakeProviderRegistry struct {
	providers map[string]credential.Provider
}

func (r fakeProviderRegistry) Provider(name string) credential.Provider {
	return r.providers[name]
}

type countingCredentialProvider struct {
	calls  []config.CredentialRefConfig
	record *credential.Record
}

func (p *countingCredentialProvider) GetHost(host string) (*credential.Record, error) {
	return nil, nil
}

func (p *countingCredentialProvider) GetGroup(group string) (*credential.Record, error) {
	return nil, nil
}

func (p *countingCredentialProvider) GetRef(ref config.CredentialRefConfig) (*credential.Record, error) {
	p.calls = append(p.calls, ref)
	return p.record, nil
}

func setTestGroupAuth(cfg *config.Config, group string, auth config.InventoryAuthConfig) string {
	if cfg.Inventory.Provider == nil {
		cfg.Inventory.Provider = make(map[string]config.InventoryProviderConfig)
	}
	localProvider := cfg.Inventory.Provider[config.ProviderLocal]
	localProvider.Type = config.ProviderLocal
	if localProvider.Group == nil {
		localProvider.Group = make(map[string]config.GroupConfig)
	}
	localProvider.Group[group] = config.GroupConfig{Auth: auth}
	cfg.Inventory.Provider[config.ProviderLocal] = localProvider
	return config.FormatInventoryGroupID(config.ProviderLocal, group)
}

func TestResolveBoundCredentialHostBindingWinsOverGroupBinding(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Credential.Provider = map[string]config.CredentialProviderConfig{
		"host-provider":  {Type: config.CredentialProviderPass},
		"group-provider": {Type: config.CredentialProviderPass},
	}
	cfg.Inventory.Host = map[string]config.InventoryHostConfig{
		"edge01": {Auth: config.InventoryAuthConfig{CredentialProvider: "host-provider", PasswordRef: "nssh/hosts/edge01"}},
	}
	labGroup := setTestGroupAuth(cfg, "lab", config.InventoryAuthConfig{CredentialProvider: "group-provider", PasswordRef: "nssh/groups/lab"})
	registry := fakeProviderRegistry{providers: map[string]credential.Provider{
		"host-provider":  fakeCredentialProvider{hosts: map[string]*credential.Record{"edge01": {Username: "hostuser", Secret: secret.NewFromString("hostpass")}}},
		"group-provider": fakeCredentialProvider{groups: map[string]*credential.Record{labGroup: {Username: "groupuser", Secret: secret.NewFromString("grouppass")}}},
	}}

	cred, err := resolveBoundCredential(cfg, registry, "edge01", labGroup, "")
	if err != nil {
		t.Fatalf("resolveBoundCredential: %v", err)
	}
	if cred == nil || cred.Source != CredSourceHost || cred.Username != "hostuser" {
		t.Fatalf("credential = %+v", cred)
	}
}

func TestResolveBoundCredentialSelectsDifferentGroupProviders(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Credential.Provider = map[string]config.CredentialProviderConfig{
		"pass-local": {Type: config.CredentialProviderPass},
		"op-network": {Type: config.CredentialProvider1Password, Config: config.CredentialProviderDetailConfig{Vault: "Network"}},
	}
	labGroup := setTestGroupAuth(cfg, "lab", config.InventoryAuthConfig{CredentialProvider: "pass-local", PasswordRef: "nssh/groups/lab"})
	prodGroup := setTestGroupAuth(cfg, "prod", config.InventoryAuthConfig{CredentialProvider: "op-network", PasswordRef: "Network Shared Admin"})
	registry := fakeProviderRegistry{providers: map[string]credential.Provider{
		"pass-local": fakeCredentialProvider{groups: map[string]*credential.Record{labGroup: {Username: "labuser", Secret: secret.NewFromString("labpass")}}},
		"op-network": fakeCredentialProvider{groups: map[string]*credential.Record{prodGroup: {Username: "produser", Secret: secret.NewFromString("prodpass")}}},
	}}

	lab, err := resolveBoundCredential(cfg, registry, "edge01", labGroup, "")
	if err != nil {
		t.Fatalf("lab resolve: %v", err)
	}
	prod, err := resolveBoundCredential(cfg, registry, "edge02", prodGroup, "")
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
	labGroup := setTestGroupAuth(cfg, "lab", config.InventoryAuthConfig{})
	registry := fakeProviderRegistry{providers: map[string]credential.Provider{
		"pass-local": fakeCredentialProvider{err: errors.New("provider unavailable")},
	}}

	cred, err := resolveBoundCredential(cfg, registry, "edge01", labGroup, "")
	if err != nil {
		t.Fatalf("provider error should not matter without a binding: %v", err)
	}
	if cred != nil {
		t.Fatalf("credential = %+v, want nil", cred)
	}
}

func TestResolveBoundCredentialUsesRequestedUsernameWhenRecordOmitsUsername(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Credential.Provider = map[string]config.CredentialProviderConfig{
		"op-network": {Type: config.CredentialProvider1Password, Config: config.CredentialProviderDetailConfig{Vault: "Network"}},
	}
	customerGroup := setTestGroupAuth(cfg, "customer", config.InventoryAuthConfig{CredentialProvider: "op-network", PasswordRef: "op://Network/Shared/password"})
	registry := fakeProviderRegistry{providers: map[string]credential.Provider{
		"op-network": fakeCredentialProvider{groups: map[string]*credential.Record{customerGroup: {Secret: secret.NewFromString("secret")}}},
	}}

	cred, err := resolveBoundCredential(cfg, registry, "edge01", customerGroup, "netops")
	if err != nil {
		t.Fatalf("resolveBoundCredential: %v", err)
	}
	if cred == nil || cred.Source != CredSourceGroup || cred.Username != "netops" {
		t.Fatalf("credential = %+v", cred)
	}
}

func TestResolveBoundCredentialSkipsMismatchedExplicitUsername(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Credential.Provider = map[string]config.CredentialProviderConfig{
		"op-network": {Type: config.CredentialProvider1Password, Config: config.CredentialProviderDetailConfig{Vault: "Network"}},
	}
	customerGroup := setTestGroupAuth(cfg, "customer", config.InventoryAuthConfig{CredentialProvider: "op-network", PasswordRef: "Network Shared Admin"})
	registry := fakeProviderRegistry{providers: map[string]credential.Provider{
		"op-network": fakeCredentialProvider{groups: map[string]*credential.Record{customerGroup: {Username: "admin", Secret: secret.NewFromString("secret")}}},
	}}

	cred, err := resolveBoundCredential(cfg, registry, "edge01", customerGroup, "netops")
	if err != nil {
		t.Fatalf("resolveBoundCredential: %v", err)
	}
	if cred != nil {
		t.Fatalf("credential = %+v, want nil for explicit username mismatch", cred)
	}
}

func TestResolveBoundCredentialSkipsDisabledHostAuth(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Credential.Provider = map[string]config.CredentialProviderConfig{
		"pass-local": {Type: config.CredentialProviderPass},
	}
	cfg.Inventory.Host = map[string]config.InventoryHostConfig{
		"edge01": {AuthDisabled: true},
	}
	labGroup := setTestGroupAuth(cfg, "lab", config.InventoryAuthConfig{CredentialProvider: "pass-local", PasswordRef: "nssh/groups/lab"})
	registry := fakeProviderRegistry{providers: map[string]credential.Provider{
		"pass-local": fakeCredentialProvider{groups: map[string]*credential.Record{labGroup: {Username: "admin", Secret: secret.NewFromString("secret")}}},
	}}

	cred, err := resolveBoundCredential(cfg, registry, "edge01", labGroup, "")
	if err != nil {
		t.Fatalf("resolveBoundCredential: %v", err)
	}
	if cred != nil {
		t.Fatalf("credential = %+v, want nil when host auth is disabled", cred)
	}
}

func TestResolveInventoryCredentialDefersDirectPasswordRefWithLiteralUsername(t *testing.T) {
	provider := &countingCredentialProvider{
		record: &credential.Record{Username: "netops", Secret: secret.NewFromString("secret"), Ref: "op://Network/Edge/password"},
	}
	registry := fakeProviderRegistry{providers: map[string]credential.Provider{"op-network": provider}}
	auth := config.InventoryAuthResolution{
		CredentialProvider: "op-network",
		PasswordRef:        "op://Network/Edge/password",
		Username:           "netops",
		Source:             "group local/customer",
	}

	cred, err := resolveInventoryCredential(registry, auth, "")
	if err != nil {
		t.Fatalf("resolveInventoryCredential: %v", err)
	}
	if cred == nil || cred.Username != "netops" || cred.Password != nil || cred.PasswordResolver == nil {
		t.Fatalf("credential = %+v, want lazy resolver with literal username", cred)
	}
	if len(provider.calls) != 0 {
		t.Fatalf("provider calls = %d, want 0 before password prompt", len(provider.calls))
	}
	resolved, err := cred.PasswordResolver(context.Background())
	if err != nil {
		t.Fatalf("PasswordResolver: %v", err)
	}
	if resolved == nil {
		t.Fatal("resolved password is nil")
	}
	if len(provider.calls) != 1 {
		t.Fatalf("provider calls after resolver = %d, want 1", len(provider.calls))
	}
}

func TestResolveHostForConnectCarriesResolvedAuthMode(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Credential.Provider = map[string]config.CredentialProviderConfig{
		"op-network": {Type: config.CredentialProvider1Password},
	}
	cfg.Inventory.Auth = config.InventoryAuthConfig{
		CredentialProvider: "op-network",
		PasswordRef:        "op://Network/Edge/password",
		Username:           "netops",
		AuthMode:           config.AuthModePassword,
	}

	resolved, err := ResolveHostForConnect("edge01", "", cfg)
	if err != nil {
		t.Fatalf("ResolveHostForConnect: %v", err)
	}
	if resolved.AuthMode != config.AuthModePassword {
		t.Fatalf("auth mode = %q, want %q", resolved.AuthMode, config.AuthModePassword)
	}
}

func TestCredentialTargetFromResolvedHostExposesSafeMetadataAndResolver(t *testing.T) {
	cred := &ResolvedCredential{
		Username: "netops",
		Source:   CredSourceGroup,
		PasswordResolver: func(ctx context.Context) (*secret.Secret, error) {
			return secret.NewFromString("secret"), nil
		},
	}
	resolved := &ResolvedHost{
		Hostname:   "edge01",
		Username:   "netops",
		Credential: cred,
	}

	target, err := CredentialTargetFromResolvedHost(resolved)
	if err != nil {
		t.Fatalf("CredentialTargetFromResolvedHost: %v", err)
	}
	if target.Host != "edge01" || target.UsernamePresent != true || target.Source != CredSourceGroup {
		t.Fatalf("target = %+v", target)
	}
	if target.Resolver == nil {
		t.Fatal("resolver is nil")
	}
	if target.Password != nil {
		t.Fatal("target exposes password before benchmark lookup")
	}
}

func TestCredentialTargetFromResolvedHostRejectsMissingCredential(t *testing.T) {
	_, err := CredentialTargetFromResolvedHost(&ResolvedHost{Hostname: "edge01"})
	if err == nil || err.Error() != "host edge01 has no configured credential" {
		t.Fatalf("error = %v, want no configured credential", err)
	}
}

func TestResolveInventoryCredentialResolvesUsernameRefBeforeSSHStart(t *testing.T) {
	provider := &countingCredentialProvider{
		record: &credential.Record{Username: "netops", Secret: secret.NewFromString("secret"), Ref: "op://Network/Edge/password"},
	}
	registry := fakeProviderRegistry{providers: map[string]credential.Provider{"op-network": provider}}
	auth := config.InventoryAuthResolution{
		CredentialProvider: "op-network",
		PasswordRef:        "op://Network/Edge/password",
		UsernameRef:        "op://Network/Edge/username",
	}

	cred, err := resolveInventoryCredential(registry, auth, "")
	if err != nil {
		t.Fatalf("resolveInventoryCredential: %v", err)
	}
	if cred == nil || cred.Username != "netops" {
		t.Fatalf("credential = %+v, want username resolved before SSH start", cred)
	}
	if len(provider.calls) != 1 {
		t.Fatalf("provider calls = %d, want 1 for username_ref", len(provider.calls))
	}
}

func TestSelectConnectionUsernameKeepsGroupUserAheadOfCredentialUsername(t *testing.T) {
	got := selectConnectionUsername(true, "", "stale-ssh-user", "chris.jones", "chris.jones@custcbb.local", "cj")
	if got != "chris.jones" {
		t.Fatalf("username = %q, want group default", got)
	}
}

func TestSelectConnectionUsernameUsesCredentialUsernameWhenInventoryUserMissing(t *testing.T) {
	got := selectConnectionUsername(true, "", "stale-ssh-user", "", "admin", "cj")
	if got != "admin" {
		t.Fatalf("username = %q, want credential username", got)
	}
}

func TestSelectConnectionUsernameKeepsSSHUserForUnmanagedHosts(t *testing.T) {
	got := selectConnectionUsername(false, "", "ssh-user", "", "credential-user", "cj")
	if got != "ssh-user" {
		t.Fatalf("username = %q, want ssh config user", got)
	}
}
