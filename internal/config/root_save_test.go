package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveInventoryHostAuthPreservesIncludesAndAvoidsFlattening(t *testing.T) {
	tmp := t.TempDir()
	mainPath := filepath.Join(tmp, "config.toml")
	writeConfigFile(t, filepath.Join(tmp, "inventory", "groups.toml"), `
[provider.local]
type = "local"

[provider.local.group.imported]
auth = { username = "shared" }
`)
	writeConfigFile(t, mainPath, `
[credential]

[credential.provider.pass-local]
type = "pass"

[inventory]
include = ["inventory/groups.toml"]
`)

	cfg, err := Load(mainPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cfg.Inventory.Host = map[string]InventoryHostConfig{
		"edge01": {
			Auth: InventoryAuthConfig{
				CredentialProvider: "pass-local",
				PasswordRef:        "nssh/hosts/edge01",
			},
		},
	}

	if err := SaveInventoryHostAuth(mainPath, cfg, "edge01"); err != nil {
		t.Fatalf("SaveInventoryHostAuth: %v", err)
	}
	data, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{`include = ["inventory/groups.toml"]`, "[inventory.host.edge01]", "nssh/hosts/edge01"} {
		if !strings.Contains(got, want) {
			t.Fatalf("root config missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "[inventory.provider.local.group.imported]") || strings.Contains(got, `username = "shared"`) {
		t.Fatalf("imported group was flattened into root config:\n%s", got)
	}
}

func TestSaveInventoryHostAuthWritesDisabledAuth(t *testing.T) {
	tmp := t.TempDir()
	mainPath := filepath.Join(tmp, "config.toml")
	writeConfigFile(t, mainPath, `
[credential]

[credential.provider.pass-local]
type = "pass"
`)

	cfg, err := Load(mainPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cfg.Inventory.Host = map[string]InventoryHostConfig{
		"edge01": {AuthDisabled: true},
	}

	if err := SaveInventoryHostAuth(mainPath, cfg, "edge01"); err != nil {
		t.Fatalf("SaveInventoryHostAuth: %v", err)
	}
	data, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{"[inventory.host.edge01]", "auth_disabled = true"} {
		if !strings.Contains(got, want) {
			t.Fatalf("root config missing %q:\n%s", want, got)
		}
	}
}

func TestDeleteInventoryGroupRefusesImportedOnlyGroup(t *testing.T) {
	tmp := t.TempDir()
	mainPath := filepath.Join(tmp, "config.toml")
	writeConfigFile(t, filepath.Join(tmp, "groups.toml"), `
[provider.local]
type = "local"

[provider.local.group.imported]
auth = { username = "shared" }
`)
	writeConfigFile(t, mainPath, `
[inventory]
include = ["groups.toml"]

[inventory.provider.local]
type = "local"

[inventory.provider.local.group.default]
`)

	cfg, err := Load(mainPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	err = DeleteInventoryGroup(mainPath, cfg, "local/imported")
	if err == nil {
		t.Fatal("expected imported group removal refusal")
	}
	if !strings.Contains(err.Error(), "imported") || !strings.Contains(err.Error(), "groups.toml") {
		t.Fatalf("unexpected error: %v", err)
	}
}
