package inv

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
)

func TestApplyInventoryAuthPatchWritesInventoryHostAuth(t *testing.T) {
	tmp := t.TempDir()
	cfg := testAuthPatchConfig("local", config.ProviderLocal, "lab", "edge01", "edge01.lab.local")
	paths := &config.Paths{ConfigDir: filepath.Join(tmp, "nssh"), ConfigFile: filepath.Join(tmp, "nssh", "config.yaml"), BackupDir: filepath.Join(tmp, "backups")}

	err := applyInventoryAuthPatch(cfg, paths, "edge01", inventoryAuthPatch{
		Auth: config.InventoryAuthConfig{
			CredentialProvider: "sops",
			PasswordRef:        "hosts.edge01.password",
			Username:           "admin",
		},
	})
	if err != nil {
		t.Fatalf("apply auth: %v", err)
	}
	got := cfg.Inventory.Host["edge01"].Auth
	if got.CredentialProvider != "sops" || got.PasswordRef != "hosts.edge01.password" || got.Username != "admin" {
		t.Fatalf("auth = %+v", got)
	}
}

func TestApplyInventoryAuthPatchClearsOnlyHostAuth(t *testing.T) {
	tmp := t.TempDir()
	cfg := testAuthPatchConfig("local", config.ProviderLocal, "lab", "edge01", "edge01.lab.local")
	cfg.Inventory.Host = map[string]config.InventoryHostConfig{
		"edge01": {Auth: config.InventoryAuthConfig{CredentialProvider: "sops", PasswordRef: "hosts.edge01.password"}},
	}
	paths := &config.Paths{ConfigDir: filepath.Join(tmp, "nssh"), ConfigFile: filepath.Join(tmp, "nssh", "config.yaml"), BackupDir: filepath.Join(tmp, "backups")}

	err := applyInventoryAuthPatch(cfg, paths, "edge01", inventoryAuthPatch{Clear: true})
	if err != nil {
		t.Fatalf("clear auth: %v", err)
	}
	if _, ok := cfg.Inventory.Host["edge01"]; ok {
		t.Fatalf("host auth still present: %+v", cfg.Inventory.Host["edge01"])
	}
}

func TestApplyInventoryAuthPatchAllowsProviderOwnedHost(t *testing.T) {
	tmp := t.TempDir()
	cfg := testAuthPatchConfig("netbox-prod", config.ProviderNetBox, "cbb", "edge01", "edge01.example.com")
	paths := &config.Paths{ConfigDir: filepath.Join(tmp, "nssh"), ConfigFile: filepath.Join(tmp, "nssh", "config.yaml"), BackupDir: filepath.Join(tmp, "backups")}

	err := applyInventoryAuthPatch(cfg, paths, "edge01", inventoryAuthPatch{
		Auth: config.InventoryAuthConfig{CredentialProvider: "sops", PasswordRef: "hosts.edge01.password"},
	})
	if err != nil {
		t.Fatalf("apply auth: %v", err)
	}
	if got := cfg.Inventory.Host["edge01"].Auth.PasswordRef; got != "hosts.edge01.password" {
		t.Fatalf("password_ref = %q", got)
	}
}

func TestApplyInventoryAuthPatchPreservesExistingUsername(t *testing.T) {
	tmp := t.TempDir()
	cfg := testAuthPatchConfig("local", config.ProviderLocal, "lab", "edge01", "edge01.lab.local")
	cfg.Inventory.Host = map[string]config.InventoryHostConfig{
		"edge01": {Auth: config.InventoryAuthConfig{
			CredentialProvider: "op-expedient",
			PasswordRef:        "op://Expedient/item/password",
			Username:           "chris.jones",
		}},
	}
	paths := &config.Paths{ConfigDir: filepath.Join(tmp, "nssh"), ConfigFile: filepath.Join(tmp, "nssh", "config.yaml"), BackupDir: filepath.Join(tmp, "backups")}

	err := applyInventoryAuthPatch(cfg, paths, "edge01", inventoryAuthPatch{
		Auth: config.InventoryAuthConfig{CredentialProvider: "sops", PasswordRef: "expedient.password", Mode: config.AuthModePassword},
	})
	if err != nil {
		t.Fatalf("apply auth: %v", err)
	}
	got := cfg.Inventory.Host["edge01"].Auth
	if got.CredentialProvider != "sops" || got.PasswordRef != "expedient.password" {
		t.Fatalf("credential = %+v", got)
	}
	if got.Username != "chris.jones" {
		t.Fatalf("username = %q, want preserved chris.jones; auth=%+v", got.Username, got)
	}
}

func testAuthPatchConfig(providerName, providerType, group, host, hostname string) *config.Config {
	cfg := config.DefaultConfig()
	providers := map[string]config.InventoryProviderConfig{
		providerName: {
			Type:   providerType,
			Groups: map[string]config.GroupConfig{group: {}},
			Group:  map[string]config.GroupConfig{group: {}},
			Hosts:  map[string]config.InventoryHostConfig{hostname: {Group: group, Aliases: []string{host}}},
		},
	}
	cfg.Inventory.Providers = providers
	cfg.Inventory.Provider = providers
	return cfg
}

func TestInventoryAuthPatchRejectsConflicts(t *testing.T) {
	cfg := config.DefaultConfig()
	err := (inventoryAuthPatch{
		Clear: true,
		Auth:  config.InventoryAuthConfig{CredentialProvider: "sops", PasswordRef: "hosts.edge01.password"},
	}).Validate(cfg)
	if err == nil {
		t.Fatal("expected clear conflict")
	}

	err = (inventoryAuthPatch{
		Auth: config.InventoryAuthConfig{CredentialProvider: "sops", PasswordRef: "hosts.edge01.password", Username: "admin", UsernameRef: "op://vault/item/username"},
	}).Validate(cfg)
	if err == nil {
		t.Fatal("expected username conflict")
	}

	err = (inventoryAuthPatch{
		Auth: config.InventoryAuthConfig{PasswordRef: "hosts.edge01.password"},
	}).Validate(cfg)
	if err == nil {
		t.Fatal("expected missing provider error")
	}
	if want := "provider is required"; !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not contain %q", err, want)
	}
}

func TestSetCommandUsesCompactCredentialFlag(t *testing.T) {
	cmd := newSetCmd()
	if flag := cmd.Flags().Lookup("cred"); flag == nil {
		t.Fatal("missing --cred flag")
	}
	for _, legacy := range []string{"password-ref", "credential-provider", "credential-clear"} {
		if flag := cmd.Flags().Lookup(legacy); flag != nil {
			t.Fatalf("legacy --%s flag should not be registered", legacy)
		}
	}
}

func TestInvAuthCommandIsNotRegistered(t *testing.T) {
	cmd := NewCmd()
	if found, _, err := cmd.Find([]string{"auth"}); err == nil && found != cmd {
		t.Fatalf("inv auth should not be registered: %v", found.CommandPath())
	}
}

func TestEffectiveInventoryAuthHostOverridesGroup(t *testing.T) {
	cfg := config.DefaultConfig()
	localGroupID := setInvTestLocalGroup(cfg, "default", config.GroupConfig{Auth: config.InventoryAuthConfig{CredentialProvider: "sops", PasswordRef: "groups.default.password"}})
	cfg.Inventory.Host = map[string]config.InventoryHostConfig{
		"edge01": {Auth: config.InventoryAuthConfig{CredentialProvider: "op-network", PasswordRef: "nssh host edge01", Username: "netops"}},
	}

	view := effectiveInventoryAuth(cfg, "edge01", localGroupID)
	if view.Source != "host edge01" || view.CredentialProvider != "op-network" || view.PasswordRef != "nssh host edge01" || view.Username != "netops" {
		t.Fatalf("view = %+v", view)
	}
}

func TestEffectiveInventoryAuthFallsBackToGroupProvider(t *testing.T) {
	cfg := config.DefaultConfig()
	labGroup := setInvTestLocalGroup(cfg, "lab", config.GroupConfig{Auth: config.InventoryAuthConfig{CredentialProvider: "sops", PasswordRef: "groups.lab.password"}})

	view := effectiveInventoryAuth(cfg, "edge01", labGroup)
	if view.Source != "group local/lab" || view.CredentialProvider != "sops" || view.PasswordRef != "groups.lab.password" {
		t.Fatalf("view = %+v", view)
	}
}

func TestEffectiveInventoryAuthMissing(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Inventory.Host = nil
	labGroup := setInvTestLocalGroup(cfg, "lab", config.GroupConfig{})

	view := effectiveInventoryAuth(cfg, "edge01", labGroup)
	if view.Source != "-" || view.CredentialProvider != "-" || view.PasswordRef != "-" {
		t.Fatalf("view = %+v", view)
	}
}

func setInvTestLocalGroup(cfg *config.Config, group string, groupCfg config.GroupConfig) string {
	if cfg.Inventory.Provider == nil {
		cfg.Inventory.Provider = make(map[string]config.InventoryProviderConfig)
	}
	localProvider := cfg.Inventory.Provider[config.ProviderLocal]
	localProvider.Type = config.ProviderLocal
	if localProvider.Group == nil {
		localProvider.Group = make(map[string]config.GroupConfig)
	}
	localProvider.Group[group] = groupCfg
	cfg.Inventory.Provider[config.ProviderLocal] = localProvider
	return config.FormatInventoryGroupID(config.ProviderLocal, group)
}

func TestInventoryAuthDisplayRowsNamePasswordRefExplicitly(t *testing.T) {
	rows := inventoryAuthDisplayRows(inventoryAuthView{
		Source:             "group customer",
		CredentialProvider: "op-network",
		PasswordRef:        "op://ExampleCorp/item/password",
		Username:           "netops",
		UsernameRef:        "-",
		UsernameSource:     "group customer",
		PasswordSource:     "group customer",
		AuthMode:           "password",
		AuthModeSource:     "group customer",
	})

	wantLabels := []string{
		"Auth Source",
		"Auth Mode",
		"Auth Mode Source",
		"Credential Provider",
		"Username Source",
		"Username",
		"Username Ref",
		"Password Source",
		"Password Ref",
	}
	if len(rows) != len(wantLabels) {
		t.Fatalf("rows = %d, want %d", len(rows), len(wantLabels))
	}
	for i, want := range wantLabels {
		if rows[i].Label != want {
			t.Fatalf("row[%d].Label = %q, want %q", i, rows[i].Label, want)
		}
	}
}

func TestInventoryDisplaySectionsSeparateSSHAndNSSHConfig(t *testing.T) {
	sections := inventoryDisplaySections(
		[]inventoryDisplayRow{{Label: "Host", Value: "edge01"}},
		[]inventoryDisplayRow{{Label: "Provider", Value: "netbox-prod"}},
	)

	if len(sections) != 2 {
		t.Fatalf("sections = %d, want 2", len(sections))
	}
	if sections[0].Title != "SSH CONFIG" {
		t.Fatalf("first section = %q, want SSH CONFIG", sections[0].Title)
	}
	if sections[1].Title != "NSSH CONFIG" {
		t.Fatalf("second section = %q, want NSSH CONFIG", sections[1].Title)
	}
}

func TestInventorySSHDisplayRowsUseSSHDirectiveNames(t *testing.T) {
	rows := inventoryHostSSHDisplayRows("edge01", "edge01.example.com", "netops", "22")

	wantLabels := []string{"Host", "HostName", "User", "Port"}
	if len(rows) != len(wantLabels) {
		t.Fatalf("rows = %d, want %d", len(rows), len(wantLabels))
	}
	for i, want := range wantLabels {
		if rows[i].Label != want {
			t.Fatalf("row[%d].Label = %q, want %q", i, rows[i].Label, want)
		}
	}
}
