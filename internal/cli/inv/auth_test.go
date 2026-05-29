package inv

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
)

func TestApplyHostAuthPatchWritesInventoryHostAuth(t *testing.T) {
	tmp := t.TempDir()
	sshDir := filepath.Join(tmp, ".ssh")
	if err := os.MkdirAll(filepath.Join(sshDir, "nssh.d"), 0700); err != nil {
		t.Fatal(err)
	}
	mainConfig := filepath.Join(sshDir, "config")
	if err := os.WriteFile(mainConfig, []byte("Include nssh.d/*\n"), 0600); err != nil {
		t.Fatal(err)
	}
	localFile := filepath.Join(sshDir, "nssh.d", "provider_local.conf")
	if err := os.WriteFile(localFile, []byte("Host edge01\n  HostName edge01.lab.local\n"), 0600); err != nil {
		t.Fatal(err)
	}

	parser := sshconfig.NewParserWithPaths(mainConfig, filepath.Join(tmp, "backups"), 5)
	cfg := config.DefaultConfig()
	paths := &config.Paths{SSHConfigDir: sshDir, BackupDir: filepath.Join(tmp, "backups")}

	err := applyHostAuthPatch(parser, cfg, paths, "edge01", hostAuthPatch{
		Auth: config.InventoryAuthConfig{
			Provider: "pass-local",
			Ref:      "nssh/hosts/edge01",
			Username: "admin",
		},
	})
	if err != nil {
		t.Fatalf("apply auth: %v", err)
	}
	got := cfg.Inventory.Host["edge01"].Auth
	if got.Provider != "pass-local" || got.Ref != "nssh/hosts/edge01" || got.Username != "admin" {
		t.Fatalf("auth = %+v", got)
	}
}

func TestApplyHostAuthPatchClearsOnlyHostAuth(t *testing.T) {
	tmp := t.TempDir()
	sshDir := filepath.Join(tmp, ".ssh")
	if err := os.MkdirAll(filepath.Join(sshDir, "nssh.d"), 0700); err != nil {
		t.Fatal(err)
	}
	mainConfig := filepath.Join(sshDir, "config")
	if err := os.WriteFile(mainConfig, []byte("Include nssh.d/*\n"), 0600); err != nil {
		t.Fatal(err)
	}
	localFile := filepath.Join(sshDir, "nssh.d", "provider_local.conf")
	if err := os.WriteFile(localFile, []byte("Host edge01\n  HostName edge01.lab.local\n"), 0600); err != nil {
		t.Fatal(err)
	}

	parser := sshconfig.NewParserWithPaths(mainConfig, filepath.Join(tmp, "backups"), 5)
	cfg := config.DefaultConfig()
	cfg.Inventory.Host = map[string]config.InventoryHostConfig{
		"edge01": {Auth: config.InventoryAuthConfig{Provider: "pass-local", Ref: "nssh/hosts/edge01"}},
	}
	paths := &config.Paths{SSHConfigDir: sshDir, BackupDir: filepath.Join(tmp, "backups")}

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
	sshDir := filepath.Join(tmp, ".ssh")
	if err := os.MkdirAll(filepath.Join(sshDir, "nssh.d"), 0700); err != nil {
		t.Fatal(err)
	}
	mainConfig := filepath.Join(sshDir, "config")
	if err := os.WriteFile(mainConfig, []byte("Include nssh.d/*\n"), 0600); err != nil {
		t.Fatal(err)
	}
	providerFile := filepath.Join(sshDir, "nssh.d", "provider_netbox-prod.conf")
	if err := os.WriteFile(providerFile, []byte("Host edge01\n  HostName edge01.example.com\n"), 0600); err != nil {
		t.Fatal(err)
	}

	parser := sshconfig.NewParserWithPaths(mainConfig, filepath.Join(tmp, "backups"), 5)
	cfg := config.DefaultConfig()
	cfg.Inventory.Provider = map[string]config.InventoryProviderConfig{
		"netbox-prod": {Type: config.ProviderNetBox, Route: []config.InventoryRouteConfig{{Group: "default"}}},
	}
	paths := &config.Paths{SSHConfigDir: sshDir, BackupDir: filepath.Join(tmp, "backups")}

	err := applyHostAuthPatch(parser, cfg, paths, "edge01", hostAuthPatch{
		Auth: config.InventoryAuthConfig{Provider: "pass-local", Ref: "nssh/hosts/edge01"},
	})
	if err != nil {
		t.Fatalf("apply auth: %v", err)
	}
	if got := cfg.Inventory.Host["edge01"].Auth.Ref; got != "nssh/hosts/edge01" {
		t.Fatalf("auth ref = %q", got)
	}
}

func TestHostAuthPatchRejectsConflicts(t *testing.T) {
	cfg := config.DefaultConfig()
	err := (hostAuthPatch{
		Clear: true,
		Auth:  config.InventoryAuthConfig{Provider: "pass-local", Ref: "nssh/hosts/edge01"},
	}).Validate(cfg)
	if err == nil {
		t.Fatal("expected clear conflict")
	}

	err = (hostAuthPatch{
		Auth: config.InventoryAuthConfig{Ref: "nssh/hosts/edge01", Username: "admin", UsernameRef: "op://vault/item/username"},
	}).Validate(cfg)
	if err == nil {
		t.Fatal("expected username conflict")
	}
}

func TestEffectiveInventoryAuthHostOverridesGroup(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Inventory.Group["default"] = config.GroupConfig{
		Auth: config.InventoryAuthConfig{Provider: "pass-local", Ref: "nssh/groups/default"},
	}
	cfg.Inventory.Host = map[string]config.InventoryHostConfig{
		"edge01": {Auth: config.InventoryAuthConfig{Provider: "op-network", Ref: "nssh host edge01", Username: "netops"}},
	}

	view := effectiveInventoryAuth(cfg, "edge01", "default")
	if view.Source != "host override" || view.Provider != "op-network" || view.Ref != "nssh host edge01" || view.Username != "netops" {
		t.Fatalf("view = %+v", view)
	}
}

func TestEffectiveInventoryAuthFallsBackToGroupAndDefaultProvider(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Inventory.Group["lab"] = config.GroupConfig{
		Auth: config.InventoryAuthConfig{Ref: "nssh/groups/lab"},
	}

	view := effectiveInventoryAuth(cfg, "edge01", "lab")
	if view.Source != "group lab" || view.Provider != "pass-local" || view.Ref != "nssh/groups/lab" {
		t.Fatalf("view = %+v", view)
	}
}

func TestEffectiveInventoryAuthMissing(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Inventory.Host = nil
	cfg.Inventory.Group = map[string]config.GroupConfig{"lab": {}}

	view := effectiveInventoryAuth(cfg, "edge01", "lab")
	if view.Source != "-" || view.Provider != "-" || view.Ref != "-" {
		t.Fatalf("view = %+v", view)
	}
}

func TestInventoryAuthDisplayRowsNamePasswordRefExplicitly(t *testing.T) {
	rows := inventoryAuthDisplayRows(inventoryAuthView{
		Source:      "group custcbb",
		Provider:    "op-expedient",
		Ref:         "op://Expedient/item/password",
		Username:    "-",
		UsernameRef: "-",
	})

	wantLabels := []string{
		"Auth Source",
		"Credential Provider",
		"Credential Password Ref",
		"Credential Username Override",
		"Credential Username Ref",
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
