package inv

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
)

func TestApplyHostAuthPatchWritesInventoryHostAuth(t *testing.T) {
	tmp := t.TempDir()
	mainConfig := filepath.Join(tmp, ".ssh", "config")
	parser := sshconfig.NewParserWithPaths(mainConfig, filepath.Join(tmp, "backups"), 5)
	cfg := testAuthPatchConfig("local", config.ProviderLocal, "lab", "edge01", "edge01.lab.local")
	paths := &config.Paths{ConfigDir: filepath.Join(tmp, "nssh"), ConfigFile: filepath.Join(tmp, "nssh", "config.yaml"), BackupDir: filepath.Join(tmp, "backups")}

	err := applyHostAuthPatch(parser, cfg, paths, "edge01", hostAuthPatch{
		Auth: config.InventoryAuthConfig{
			CredentialProvider: "pass-local",
			PasswordRef:        "nssh/hosts/edge01",
			Username:           "admin",
		},
	})
	if err != nil {
		t.Fatalf("apply auth: %v", err)
	}
	got := cfg.Inventory.Host["edge01"].Auth
	if got.CredentialProvider != "pass-local" || got.PasswordRef != "nssh/hosts/edge01" || got.Username != "admin" {
		t.Fatalf("auth = %+v", got)
	}
}

func TestApplyHostAuthPatchClearsOnlyHostAuth(t *testing.T) {
	tmp := t.TempDir()
	mainConfig := filepath.Join(tmp, ".ssh", "config")
	parser := sshconfig.NewParserWithPaths(mainConfig, filepath.Join(tmp, "backups"), 5)
	cfg := testAuthPatchConfig("local", config.ProviderLocal, "lab", "edge01", "edge01.lab.local")
	cfg.Inventory.Host = map[string]config.InventoryHostConfig{
		"edge01": {Auth: config.InventoryAuthConfig{CredentialProvider: "pass-local", PasswordRef: "nssh/hosts/edge01"}},
	}
	paths := &config.Paths{ConfigDir: filepath.Join(tmp, "nssh"), ConfigFile: filepath.Join(tmp, "nssh", "config.yaml"), BackupDir: filepath.Join(tmp, "backups")}

	err := applyHostAuthPatch(parser, cfg, paths, "edge01", hostAuthPatch{Clear: true})
	if err != nil {
		t.Fatalf("clear auth: %v", err)
	}
	if _, ok := cfg.Inventory.Host["edge01"]; ok {
		t.Fatalf("host auth still present: %+v", cfg.Inventory.Host["edge01"])
	}
}

func TestApplyHostAuthPatchAllowsProviderOwnedHost(t *testing.T) {
	tmp := t.TempDir()
	mainConfig := filepath.Join(tmp, ".ssh", "config")
	parser := sshconfig.NewParserWithPaths(mainConfig, filepath.Join(tmp, "backups"), 5)
	cfg := testAuthPatchConfig("netbox-prod", config.ProviderNetBox, "cbb", "edge01", "edge01.example.com")
	paths := &config.Paths{ConfigDir: filepath.Join(tmp, "nssh"), ConfigFile: filepath.Join(tmp, "nssh", "config.yaml"), BackupDir: filepath.Join(tmp, "backups")}

	err := applyHostAuthPatch(parser, cfg, paths, "edge01", hostAuthPatch{
		Auth: config.InventoryAuthConfig{CredentialProvider: "pass-local", PasswordRef: "nssh/hosts/edge01"},
	})
	if err != nil {
		t.Fatalf("apply auth: %v", err)
	}
	if got := cfg.Inventory.Host["edge01"].Auth.PasswordRef; got != "nssh/hosts/edge01" {
		t.Fatalf("password_ref = %q", got)
	}
}

func testAuthPatchConfig(providerName, providerType, group, host, hostname string) *config.Config {
	cfg := config.DefaultConfig()
	providers := map[string]config.InventoryProviderConfig{
		providerName: {
			Type:   providerType,
			Groups: map[string]config.GroupConfig{group: {}},
			Group:  map[string]config.GroupConfig{group: {}},
			Hosts:  map[string]config.InventoryHostConfig{host: {Group: group, Hostname: hostname}},
		},
	}
	cfg.Inventory.Providers = providers
	cfg.Inventory.Provider = providers
	return cfg
}

func TestHostAuthPatchRejectsConflicts(t *testing.T) {
	cfg := config.DefaultConfig()
	err := (hostAuthPatch{
		Clear: true,
		Auth:  config.InventoryAuthConfig{CredentialProvider: "pass-local", PasswordRef: "nssh/hosts/edge01"},
	}).Validate(cfg)
	if err == nil {
		t.Fatal("expected clear conflict")
	}

	err = (hostAuthPatch{
		Auth: config.InventoryAuthConfig{CredentialProvider: "pass-local", PasswordRef: "nssh/hosts/edge01", Username: "admin", UsernameRef: "op://vault/item/username"},
	}).Validate(cfg)
	if err == nil {
		t.Fatal("expected username conflict")
	}

	err = (hostAuthPatch{
		Auth: config.InventoryAuthConfig{PasswordRef: "nssh/hosts/edge01"},
	}).Validate(cfg)
	if err == nil {
		t.Fatal("expected missing provider error")
	}
	if want := "provider is required"; !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not contain %q", err, want)
	}
}

func TestSetCommandUsesPasswordRefFlag(t *testing.T) {
	cmd := newSetCmd()
	if flag := cmd.Flags().Lookup("password-ref"); flag == nil {
		t.Fatal("missing --password-ref flag")
	}
	if flag := cmd.Flags().Lookup("credential-ref"); flag != nil {
		t.Fatal("legacy --credential-ref flag should not be registered")
	}
}

func TestEffectiveInventoryAuthHostOverridesGroup(t *testing.T) {
	cfg := config.DefaultConfig()
	localGroupID := setInvTestLocalGroup(cfg, "default", config.GroupConfig{Auth: config.InventoryAuthConfig{CredentialProvider: "pass-local", PasswordRef: "nssh/groups/default"}})
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
	labGroup := setInvTestLocalGroup(cfg, "lab", config.GroupConfig{Auth: config.InventoryAuthConfig{CredentialProvider: "pass-local", PasswordRef: "nssh/groups/lab"}})

	view := effectiveInventoryAuth(cfg, "edge01", labGroup)
	if view.Source != "group local/lab" || view.CredentialProvider != "pass-local" || view.PasswordRef != "nssh/groups/lab" {
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
