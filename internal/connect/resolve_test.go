package connect

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/credential"
	"github.com/ntwrknrd/nssh/internal/secret"
	"github.com/ntwrknrd/nssh/internal/ssh/connector"
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

func testSmartResolveConfig() *config.Config {
	cfg := config.DefaultConfig()
	cfg.Inventory.Provider = nil
	cfg.Inventory.Providers = map[string]config.InventoryProviderConfig{
		config.ProviderLocal: {
			Type: config.ProviderLocal,
			Groups: map[string]config.GroupConfig{
				"lab": {Auth: config.InventoryAuthConfig{Username: "group-user"}},
			},
			Hosts: map[string]config.InventoryHostConfig{
				"edge01.example.com": {
					Group:   "lab",
					Aliases: []string{"router-one"},
				},
				"810-cactimain01.ldap.custcbb.local": {
					Group: "lab",
				},
				"clab-dfz-core01": {
					Group:   "lab",
					Aliases: []string{"dfz-core01"},
				},
				"clab-dfz-core02": {
					Group:   "lab",
					Aliases: []string{"dfz-core02"},
				},
			},
		},
	}
	return cfg
}

func TestResolveSmartHostForConnectMatchesExactAliasesAndShortNames(t *testing.T) {
	cfg := testSmartResolveConfig()

	aliasResolved, err := ResolveSmartHostForConnect("router-one", "", cfg)
	if err != nil {
		t.Fatalf("ResolveSmartHostForConnect alias: %v", err)
	}
	shortResolved, err := ResolveSmartHostForConnect("edge01", "", cfg)
	if err != nil {
		t.Fatalf("ResolveSmartHostForConnect short: %v", err)
	}
	exactResolved, err := ResolveHostForConnect("edge01.example.com", "", cfg)
	if err != nil {
		t.Fatalf("ResolveHostForConnect exact: %v", err)
	}

	if aliasResolved.Canonical != exactResolved.Canonical || shortResolved.Canonical != exactResolved.Canonical {
		t.Fatalf("canonical alias=%q short=%q exact=%q", aliasResolved.Canonical, shortResolved.Canonical, exactResolved.Canonical)
	}
	if aliasResolved.Hostname != "edge01.example.com" || shortResolved.Hostname != "edge01.example.com" {
		t.Fatalf("hostname alias=%q short=%q, want edge01.example.com", aliasResolved.Hostname, shortResolved.Hostname)
	}
}

func TestResolveSmartHostForConnectAutoSelectsSinglePartialMatch(t *testing.T) {
	cfg := testSmartResolveConfig()
	oldSelectHost := selectHostFunc
	defer func() { selectHostFunc = oldSelectHost }()
	selectHostFunc = func(string, []string, string) (string, error) {
		t.Fatal("selector should not open for one partial match")
		return "", nil
	}

	resolved, err := ResolveSmartHostForConnect("cacti", "", cfg)
	if err != nil {
		t.Fatalf("ResolveSmartHostForConnect: %v", err)
	}
	if resolved.Canonical != "810-cactimain01.ldap.custcbb.local" {
		t.Fatalf("canonical = %q, want cacti host", resolved.Canonical)
	}
}

func TestResolveSmartHostForConnectUsesSelectorForMultiplePartialMatches(t *testing.T) {
	cfg := testSmartResolveConfig()
	oldSelectHost := selectHostFunc
	defer func() { selectHostFunc = oldSelectHost }()
	selectHostFunc = func(prompt string, options []string, initialQuery string) (string, error) {
		if prompt != "Select host" {
			t.Fatalf("prompt = %q, want Select host", prompt)
		}
		wantOptions := []string{"clab-dfz-core01", "clab-dfz-core02"}
		if !reflect.DeepEqual(options, wantOptions) {
			t.Fatalf("options = %#v, want %#v", options, wantOptions)
		}
		if initialQuery != "dfz" {
			t.Fatalf("initialQuery = %q, want dfz", initialQuery)
		}
		return "clab-dfz-core02", nil
	}

	resolved, err := ResolveSmartHostForConnect("dfz", "", cfg)
	if err != nil {
		t.Fatalf("ResolveSmartHostForConnect: %v", err)
	}
	if resolved.Canonical != "clab-dfz-core02" {
		t.Fatalf("canonical = %q, want clab-dfz-core02", resolved.Canonical)
	}
}

func TestResolveSmartHostForConnectMissReturnsHostNotFound(t *testing.T) {
	_, err := ResolveSmartHostForConnect("missing-edge", "", testSmartResolveConfig())
	var notFound *HostNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("ResolveSmartHostForConnect error = %v, want HostNotFoundError", err)
	}
	if notFound.Hostname != "missing-edge" {
		t.Fatalf("not found hostname = %q, want original query", notFound.Hostname)
	}
}

func TestResolveSmartHostForConnectPreservesExplicitUserWithPartialMatch(t *testing.T) {
	resolved, err := ResolveSmartHostForConnect("admin@cacti", "", testSmartResolveConfig())
	if err != nil {
		t.Fatalf("ResolveSmartHostForConnect: %v", err)
	}
	if resolved.Canonical != "810-cactimain01.ldap.custcbb.local" {
		t.Fatalf("canonical = %q, want cacti host", resolved.Canonical)
	}
	if resolved.Username != "admin" {
		t.Fatalf("username = %q, want explicit user", resolved.Username)
	}
}

func TestResolveHostForConnectEmitsPreSSHTimings(t *testing.T) {
	t.Setenv("NSSH_DEBUG", "1")
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	cfg := config.DefaultConfig()
	cfg.Inventory.Provider = nil
	cfg.Inventory.Providers = map[string]config.InventoryProviderConfig{
		config.ProviderLocal: {
			Type: config.ProviderLocal,
			Hosts: map[string]config.InventoryHostConfig{
				"edge01": {},
			},
		},
	}

	_, stderr := captureOutput(t, func() {
		resolved, err := ResolveHostForConnect("edge01", "", cfg)
		if err != nil {
			t.Fatalf("ResolveHostForConnect: %v", err)
		}
		if resolved.Hostname != "edge01" {
			t.Fatalf("hostname = %q, want edge01", resolved.Hostname)
		}
	})

	for _, stage := range []string{
		connector.TimingCatalogTotal,
		connector.TimingProviderStateList,
		connector.TimingProviderStateLoad,
		connector.TimingCatalogLocalHosts,
		connector.TimingCatalogProviderHosts,
		connector.TimingAuthResolve,
		connector.TimingCredentialRegistry,
		connector.TimingCredentialLookup,
	} {
		if !strings.Contains(stderr, "NSSH_TIMING:"+stage+":") {
			t.Fatalf("stderr = %q, want %s timing", stderr, stage)
		}
	}
}

func TestResolveBoundCredentialHostBindingWinsOverGroupBinding(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Credential.Provider = map[string]config.CredentialProviderConfig{
		"host-provider":  {Type: config.CredentialProviderSOPSAge, File: "~/.local/share/nssh/credentials.sops.yaml"},
		"group-provider": {Type: config.CredentialProviderSOPSAge, File: "~/.local/share/nssh/credentials.sops.yaml"},
	}
	cfg.Inventory.Host = map[string]config.InventoryHostConfig{
		"edge01": {Auth: config.InventoryAuthConfig{CredentialProvider: "host-provider", PasswordRef: "hosts.edge01.password"}},
	}
	labGroup := setTestGroupAuth(cfg, "lab", config.InventoryAuthConfig{CredentialProvider: "group-provider", PasswordRef: "groups.lab.password"})
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
		"sops":       {Type: config.CredentialProviderSOPSAge, File: "~/.local/share/nssh/credentials.sops.yaml"},
		"op-network": {Type: config.CredentialProvider1Password, Config: config.CredentialProviderDetailConfig{Vault: "Network"}},
	}
	labGroup := setTestGroupAuth(cfg, "lab", config.InventoryAuthConfig{CredentialProvider: "sops", PasswordRef: "groups.lab.password"})
	prodGroup := setTestGroupAuth(cfg, "prod", config.InventoryAuthConfig{CredentialProvider: "op-network", PasswordRef: "Network Shared Admin"})
	registry := fakeProviderRegistry{providers: map[string]credential.Provider{
		"sops":       fakeCredentialProvider{groups: map[string]*credential.Record{labGroup: {Username: "labuser", Secret: secret.NewFromString("labpass")}}},
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
		"sops": fakeCredentialProvider{err: errors.New("provider unavailable")},
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
		"sops": {Type: config.CredentialProviderSOPSAge, File: "~/.local/share/nssh/credentials.sops.yaml"},
	}
	cfg.Inventory.Host = map[string]config.InventoryHostConfig{
		"edge01": {AuthDisabled: true},
	}
	labGroup := setTestGroupAuth(cfg, "lab", config.InventoryAuthConfig{CredentialProvider: "sops", PasswordRef: "groups.lab.password"})
	registry := fakeProviderRegistry{providers: map[string]credential.Provider{
		"sops": fakeCredentialProvider{groups: map[string]*credential.Record{labGroup: {Username: "admin", Secret: secret.NewFromString("secret")}}},
	}}

	cred, err := resolveBoundCredential(cfg, registry, "edge01", labGroup, "")
	if err != nil {
		t.Fatalf("resolveBoundCredential: %v", err)
	}
	if cred != nil {
		t.Fatalf("credential = %+v, want nil when host auth is disabled", cred)
	}
}

func TestResolveLiteralHostForConnectBypassesCatalog(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.SSH.Defaults.Options = config.SSHOptions{
		"LogLevel": config.NewSSHOptionString("ERROR"),
	}

	resolved, err := ResolveLiteralHostForConnect("admin@192.0.2.10", "", cfg)
	if err != nil {
		t.Fatalf("ResolveLiteralHostForConnect: %v", err)
	}

	if resolved.Query != "admin@192.0.2.10" {
		t.Fatalf("query = %q, want original target", resolved.Query)
	}
	if resolved.Hostname != "192.0.2.10" || resolved.Canonical != "192.0.2.10" {
		t.Fatalf("host = %q canonical = %q, want literal address", resolved.Hostname, resolved.Canonical)
	}
	if resolved.Username != "admin" {
		t.Fatalf("username = %q, want admin", resolved.Username)
	}
	if resolved.Port != 22 {
		t.Fatalf("port = %d, want 22", resolved.Port)
	}
	if resolved.Credential != nil {
		t.Fatalf("credential = %+v, want nil for unmanaged literal target", resolved.Credential)
	}
	if got := resolved.SSH.Options["LogLevel"].Scalar; got != "ERROR" {
		t.Fatalf("default LogLevel = %q, want ERROR", got)
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
		"sops": {Type: config.CredentialProviderSOPSAge, File: "~/.local/share/nssh/credentials.sops.yaml"},
	}
	cfg.Inventory.Provider = nil
	cfg.Inventory.Providers = map[string]config.InventoryProviderConfig{
		config.ProviderLocal: {
			Type: config.ProviderLocal,
			Groups: map[string]config.GroupConfig{
				"customer": {Auth: config.InventoryAuthConfig{
					CredentialProvider: "sops",
					PasswordRef:        "groups.customer.password",
					Username:           "netops",
					Mode:               config.AuthModePassword,
				}},
			},
			Hosts: map[string]config.InventoryHostConfig{
				"edge01.example.com": {Group: "customer", Aliases: []string{"edge01"}},
			},
		},
	}

	resolved, err := ResolveHostForConnect("edge01", "", cfg)
	if err != nil {
		t.Fatalf("ResolveHostForConnect: %v", err)
	}
	if resolved.AuthMode != config.AuthModePassword {
		t.Fatalf("auth mode = %q, want %q", resolved.AuthMode, config.AuthModePassword)
	}
}

func TestResolveHostForConnectDoesNotUseGroupAuthForUngroupedLocalHost(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Inventory.Provider = nil
	cfg.Inventory.Providers = map[string]config.InventoryProviderConfig{
		config.ProviderLocal: {
			Type: config.ProviderLocal,
			Auth: config.InventoryAuthConfig{Mode: config.AuthModePassword, Username: "provider-user"},
			Groups: map[string]config.GroupConfig{
				"local": {Auth: config.InventoryAuthConfig{Username: "group-user"}},
			},
			Hosts: map[string]config.InventoryHostConfig{
				"edge01.example.com": {Aliases: []string{"edge01"}},
			},
		},
	}

	resolved, err := ResolveHostForConnect("edge01", "", cfg)
	if err != nil {
		t.Fatalf("ResolveHostForConnect: %v", err)
	}
	if resolved.Group != "" {
		t.Fatalf("group = %q, want empty", resolved.Group)
	}
	if resolved.Username != "provider-user" {
		t.Fatalf("username = %q, want provider-user", resolved.Username)
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
