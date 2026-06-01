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

func TestSaveInventoryGroupAndHostAuthWritesBothChanges(t *testing.T) {
	tmp := t.TempDir()
	mainPath := filepath.Join(tmp, "config.toml")
	writeConfigFile(t, mainPath, `
[credential.provider.pass-local]
type = "pass"
`)

	cfg, err := Load(mainPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cfg.Inventory.Provider = map[string]InventoryProviderConfig{
		"local": {
			Type:  ProviderLocal,
			Group: map[string]GroupConfig{"lab": {}},
		},
	}
	cfg.Inventory.Host = map[string]InventoryHostConfig{
		"edge01": {
			Auth: InventoryAuthConfig{
				CredentialProvider: "pass-local",
				PasswordRef:        "nssh/hosts/edge01",
			},
		},
	}

	if err := SaveInventoryGroupAndHostAuth(mainPath, cfg, "local/lab", "edge01"); err != nil {
		t.Fatalf("SaveInventoryGroupAndHostAuth: %v", err)
	}
	data, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{"[inventory.provider.local]", "[inventory.provider.local.group.lab]", "[inventory.host.edge01]", "nssh/hosts/edge01"} {
		if !strings.Contains(got, want) {
			t.Fatalf("root config missing %q:\n%s", want, got)
		}
	}
}

func TestSaveInventoryGroupWritesProviderSourceFile(t *testing.T) {
	tmp := t.TempDir()
	mainPath := filepath.Join(tmp, "config.toml")
	providerPath := filepath.Join(tmp, "inventory", "local.toml")
	writeConfigFile(t, providerPath, `
[provider.local]
type = "local"

[provider.local.group.existing.match]
domain_suffix = [".existing.local"]
`)
	writeConfigFile(t, mainPath, `
[inventory]
include = ["inventory/local.toml"]
`)

	cfg, err := Load(mainPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	localProvider := cfg.Inventory.Provider[ProviderLocal]
	localProvider.Group["lab"] = GroupConfig{Match: InventoryMatch{"domain_suffix": []string{".lab.local"}}}
	cfg.Inventory.Provider[ProviderLocal] = localProvider

	if err := SaveInventoryGroup(mainPath, cfg, "local/lab"); err != nil {
		t.Fatalf("SaveInventoryGroup: %v", err)
	}
	mainData, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(mainData), "group.lab") {
		t.Fatalf("root config should not receive new provider group:\n%s", mainData)
	}
	providerData, err := os.ReadFile(providerPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(providerData)
	for _, want := range []string{"[provider.local]", "[provider.local.group.lab.match]", `domain_suffix = [".lab.local"]`} {
		if !strings.Contains(got, want) {
			t.Fatalf("provider config missing %q:\n%s", want, got)
		}
	}
}

func TestSaveInventoryGroupAndHostAuthSplitsProviderGroupAndRootHostAuth(t *testing.T) {
	tmp := t.TempDir()
	mainPath := filepath.Join(tmp, "config.toml")
	providerPath := filepath.Join(tmp, "inventory", "local.toml")
	writeConfigFile(t, providerPath, `
[provider.local]
type = "local"
`)
	writeConfigFile(t, mainPath, `
[credential.provider.pass-local]
type = "pass"

[inventory]
include = ["inventory/local.toml"]
`)

	cfg, err := Load(mainPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	localProvider := cfg.Inventory.Provider[ProviderLocal]
	localProvider.Group["lab"] = GroupConfig{Match: InventoryMatch{"domain_suffix": []string{".lab.local"}}}
	cfg.Inventory.Provider[ProviderLocal] = localProvider
	cfg.Inventory.Host = map[string]InventoryHostConfig{
		"edge01": {Auth: InventoryAuthConfig{CredentialProvider: "pass-local", PasswordRef: "nssh/hosts/edge01"}},
	}

	if err := SaveInventoryGroupAndHostAuth(mainPath, cfg, "local/lab", "edge01"); err != nil {
		t.Fatalf("SaveInventoryGroupAndHostAuth: %v", err)
	}
	providerData, err := os.ReadFile(providerPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(providerData), "[provider.local.group.lab.match]") {
		t.Fatalf("provider config missing new group:\n%s", providerData)
	}
	mainData, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(mainData)
	for _, want := range []string{`include = ["inventory/local.toml"]`, "[inventory.host.edge01]", "nssh/hosts/edge01"} {
		if !strings.Contains(got, want) {
			t.Fatalf("root config missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "group.lab") {
		t.Fatalf("root config should not receive provider group:\n%s", got)
	}
}

func TestDeleteInventoryHostAuthRemovesRootHostAuth(t *testing.T) {
	tmp := t.TempDir()
	mainPath := filepath.Join(tmp, "config.toml")
	writeConfigFile(t, filepath.Join(tmp, "inventory", "local.toml"), `
[provider.local]
type = "local"
`)
	writeConfigFile(t, mainPath, `
[inventory]
include = ["inventory/local.toml"]

[inventory.host.edge01]
auth = { credential_provider = "pass-local", password_ref = "nssh/hosts/edge01" }

[inventory.host.edge02]
auth = { username = "netops" }
`)

	cfg, err := Load(mainPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	delete(cfg.Inventory.Host, "edge01")
	if err := DeleteInventoryHostAuth(mainPath, cfg, "edge01"); err != nil {
		t.Fatalf("DeleteInventoryHostAuth: %v", err)
	}
	data, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if strings.Contains(got, "edge01") || strings.Contains(got, "nssh/hosts/edge01") {
		t.Fatalf("removed host auth still present:\n%s", got)
	}
	for _, want := range []string{`include = ["inventory/local.toml"]`, "[inventory.host.edge02]", `username = "netops"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("root config missing %q:\n%s", want, got)
		}
	}
}

func TestInventoryHostAuthConfigTextUsesOwningSourceFormat(t *testing.T) {
	tmp := t.TempDir()
	mainPath := filepath.Join(tmp, "config.toml")
	hostPath := filepath.Join(tmp, "inventory", "hosts.toml")
	writeConfigFile(t, hostPath, `
[host.edge01]
auth = { credential_provider = "pass-local", password_ref = "nssh/hosts/edge01" }
`)
	writeConfigFile(t, mainPath, `
[inventory]
include = ["inventory/hosts.toml"]
`)

	cfg, err := Load(mainPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	text, err := InventoryHostAuthConfigText(mainPath, cfg, "edge01")
	if err != nil {
		t.Fatalf("InventoryHostAuthConfigText: %v", err)
	}
	for _, want := range []string{"[host.edge01]", `auth = { credential_provider = "pass-local", password_ref = "nssh/hosts/edge01" }`} {
		if !strings.Contains(text, want) {
			t.Fatalf("text missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "[inventory.host.edge01]") {
		t.Fatalf("text used root-scoped host format:\n%s", text)
	}
}

func TestDeleteInventoryHostAuthRemovesIncludedHostAuth(t *testing.T) {
	tmp := t.TempDir()
	mainPath := filepath.Join(tmp, "config.toml")
	hostPath := filepath.Join(tmp, "inventory", "hosts.toml")
	writeConfigFile(t, hostPath, `
[host.edge01]
auth = { credential_provider = "pass-local", password_ref = "nssh/hosts/edge01" }

[host.edge02]
auth = { username = "netops" }
`)
	writeConfigFile(t, mainPath, `
[inventory]
include = ["inventory/hosts.toml"]
`)

	cfg, err := Load(mainPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	delete(cfg.Inventory.Host, "edge01")
	if err := DeleteInventoryHostAuth(mainPath, cfg, "edge01"); err != nil {
		t.Fatalf("DeleteInventoryHostAuth: %v", err)
	}
	data, err := os.ReadFile(hostPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if strings.Contains(got, "edge01") || strings.Contains(got, "nssh/hosts/edge01") {
		t.Fatalf("removed host auth still present in include:\n%s", got)
	}
	for _, want := range []string{"[host.edge02]", `username = "netops"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("host include missing %q:\n%s", want, got)
		}
	}
	mainData, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mainData), `include = ["inventory/hosts.toml"]`) {
		t.Fatalf("root include was not preserved:\n%s", mainData)
	}
}

func TestInventoryGroupConfigTextUsesOwningSourceFormat(t *testing.T) {
	tmp := t.TempDir()
	mainPath := filepath.Join(tmp, "config.toml")
	providerPath := filepath.Join(tmp, "inventory", "local.toml")
	writeConfigFile(t, providerPath, `
[provider.local]
type = "local"

[provider.local.group.lab]
auth = { username = "netops" }

[provider.local.group.lab.match]
domain_suffix = [".lab.local"]
`)
	writeConfigFile(t, mainPath, `
[inventory]
include = ["inventory/local.toml"]
`)

	cfg, err := Load(mainPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	text, err := InventoryGroupConfigText(mainPath, cfg, "local/lab")
	if err != nil {
		t.Fatalf("InventoryGroupConfigText: %v", err)
	}
	for _, want := range []string{"[provider.local.group.lab]", `auth = { username = "netops" }`, "[provider.local.group.lab.match]", `domain_suffix = [".lab.local"]`} {
		if !strings.Contains(text, want) {
			t.Fatalf("text missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "[inventory.provider.local.group.lab]") {
		t.Fatalf("text used root-scoped group format:\n%s", text)
	}
}

func TestDeleteInventoryGroupRemovesRootGroup(t *testing.T) {
	tmp := t.TempDir()
	mainPath := filepath.Join(tmp, "config.toml")
	writeConfigFile(t, filepath.Join(tmp, "inventory", "local.toml"), `
[provider.local]
type = "local"
`)
	writeConfigFile(t, mainPath, `
[inventory]
include = ["inventory/local.toml"]

[inventory.provider.local]
type = "local"
auth = { username = "shared" }

[inventory.provider.local.group.lab.match]
domain_suffix = [".lab.local"]

[inventory.provider.local.group.keep.match]
domain_suffix = [".keep.local"]
`)

	cfg, err := Load(mainPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	localProvider := cfg.Inventory.Provider[ProviderLocal]
	delete(localProvider.Group, "lab")
	cfg.Inventory.Provider[ProviderLocal] = localProvider
	if err := DeleteInventoryGroup(mainPath, cfg, "local/lab"); err != nil {
		t.Fatalf("DeleteInventoryGroup: %v", err)
	}
	data, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if strings.Contains(got, "group.lab") || strings.Contains(got, ".lab.local") {
		t.Fatalf("removed group still present:\n%s", got)
	}
	for _, want := range []string{`include = ["inventory/local.toml"]`, "[inventory.provider.local]", `username = "shared"`, "[inventory.provider.local.group.keep.match]", `domain_suffix = [".keep.local"]`} {
		if !strings.Contains(got, want) {
			t.Fatalf("root config missing %q:\n%s", want, got)
		}
	}
}

func TestDeleteInventoryGroupRemovesProviderSourceGroup(t *testing.T) {
	tmp := t.TempDir()
	mainPath := filepath.Join(tmp, "config.toml")
	providerPath := filepath.Join(tmp, "inventory", "local.toml")
	writeConfigFile(t, providerPath, `
[provider.local]
type = "local"
auth = { username = "shared" }
config = { jump_host = "bastion" }

[provider.local.group.lab.match]
domain_suffix = [".lab.local"]

[provider.local.group.keep.match]
domain_suffix = [".keep.local"]
`)
	writeConfigFile(t, mainPath, `
[inventory]
include = ["inventory/local.toml"]
`)

	cfg, err := Load(mainPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	localProvider := cfg.Inventory.Provider[ProviderLocal]
	delete(localProvider.Group, "lab")
	cfg.Inventory.Provider[ProviderLocal] = localProvider
	if err := DeleteInventoryGroup(mainPath, cfg, "local/lab"); err != nil {
		t.Fatalf("DeleteInventoryGroup: %v", err)
	}
	providerData, err := os.ReadFile(providerPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(providerData)
	if strings.Contains(got, "group.lab") || strings.Contains(got, ".lab.local") {
		t.Fatalf("removed group still present in provider include:\n%s", got)
	}
	for _, want := range []string{"[provider.local]", `type = "local"`, `username = "shared"`, `jump_host = "bastion"`, "[provider.local.group.keep.match]", `domain_suffix = [".keep.local"]`} {
		if !strings.Contains(got, want) {
			t.Fatalf("provider config missing %q:\n%s", want, got)
		}
	}
	mainData, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	mainGot := string(mainData)
	if !strings.Contains(mainGot, `include = ["inventory/local.toml"]`) {
		t.Fatalf("root include was not preserved:\n%s", mainGot)
	}
	if strings.Contains(mainGot, "group.keep") || strings.Contains(mainGot, "group.lab") {
		t.Fatalf("provider config was flattened into root:\n%s", mainGot)
	}
}
