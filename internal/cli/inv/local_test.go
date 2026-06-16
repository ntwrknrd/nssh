package inv

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/credential"
	"github.com/ntwrknrd/nssh/internal/inventory"
	"github.com/ntwrknrd/nssh/internal/secret"
	"github.com/ntwrknrd/nssh/internal/ssh/compat"
	"github.com/ntwrknrd/nssh/internal/ssh/connector"
	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
	"github.com/ntwrknrd/nssh/internal/ui"
)

func TestUpsertLocalHostWritesSingleLocalProviderFile(t *testing.T) {
	tmp := t.TempDir()
	mainConfig := filepath.Join(tmp, ".ssh", "config")
	parser := sshconfig.NewParserWithPaths(mainConfig, filepath.Join(tmp, "backups"), 5)
	cfg := &config.Config{Inventory: config.InventoryConfig{
		Group: map[string]config.GroupConfig{
			"lab": {},
		},
	}}
	paths := &config.Paths{ConfigDir: filepath.Join(tmp, "nssh"), ConfigFile: filepath.Join(tmp, "nssh", "config.yaml"), BackupDir: filepath.Join(tmp, "backups")}

	err := upsertLocalHost(parser, cfg, paths, hostPatch{
		Host:     "edge01",
		Group:    "local/lab",
		HostName: "edge01.lab.local",
		User:     "admin",
		Port:     2222,
		PortSet:  true,
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	content, err := os.ReadFile(localProviderYAMLPath(cfg, paths))
	if err != nil {
		t.Fatalf("read local file: %v", err)
	}
	got := string(content)
	for _, want := range []string{"edge01:", "group: lab", "hostname: edge01.lab.local", "port: 2222", "username: admin"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestLocalProviderOwnerLabelUsesLocalProviderFile(t *testing.T) {
	tmp := t.TempDir()
	paths := &config.Paths{ConfigDir: filepath.Join(tmp, "nssh")}

	got := localProviderOwnerLabel(paths)
	want := "Inventory Filepath: " + filepath.Join(paths.ConfigDir, "inventory", "local.yaml")
	if got != want {
		t.Fatalf("label = %q, want %q", got, want)
	}
}

func TestPromptLocalHostAddDetailsCollectsHostHostnameUserPortAndAuth(t *testing.T) {
	cfg := &config.Config{
		Credential: config.CredentialConfig{Provider: map[string]config.CredentialProviderConfig{
			"op-network": {Type: config.CredentialProvider1Password, Config: config.CredentialProviderDetailConfig{Vault: "Network"}},
		}},
		Inventory: config.InventoryConfig{Group: map[string]config.GroupConfig{
			"custcbb": {DomainSuffix: []string{".custcbb.local"}, Auth: config.InventoryAuthConfig{Username: "netops"}},
		}},
	}
	prompter := &fakeLocalHostAddPrompter{
		inputs: map[string]string{
			"Host":     "810-neteng01",
			"HostName": "810-neteng01.custcbb.local",
			"Port":     "2222",
		},
		selects: map[string]string{
			"Authentication":      config.AuthModePassword,
			"Credential source":   "host",
			"Credential provider": "op-network",
			"Credential item":     "op://Network/810-neteng01/password",
		},
	}
	old := listCredentialItems
	listCredentialItems = func(*config.Config, string) ([]credentialItem, error) {
		return []credentialItem{{Label: "810-neteng01", Ref: "op://Network/810-neteng01/password"}}, nil
	}
	defer func() { listCredentialItems = old }()

	patch, err := promptLocalHostAddDetails(cfg, hostPatch{Host: "810-neteng01", Group: "local/custcbb"}, prompter)
	if err != nil {
		t.Fatalf("promptLocalHostAddDetails: %v", err)
	}

	if patch.Host != "810-neteng01" || patch.HostName != "810-neteng01.custcbb.local" {
		t.Fatalf("host fields = %q/%q", patch.Host, patch.HostName)
	}
	if patch.User != "" || patch.Port != 2222 || !patch.PortSet {
		t.Fatalf("user/port = %q/%d/%v", patch.User, patch.Port, patch.PortSet)
	}
	if patch.AuthMode != config.AuthModePassword {
		t.Fatalf("auth mode = %q, want password", patch.AuthMode)
	}
	if patch.Auth.CredentialProvider != "op-network" || patch.Auth.PasswordRef != "op://Network/810-neteng01/password" || patch.Auth.Username != "" || patch.Auth.UsernameRef != "" {
		t.Fatalf("auth = %+v", patch.Auth)
	}
	if patch.AuthDisabled {
		t.Fatal("host stored credential should not disable auth")
	}
	wantPrompts := "Host,HostName,Port,Authentication,Credential source,Credential provider,Credential item"
	if got := strings.Join(prompter.prompts, ","); got != wantPrompts {
		t.Fatalf("prompts = %s, want %s", got, wantPrompts)
	}
}

func TestPromptLocalHostAddDetailsPromptsUserForPublicKey(t *testing.T) {
	cfg := &config.Config{
		Inventory: config.InventoryConfig{Group: map[string]config.GroupConfig{
			"lab": {Auth: config.InventoryAuthConfig{Username: "netops"}},
		}},
	}
	prompter := &fakeLocalHostAddPrompter{
		inputs: map[string]string{
			"User": "admin",
		},
		selects: map[string]string{
			"Authentication": config.AuthModeKey,
		},
	}

	patch, err := promptLocalHostAddDetails(cfg, hostPatch{Host: "edge01", Group: "local/lab"}, prompter)
	if err != nil {
		t.Fatalf("promptLocalHostAddDetails: %v", err)
	}
	if patch.AuthMode != config.AuthModeKey || patch.User != "admin" {
		t.Fatalf("patch = %+v", patch)
	}
	if patch.Auth.Username != "admin" || patch.Auth.AuthMode != config.AuthModeKey || patch.AuthDisabled {
		t.Fatalf("public key auth = %+v disabled=%v", patch.Auth, patch.AuthDisabled)
	}
	wantPrompts := "Host,HostName,Port,Authentication,User"
	if got := strings.Join(prompter.prompts, ","); got != wantPrompts {
		t.Fatalf("prompts = %s, want %s", got, wantPrompts)
	}
}

func TestPromptLocalHostAddDetailsPasswordWithoutStoredCredentialPromptsUser(t *testing.T) {
	cfg := &config.Config{
		Inventory: config.InventoryConfig{Group: map[string]config.GroupConfig{
			"lab": {Auth: config.InventoryAuthConfig{CredentialProvider: "pass-local", PasswordRef: "nssh/groups/lab"}},
		}},
	}
	prompter := &fakeLocalHostAddPrompter{
		inputs: map[string]string{
			"User": "admin",
		},
		selects: map[string]string{
			"Authentication":    config.AuthModePassword,
			"Credential source": "none",
		},
	}

	patch, err := promptLocalHostAddDetails(cfg, hostPatch{Host: "edge01", Group: "local/lab"}, prompter)
	if err != nil {
		t.Fatalf("promptLocalHostAddDetails: %v", err)
	}
	if patch.User != "admin" || patch.Auth.Username != "admin" || patch.Auth.AuthMode != config.AuthModePassword || patch.AuthDisabled {
		t.Fatalf("patch = %+v", patch)
	}
	wantPrompts := "Host,HostName,Port,Authentication,Credential source,User"
	if got := strings.Join(prompter.prompts, ","); got != wantPrompts {
		t.Fatalf("prompts = %s, want %s", got, wantPrompts)
	}
}

func TestPromptLocalHostAddDetailsCanUseGroupCredential(t *testing.T) {
	cfg := &config.Config{
		Credential: config.CredentialConfig{Provider: map[string]config.CredentialProviderConfig{
			"op-expedient": {Type: config.CredentialProvider1Password, Config: config.CredentialProviderDetailConfig{Vault: "Expedient"}},
		}},
		Inventory: config.InventoryConfig{Group: map[string]config.GroupConfig{
			"lab": {Auth: config.InventoryAuthConfig{CredentialProvider: "op-expedient", PasswordRef: "op://Expedient/bdmuxl2pscoec17gsdt5geodzu/password"}},
		}},
	}
	prompter := &fakeLocalHostAddPrompter{
		selects: map[string]string{
			"Authentication":    config.AuthModePassword,
			"Credential source": "group",
		},
	}

	patch, err := promptLocalHostAddDetails(cfg, hostPatch{Host: "edge01", Group: "local/lab"}, prompter)
	if err != nil {
		t.Fatalf("promptLocalHostAddDetails: %v", err)
	}
	if patch.Auth.IsSet() {
		t.Fatalf("host-specific auth = %+v, want unset when using group credential", patch.Auth)
	}
	if patch.AuthMode != config.AuthModePassword {
		t.Fatalf("auth mode = %q, want password", patch.AuthMode)
	}
	options := prompter.options["Credential source"]
	if len(options) == 0 {
		t.Fatal("Credential source options were not captured")
	}
	if got := options[0].Label; strings.Contains(got, "/password") || !strings.Contains(got, "op://Expedient/bdmuxl2pscoec17gsdt5geodzu") {
		t.Fatalf("group credential label = %q", got)
	}
}

func TestPromptLocalHostAddDetailsCanBackOutOfCredentialProvider(t *testing.T) {
	cfg := &config.Config{
		Credential: config.CredentialConfig{Provider: map[string]config.CredentialProviderConfig{
			"op-network": {Type: config.CredentialProvider1Password, Config: config.CredentialProviderDetailConfig{Vault: "Network"}},
		}},
	}
	prompter := &fakeLocalHostAddPrompter{
		inputs: map[string]string{
			"User": "admin",
		},
		selectQueue: map[string][]string{
			"Authentication":      {config.AuthModePassword, config.AuthModeKey},
			"Credential source":   {"host", promptBackValue},
			"Credential provider": {promptBackValue},
		},
	}

	patch, err := promptLocalHostAddDetails(cfg, hostPatch{Host: "edge01", Group: "local/lab"}, prompter)
	if err != nil {
		t.Fatalf("promptLocalHostAddDetails: %v", err)
	}
	if patch.AuthMode != config.AuthModeKey || patch.User != "admin" {
		t.Fatalf("patch = %+v, want public key admin after backing out", patch)
	}
	wantPrompts := "Host,HostName,Port,Authentication,Credential source,Credential provider,Credential source,Authentication,User"
	if got := strings.Join(prompter.prompts, ","); got != wantPrompts {
		t.Fatalf("prompts = %s, want %s", got, wantPrompts)
	}
	if options := prompter.options["Credential provider"]; options[len(options)-1].Value != promptBackValue {
		t.Fatalf("credential provider options missing back: %+v", options)
	}
}

func TestApplyInteractiveHostAuthSelectionClearsStaleHostAuthForGroupCredential(t *testing.T) {
	cfg := &config.Config{
		Inventory: config.InventoryConfig{
			Host: map[string]config.InventoryHostConfig{
				"edge01": {AuthDisabled: true},
			},
		},
	}

	changed := applyInteractiveHostAuthSelection(cfg, hostPatch{Host: "edge01"})

	if !changed {
		t.Fatal("expected stale host auth to be cleared")
	}
	if _, ok := cfg.Inventory.Host["edge01"]; ok {
		t.Fatalf("host auth still present: %+v", cfg.Inventory.Host["edge01"])
	}
}

func TestApplyInteractiveHostAuthSelectionStoresHostCredential(t *testing.T) {
	cfg := &config.Config{}
	patch := hostPatch{
		Host: "edge01",
		Auth: config.InventoryAuthConfig{CredentialProvider: "pass-local", PasswordRef: "nssh/hosts/edge01"},
	}

	changed := applyInteractiveHostAuthSelection(cfg, patch)

	if !changed {
		t.Fatal("expected host auth change")
	}
	if got := cfg.Inventory.Host["edge01"].Auth; got.CredentialProvider != "pass-local" || got.PasswordRef != "nssh/hosts/edge01" {
		t.Fatalf("host auth = %+v", got)
	}
}

func TestApplyInteractiveHostAuthSelectionStoresDisabledAuth(t *testing.T) {
	cfg := &config.Config{}
	patch := hostPatch{Host: "edge01", AuthDisabled: true}

	changed := applyInteractiveHostAuthSelection(cfg, patch)

	if !changed {
		t.Fatal("expected host auth change")
	}
	if !cfg.Inventory.Host["edge01"].AuthDisabled {
		t.Fatalf("host auth = %+v", cfg.Inventory.Host["edge01"])
	}
}

func TestLocalHostProbeUserKeepsGroupDefaultAheadOfCredentialUsername(t *testing.T) {
	cfg := &config.Config{
		Inventory: config.InventoryConfig{Group: map[string]config.GroupConfig{
			"custcbb": {Auth: config.InventoryAuthConfig{Username: "chris.jones"}},
		}},
	}
	record := &credential.Record{Username: "chris.jones@custcbb.local"}

	got := localHostProbeUser(cfg, hostPatch{Host: "edge01", Group: "local/custcbb"}, record)

	if got != "chris.jones" {
		t.Fatalf("probe user = %q, want group default", got)
	}
}

func TestLocalHostProbeUserUsesCredentialUsernameWhenInventoryUserMissing(t *testing.T) {
	cfg := &config.Config{}
	record := &credential.Record{Username: "admin"}

	got := localHostProbeUser(cfg, hostPatch{Host: "edge01"}, record)

	if got != "admin" {
		t.Fatalf("probe user = %q, want credential username", got)
	}
}

func TestPromptHostCredentialAuthFallsBackToManualRef(t *testing.T) {
	cfg := &config.Config{Credential: config.CredentialConfig{Provider: map[string]config.CredentialProviderConfig{
		"pass-local": {Type: config.CredentialProviderPass},
	}}}
	prompter := &fakeLocalHostAddPrompter{
		inputs: map[string]string{
			"Password ref": "nssh/hosts/edge01",
		},
		selects: map[string]string{
			"Credential provider": "pass-local",
		},
	}
	old := listCredentialItems
	listCredentialItems = func(*config.Config, string) ([]credentialItem, error) { return nil, nil }
	defer func() { listCredentialItems = old }()

	auth, err := promptHostCredentialAuth(cfg, "edge01", prompter)
	if err != nil {
		t.Fatalf("promptHostCredentialAuth: %v", err)
	}
	if auth.CredentialProvider != "pass-local" || auth.PasswordRef != "nssh/hosts/edge01" || auth.Username != "" || auth.UsernameRef != "" {
		t.Fatalf("auth = %+v", auth)
	}
}

func TestResolveLocalHostCredentialUsesHostAuthMapping(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Credential.Provider = map[string]config.CredentialProviderConfig{"pass-local": {Type: config.CredentialProviderPass}}
	cfg.Inventory.Host = map[string]config.InventoryHostConfig{
		"edge01": {Auth: config.InventoryAuthConfig{CredentialProvider: "pass-local", PasswordRef: "nssh/hosts/edge01", Username: "admin"}},
	}
	old := newCredentialRegistry
	newCredentialRegistry = func(*config.Config) (credentialRegistry, error) {
		return fakeCredentialRegistry{providers: map[string]credential.Provider{"pass-local": fakeCredentialProvider{record: &credential.Record{Username: "admin", Secret: secretFromTest("pw")}}}}, nil
	}
	defer func() { newCredentialRegistry = old }()

	secret, err := resolveLocalHostCredentialSecret(cfg, hostPatch{Host: "edge01", Group: "local/lab", User: "admin"})
	if err != nil {
		t.Fatalf("resolveLocalHostCredentialSecret: %v", err)
	}
	if secret == nil {
		t.Fatal("expected credential secret")
	}
	defer secret.Destroy()
}

func TestUpsertLocalHostWritesSelectedAuthModeAndCompatFixes(t *testing.T) {
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
	paths := &config.Paths{ConfigDir: filepath.Join(tmp, "nssh"), ConfigFile: filepath.Join(tmp, "nssh", "config.yaml"), SSHConfigDir: sshDir, BackupDir: filepath.Join(tmp, "backups")}

	err := upsertLocalHost(parser, cfg, paths, hostPatch{
		Host:        "edge01",
		Group:       "local/lab",
		HostName:    "edge01.lab.local",
		AuthMode:    config.AuthModePassword,
		CompatFixes: []compat.CompatType{compat.CompatKex},
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	content, err := os.ReadFile(localProviderYAMLPath(cfg, paths))
	if err != nil {
		t.Fatalf("read local file: %v", err)
	}
	got := string(content)
	for _, want := range []string{
		"mode: password",
		"compat:",
		"- legacy-kex",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestLocalWrittenHostConfigReturnsPersistedStanza(t *testing.T) {
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
	if err := upsertLocalHost(parser, cfg, paths, hostPatch{Host: "edge01", Group: "local/lab", HostName: "edge01.lab.local"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	stanza, err := localWrittenHostConfig(parser, cfg, paths, "edge01")
	if err != nil {
		t.Fatalf("localWrittenHostConfig: %v", err)
	}
	for _, want := range []string{"edge01:", "hostname: edge01.lab.local"} {
		if !strings.Contains(stanza, want) {
			t.Fatalf("missing %q in:\n%s", want, stanza)
		}
	}
}

func TestLocalHostCompatibilityAppliesDetectedFixesBeforeRetry(t *testing.T) {
	old := localHostConnectionTest
	defer func() { localHostConnectionTest = old }()
	var calls int
	localHostConnectionTest = func(ctx context.Context, host *sshconfig.HostEntry, cfg connector.TestConfig) (*connector.TestResult, error) {
		calls++
		if calls == 1 {
			return &connector.TestResult{
				ExitCode: 255,
				Stderr:   "Unable to negotiate with 192.0.2.1 port 22: no matching key exchange method found.",
			}, nil
		}
		content, err := os.ReadFile(cfg.ConfigFile)
		if err != nil {
			t.Fatalf("read temp config: %v", err)
		}
		if !strings.Contains(string(content), "KexAlgorithms") {
			t.Fatalf("retry temp config missing compat fix:\n%s", content)
		}
		return &connector.TestResult{
			ExitCode: 255,
			Stderr: `debug1: kex: algorithm: diffie-hellman-group14-sha1
Permission denied (publickey,password).`,
		}, nil
	}

	host := sshconfig.CreateHostEntry("edge01", "edge01.lab.local", "admin", 22, false, "provider_local.conf")
	result, err := testLocalHostCompatibility(context.Background(), config.DefaultConfig(), host, 5, nil)
	if err != nil {
		t.Fatalf("testLocalHostCompatibility: %v", err)
	}
	if !result.Success {
		t.Fatalf("result success = false, reason %s", result.StoppedReason)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
	if len(result.FixesApplied) != 1 || result.FixesApplied[0] != compat.CompatKex {
		t.Fatalf("fixes = %v, want [kex]", result.FixesApplied)
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

func TestResolveLocalHostGroupInfersFromMatchDomainSuffix(t *testing.T) {
	cfg := &config.Config{Inventory: config.InventoryConfig{
		Provider: map[string]config.InventoryProviderConfig{
			config.ProviderLocal: {
				Type: config.ProviderLocal,
				Group: map[string]config.GroupConfig{
					"cbb":     {Match: config.InventoryMatch{"domain_suffix": []string{".expedient.com"}}},
					"custcbb": {Match: config.InventoryMatch{"domain_suffix": []string{".custcbb.local"}}},
				},
			},
		},
	}}

	group, err := resolveLocalHostGroup(cfg, hostPatch{
		Host:     "nre-netlab01",
		HostName: "nre-netlab01.custcbb.local",
	}, nil, nil)
	if err != nil {
		t.Fatalf("resolveLocalHostGroup: %v", err)
	}
	if group != "local/custcbb" {
		t.Fatalf("group = %q, want local/custcbb", group)
	}
}

func TestResolveLocalHostGroupPromptsWhenDomainDoesNotMatch(t *testing.T) {
	cfg := &config.Config{Inventory: config.InventoryConfig{
		Group: map[string]config.GroupConfig{
			"cbb": {},
			"lab": {},
		},
	}}
	var options []string

	group, err := resolveLocalHostGroup(cfg, hostPatch{Host: "lab-router"}, nil, func(g []string) (string, error) {
		options = g
		return "local/lab", nil
	})
	if err != nil {
		t.Fatalf("resolveLocalHostGroup: %v", err)
	}
	if group != "local/lab" {
		t.Fatalf("group = %q, want local/lab", group)
	}
	if strings.Join(options, ",") != "local/cbb,local/lab" {
		t.Fatalf("prompt options = %v, want sorted local/cbb, local/lab", options)
	}
}

func TestResolveLocalHostGroupPromptsWithoutExistingGroups(t *testing.T) {
	cfg := &config.Config{}
	var prompted bool

	group, err := resolveLocalHostGroup(cfg, hostPatch{Host: "lab-router"}, nil, func(g []string) (string, error) {
		prompted = true
		if len(g) != 0 {
			t.Fatalf("prompt groups = %v, want empty", g)
		}
		return "local/lab", nil
	})
	if err != nil {
		t.Fatalf("resolveLocalHostGroup: %v", err)
	}
	if !prompted {
		t.Fatal("expected prompt")
	}
	if group != "local/lab" {
		t.Fatalf("group = %q, want local/lab", group)
	}
}

func TestNormalizePromptedLocalGroupAcceptsBareLocalName(t *testing.T) {
	group, err := normalizePromptedLocalGroup(" lab ")
	if err != nil {
		t.Fatalf("normalizePromptedLocalGroup: %v", err)
	}
	if group != "local/lab" {
		t.Fatalf("group = %q, want local/lab", group)
	}
}

func TestNormalizePromptedLocalGroupRejectsInvalidName(t *testing.T) {
	_, err := normalizePromptedLocalGroup("local/bad group")
	if err == nil {
		t.Fatal("expected invalid group error")
	}
}

func TestInventoryGroupPromptOptionsIncludesCreate(t *testing.T) {
	options := inventoryGroupPromptOptions([]ui.SelectOption{{Label: "local/lab", Value: "local/lab"}})
	if len(options) != 2 {
		t.Fatalf("options = %d, want 2", len(options))
	}
	if options[1].Value != promptCreateGroupValue {
		t.Fatalf("options = %+v, want create option", options)
	}
}

func TestDefaultHostNameForGroupDoesNotAppendDomainSuffix(t *testing.T) {
	cfg := &config.Config{Inventory: config.InventoryConfig{
		Group: map[string]config.GroupConfig{
			"custcbb": {DomainSuffix: []string{".custcbb.local"}},
		},
	}}

	got := defaultHostNameForGroup(cfg, "nre-netlab01", "local/custcbb")
	if got != "nre-netlab01" {
		t.Fatalf("hostname = %q, want nre-netlab01", got)
	}
}

func TestUpsertLocalHostPreservesExistingGroupWhenGroupOmitted(t *testing.T) {
	tmp := t.TempDir()
	mainConfig := filepath.Join(tmp, ".ssh", "config")
	parser := sshconfig.NewParserWithPaths(mainConfig, filepath.Join(tmp, "backups"), 5)
	cfg := &config.Config{Inventory: config.InventoryConfig{
		Providers: map[string]config.InventoryProviderConfig{
			config.ProviderLocal: {
				Type:   config.ProviderLocal,
				Groups: map[string]config.GroupConfig{"lab": {}},
				Hosts:  map[string]config.InventoryHostConfig{"edge01": {Group: "lab", Hostname: "old.lab.local"}},
			},
		},
	}}
	paths := &config.Paths{ConfigDir: filepath.Join(tmp, "nssh"), ConfigFile: filepath.Join(tmp, "nssh", "config.yaml"), BackupDir: filepath.Join(tmp, "backups")}

	if err := upsertLocalHost(parser, cfg, paths, hostPatch{Host: "edge01", HostName: "new.lab.local"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	content, err := os.ReadFile(localProviderYAMLPath(cfg, paths))
	if err != nil {
		t.Fatal(err)
	}
	got := string(content)
	for _, want := range []string{"group: lab", "hostname: new.lab.local"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestUpsertLocalHostRefusesProviderOwnedHost(t *testing.T) {
	tmp := t.TempDir()
	mainConfig := filepath.Join(tmp, ".ssh", "config")
	parser := sshconfig.NewParserWithPaths(mainConfig, filepath.Join(tmp, "backups"), 5)
	cfg := &config.Config{Inventory: config.InventoryConfig{
		Group: map[string]config.GroupConfig{
			"lab": {},
		},
		Providers: map[string]config.InventoryProviderConfig{
			"netbox-prod": {
				Type:  config.ProviderNetBox,
				Hosts: map[string]config.InventoryHostConfig{"edge01": {Group: "cbb", Hostname: "edge01.example.com"}},
			},
		},
	}}
	paths := &config.Paths{ConfigDir: filepath.Join(tmp, "nssh"), ConfigFile: filepath.Join(tmp, "nssh", "config.yaml"), BackupDir: filepath.Join(tmp, "backups")}

	err := upsertLocalHost(parser, cfg, paths, hostPatch{Host: "edge01", Group: "local/lab", User: "admin"})
	if err == nil {
		t.Fatal("expected provider-owned mutation refusal")
	}
	if !strings.Contains(err.Error(), "provider-owned") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRemoveLocalHostRemovesOnlyLocalHosts(t *testing.T) {
	tmp := t.TempDir()
	mainConfig := filepath.Join(tmp, ".ssh", "config")
	parser := sshconfig.NewParserWithPaths(mainConfig, filepath.Join(tmp, "backups"), 5)
	cfg := &config.Config{Inventory: config.InventoryConfig{
		Providers: map[string]config.InventoryProviderConfig{
			config.ProviderLocal: {
				Type:   config.ProviderLocal,
				Groups: map[string]config.GroupConfig{"lab": {}},
				Hosts: map[string]config.InventoryHostConfig{
					"edge01": {Group: "lab", Hostname: "edge01.lab.local"},
					"edge02": {Group: "lab", Hostname: "edge02.lab.local"},
				},
			},
		},
	}}
	paths := &config.Paths{ConfigDir: filepath.Join(tmp, "nssh"), ConfigFile: filepath.Join(tmp, "nssh", "config.yaml"), BackupDir: filepath.Join(tmp, "backups")}

	removed, err := removeLocalHost(parser, cfg, paths, "edge01")
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if !removed {
		t.Fatal("expected host removal")
	}
	content, err := os.ReadFile(localProviderYAMLPath(cfg, paths))
	if err != nil {
		t.Fatal(err)
	}
	got := string(content)
	if strings.Contains(got, "edge01:") {
		t.Fatalf("removed host still present:\n%s", got)
	}
	if !strings.Contains(got, "edge02:") {
		t.Fatalf("other host missing:\n%s", got)
	}
}

func TestRemoveInventoryHostConfigClearsAuthOverride(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	if err := os.WriteFile(configPath, []byte(strings.TrimSpace(`
inventory:
  hosts:
    edge01:
      auth:
        credential_provider: pass-local
        password_ref: nssh/hosts/edge01
    edge02:
      auth:
        username: netops
`)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := removeInventoryHostConfig(configPath, cfg, "edge01"); err != nil {
		t.Fatalf("removeInventoryHostConfig: %v", err)
	}
	if _, ok := cfg.Inventory.Host["edge01"]; ok {
		t.Fatalf("host auth still present in loaded config: %+v", cfg.Inventory.Host)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if strings.Contains(got, "edge01") || strings.Contains(got, "nssh/hosts/edge01") {
		t.Fatalf("removed host auth still present:\n%s", got)
	}
	if !strings.Contains(got, "edge02:") {
		t.Fatalf("unrelated host auth missing:\n%s", got)
	}
}

func TestRemovedConfigTextIndentsBlocks(t *testing.T) {
	got := removedConfigText("Host edge01\n  HostName edge01.lab.local\n", "[inventory.host.edge01]\nauth_disabled = true\n")
	for _, want := range []string{
		"    Host edge01",
		"      HostName edge01.lab.local",
		"    [inventory.host.edge01]",
		"    auth_disabled = true",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("removed config text missing %q:\n%s", want, got)
		}
	}
}

func TestEnsureInventoryGroupEmptyRefusesNonEmptyGroup(t *testing.T) {
	tmp := t.TempDir()
	sshDir := filepath.Join(tmp, ".ssh")
	localFile := filepath.Join(sshDir, "nssh.d", "provider_local.conf")
	host := sshconfig.CreateHostEntry("edge01", "edge01.lab.local", "", 22, false, localFile)
	inventory.SetLocalHostGroup(host, "local/lab")
	cfg := &config.Config{Inventory: config.InventoryConfig{
		Provider: map[string]config.InventoryProviderConfig{
			config.ProviderLocal: {
				Type:  config.ProviderLocal,
				Group: map[string]config.GroupConfig{"lab": {}},
			},
		},
	}}
	paths := &config.Paths{SSHConfigDir: sshDir}

	err := ensureInventoryGroupEmpty("local/lab", []*sshconfig.HostEntry{host}, cfg, paths)
	if err == nil {
		t.Fatal("expected non-empty group refusal")
	}
	if !strings.Contains(err.Error(), `group "local/lab" still contains host "edge01"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRemoveInventoryGroupConfigDeletesProviderSourceGroup(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.yaml")
	providerPath := filepath.Join(tmp, "inventory", "local.yaml")
	if err := os.MkdirAll(filepath.Dir(providerPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(providerPath, []byte(strings.TrimSpace(`
inventory:
  providers:
    local:
      type: local
      groups:
        lab:
          match:
            domain_suffix: [.lab.local]
        keep:
          match:
            domain_suffix: [.keep.local]
`)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(strings.TrimSpace(`
include: [inventory/local.yaml]
`)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := removeInventoryGroupConfig(configPath, cfg, "local/lab"); err != nil {
		t.Fatalf("removeInventoryGroupConfig: %v", err)
	}
	providerData, err := os.ReadFile(providerPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(providerData)
	if strings.Contains(got, "lab:") || strings.Contains(got, ".lab.local") {
		t.Fatalf("removed group still present:\n%s", got)
	}
	if !strings.Contains(got, "keep:") {
		t.Fatalf("unrelated group missing:\n%s", got)
	}
	rootData, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rootData), "keep:") {
		t.Fatalf("provider config was flattened into root:\n%s", rootData)
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
	if err := os.WriteFile(filepath.Join(sshDir, "conf.d", "external_hosts"), []byte("Host unmanaged\n  HostName unmanaged.example.com\n"), 0600); err != nil {
		t.Fatal(err)
	}

	parser := sshconfig.NewParserWithPaths(mainConfig, filepath.Join(tmp, "backups"), 5)
	cfg := &config.Config{Inventory: config.InventoryConfig{
		Providers: map[string]config.InventoryProviderConfig{
			config.ProviderLocal: {
				Type:   config.ProviderLocal,
				Groups: map[string]config.GroupConfig{"lab": {}},
				Hosts:  map[string]config.InventoryHostConfig{"managed": {Group: "lab", Hostname: "managed.example.com"}},
			},
		},
	}}
	paths := &config.Paths{ConfigDir: filepath.Join(tmp, "nssh"), ConfigFile: filepath.Join(tmp, "nssh", "config.yaml"), SSHConfigDir: sshDir, BackupDir: filepath.Join(tmp, "backups")}

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
		hosts[0]: {Owner: "local", Group: "local/customer"},
		hosts[1]: {Owner: "netbox-prod", Group: "local/lab"},
		hosts[2]: {Owner: "local", Group: "local/corp"},
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
		{name: "field group", pattern: "group:local/customer", want: "151-core1"},
		{name: "multiple terms", pattern: "group:local/corp user:ops", want: "router.example.com"},
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
	filtered, err := filterInventoryHosts(hosts, "group:local/corp", func(host *sshconfig.HostEntry) hostMetadata {
		return meta[host]
	})
	if err != nil {
		t.Fatalf("filterInventoryHosts: %v", err)
	}
	if len(filtered) != 1 || filtered[0].Host != "router.example.com" {
		t.Fatalf("group:local/corp filtered = %+v", filtered)
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
	mainConfig := filepath.Join(tmp, ".ssh", "config")
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
	paths := &config.Paths{ConfigDir: filepath.Join(tmp, "nssh"), ConfigFile: filepath.Join(tmp, "nssh", "config.yaml"), BackupDir: filepath.Join(tmp, "backups")}

	result, err := importLocalCSV(parser, cfg, paths, csvPath, "local/lab")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.Added != 2 || result.Skipped != 0 || result.Failed != 0 {
		t.Fatalf("result = %+v", result)
	}
	content, err := os.ReadFile(localProviderYAMLPath(cfg, paths))
	if err != nil {
		t.Fatal(err)
	}
	got := string(content)
	if strings.Index(got, "edge01:") > strings.Index(got, "edge02:") {
		t.Fatalf("hosts not sorted:\n%s", got)
	}
	for _, want := range []string{"group: lab", "edge01:", "hostname: edge01.lab.local", "edge02:", "port: 2222"} {
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
	created := ensureGroup(cfg, "local/lab")
	if !created {
		t.Fatal("expected group creation")
	}
	if len(cfg.Inventory.Provider["local"].Group["lab"].DomainSuffix) != 0 {
		t.Fatalf("group config = %+v, want metadata-only group", cfg.Inventory.Provider["local"].Group["lab"])
	}
	if ensureGroup(cfg, "local/lab") {
		t.Fatal("expected existing group to be preserved")
	}
}

func TestEnsureLocalGroupCreatesMissingGroup(t *testing.T) {
	cfg := &config.Config{}
	created, err := ensureLocalGroup(cfg, "local/lab", hostPatch{Host: "edge01", HostName: "edge01.lab.local"})
	if err != nil {
		t.Fatalf("ensureLocalGroup: %v", err)
	}
	if !created {
		t.Fatal("expected group creation")
	}
	if _, ok := cfg.Inventory.Provider[config.ProviderLocal].Group["lab"]; !ok {
		t.Fatalf("local/lab missing from config: %+v", cfg.Inventory.Provider)
	}
	if got := cfg.Inventory.Provider[config.ProviderLocal].Group["lab"].Match["domain_suffix"]; strings.Join(got, ",") != ".lab.local" {
		t.Fatalf("local/lab match domain_suffix = %v, want .lab.local", got)
	}
}

type fakeLocalHostAddPrompter struct {
	inputs      map[string]string
	selects     map[string]string
	inputQueue  map[string][]string
	selectQueue map[string][]string
	secrets     map[string]string
	options     map[string][]ui.SelectOption
	prompts     []string
}

func (p *fakeLocalHostAddPrompter) InputWithDefault(title, defaultValue string) (string, error) {
	p.prompts = append(p.prompts, title)
	if values := p.inputQueue[title]; len(values) > 0 {
		value := values[0]
		p.inputQueue[title] = values[1:]
		return value, nil
	}
	if value, ok := p.inputs[title]; ok {
		return value, nil
	}
	return defaultValue, nil
}

func (p *fakeLocalHostAddPrompter) Select(title string, options []ui.SelectOption) (string, error) {
	p.prompts = append(p.prompts, title)
	if p.options == nil {
		p.options = make(map[string][]ui.SelectOption)
	}
	p.options[title] = append([]ui.SelectOption(nil), options...)
	if values := p.selectQueue[title]; len(values) > 0 {
		value := values[0]
		p.selectQueue[title] = values[1:]
		return value, nil
	}
	if value, ok := p.selects[title]; ok {
		return value, nil
	}
	if len(options) == 0 {
		return "", nil
	}
	return options[0].Value, nil
}

type fakeCredentialRegistry struct {
	providers map[string]credential.Provider
}

func (r fakeCredentialRegistry) Provider(name string) credential.Provider {
	return r.providers[name]
}

type fakeCredentialProvider struct {
	record *credential.Record
}

func (p fakeCredentialProvider) GetHost(string) (*credential.Record, error) {
	return p.record, nil
}

func (p fakeCredentialProvider) GetGroup(string) (*credential.Record, error) {
	return p.record, nil
}

func (p fakeCredentialProvider) GetRef(config.CredentialRefConfig) (*credential.Record, error) {
	return p.record, nil
}

func secretFromTest(value string) *secret.Secret {
	return secret.NewFromString(value)
}
