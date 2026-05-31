package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadSupportsRootAndSectionScopedIncludes(t *testing.T) {
	tmp := t.TempDir()
	writeConfigFile(t, filepath.Join(tmp, "base.toml"), `
[agent]
idle_timeout = "30m"
activity_increment = "5m"
max_lifetime = "8h"
`)
	writeConfigFile(t, filepath.Join(tmp, "credentials", "example.toml"), `
[provider.op-network]
type = "1password"
config = { vault = "ExampleCorp", session = "agent" }
`)
	writeConfigFile(t, filepath.Join(tmp, "inventory", "groups", "corp.toml"), `
[provider.local]
type = "local"

[provider.local.group.corp]
domain_suffix = [".example.com"]
auth = { username = "shared-user" }

[provider.local.group.customer]
domain_suffix = [".customer.local"]
auth = { credential_provider = "op-network", password_ref = "op://ExampleCorp/item/password", username = "netops" }

[provider.netbox-prod.group.corp.match]
domain_suffix = [".example.com"]
manufacturer = ["Juniper", "Arista"]
`)
	writeConfigFile(t, filepath.Join(tmp, "inventory", "netbox.toml"), `
[provider.netbox-prod]
type = "netbox"
config = { url_env = "NETBOX_URL", token_env = "NETBOX_TOKEN" }
`)
	mainPath := filepath.Join(tmp, "config.toml")
	writeConfigFile(t, mainPath, `
include = ["base.toml"]

[agent]
idle_timeout = "4h"

[credential]
include = ["credentials/*.toml"]

[inventory]
include = ["inventory/groups/*.toml", "inventory/netbox.toml"]

[inventory.provider.local.group.corp]
auth = { username = "local-user" }
`)

	cfg, err := Load(mainPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Agent.IdleTimeout.Duration() != 4*time.Hour {
		t.Fatalf("agent.idle_timeout = %v", cfg.Agent.IdleTimeout.Duration())
	}
	if cfg.Agent.ActivityIncrement.Duration() != 5*time.Minute {
		t.Fatalf("agent.activity_increment = %v", cfg.Agent.ActivityIncrement.Duration())
	}
	if cfg.Credential.Provider["op-network"].Config.Vault != "ExampleCorp" {
		t.Fatalf("op-network provider = %+v", cfg.Credential.Provider["op-network"])
	}
	if _, ok := cfg.Credential.Provider["pass-local"]; ok {
		t.Fatalf("implicit pass-local provider should not be retained when config defines providers: %+v", cfg.Credential.Provider)
	}
	if cfg.Inventory.Provider["local"].Group["corp"].Auth.Username != "local-user" {
		t.Fatalf("corp auth username = %q", cfg.Inventory.Provider["local"].Group["corp"].Auth.Username)
	}
	if cfg.Inventory.Provider["local"].Group["customer"].Auth.CredentialProvider != "op-network" {
		t.Fatalf("customer auth = %+v", cfg.Inventory.Provider["local"].Group["customer"].Auth)
	}
	if got := cfg.Inventory.ProviderSelectors("netbox-prod"); len(got) != 1 || got[0].Group != "netbox-prod/corp" {
		t.Fatalf("netbox selectors = %+v", got)
	}
	if source := cfg.InventoryGroupSource("local", "customer"); !strings.HasSuffix(source, filepath.Join("inventory", "groups", "corp.toml")) {
		t.Fatalf("customer source = %q", source)
	}
	if source := cfg.InventoryGroupSource("local", "corp"); source != mainPath {
		t.Fatalf("corp source = %q, want root file", source)
	}
}

func TestLoadProviderConfigDoesNotRetainImplicitPassLocalGroupAuth(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.toml")
	writeConfigFile(t, path, `
[credential.provider.op-network]
type = "1password"
config = { vault = "Network", session = "agent" }
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := cfg.Credential.Provider["pass-local"]; ok {
		t.Fatalf("implicit pass-local provider should not be retained: %+v", cfg.Credential.Provider)
	}
	if auth := cfg.Inventory.Provider["local"].Group["default"].Auth; auth.IsSet() {
		t.Fatalf("implicit default group auth should not be retained: %+v", auth)
	}
}

func TestLoadIncludeGlobOrderAndArrayReplacement(t *testing.T) {
	tmp := t.TempDir()
	writeConfigFile(t, filepath.Join(tmp, "conf.d", "01-base.toml"), `
[inventory.provider.netbox-prod]
type = "netbox"

[inventory.provider.netbox-prod.group.default.match]
role = ["router"]
`)
	writeConfigFile(t, filepath.Join(tmp, "conf.d", "02-override.toml"), `
[inventory.provider.netbox-prod.group.default.match]
role = ["switch"]
`)
	mainPath := filepath.Join(tmp, "config.toml")
	writeConfigFile(t, mainPath, `include = ["conf.d/*.toml"]`)

	cfg, err := Load(mainPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	selectors := cfg.Inventory.ProviderSelectors("netbox-prod")
	if len(selectors) != 1 {
		t.Fatalf("selector count = %d, want one selector: %+v", len(selectors), selectors)
	}
	if selectors[0].Group != "netbox-prod/default" || selectors[0].Match["role"][0] != "switch" {
		t.Fatalf("selector = %+v", selectors[0])
	}
}

func TestLoadReportsMissingIncludeAndCycles(t *testing.T) {
	tmp := t.TempDir()
	missingPath := filepath.Join(tmp, "missing.toml")
	writeConfigFile(t, missingPath, `include = ["does-not-exist/*.toml"]`)
	if _, err := Load(missingPath); err == nil || !strings.Contains(err.Error(), "no matches") {
		t.Fatalf("missing include error = %v", err)
	}

	writeConfigFile(t, filepath.Join(tmp, "a.toml"), `include = ["b.toml"]`)
	writeConfigFile(t, filepath.Join(tmp, "b.toml"), `include = ["a.toml"]`)
	if _, err := Load(filepath.Join(tmp, "a.toml")); err == nil || !strings.Contains(err.Error(), "include cycle") {
		t.Fatalf("cycle error = %v", err)
	}
}

func TestLoadRejectsUnknownConfigKeys(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.toml")
	writeConfigFile(t, path, `
[inventory.provider.local.group.corp]
local_file = "local_corp.conf"

[inventory.provider.netbox-prod]
type = "netbox"
refresh_interval = "15m"
`)

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected unknown key error")
	}
	if !strings.Contains(err.Error(), "inventory.provider.local.group.corp.local_file") {
		t.Fatalf("error %q does not mention stale group key", err)
	}
}

func TestLoadRejectsCredentialDefaultProvider(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.toml")
	writeConfigFile(t, path, `
[credential]
default_provider = "pass-local"

[credential.provider.pass-local]
type = "pass"

[inventory.group.default.auth]
credential_provider = "pass-local"
password_ref = "nssh/groups/default"
`)

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected unknown default_provider error")
	}
	if !strings.Contains(err.Error(), "credential.default_provider") {
		t.Fatalf("error %q does not mention credential.default_provider", err)
	}
}

func TestLoadRejectsInventoryDefaultGroup(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.toml")
	writeConfigFile(t, path, `
[inventory]
default_group = "default"

[inventory.group.default]
`)

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected unknown default_group error")
	}
	if !strings.Contains(err.Error(), "inventory.default_group") {
		t.Fatalf("error %q does not mention inventory.default_group", err)
	}
}

func TestMarshalSparseIncludesCommentsForEmittedOptions(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Agent.IdleTimeout = Duration(4 * time.Hour)
	cfg.Agent.ActivityIncrement = Duration(30 * time.Minute)
	cfg.Agent.MaxLifetime = Duration(8 * time.Hour)
	cfg.Logging.Session.Enabled = boolPtr(true)
	cfg.Logging.Session.WindowSize = "145x30"
	cfg.Logging.Session.AutoExportTxt = true
	cfg.SSH.Security.HostKeyPolicy = "tofu"

	got, err := MarshalSparse(cfg)
	if err != nil {
		t.Fatalf("MarshalSparse: %v", err)
	}
	for _, want := range []string{
		"# How long the nssh runtime agent can sit idle before exiting.",
		"# Record SSH sessions automatically.",
		"# Host key behavior preset.",
		`idle_timeout = "4h0m0s"`,
		`window_size = "145x30"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("sparse config missing %q:\n%s", want, got)
		}
	}
	for _, reject := range []string{"default_context", "base_url = \"\"", "account = \"\"", "default_provider", "default_group", "op-network", "ExampleCorp"} {
		if strings.Contains(got, reject) {
			t.Fatalf("sparse config should omit %q:\n%s", reject, got)
		}
	}
}

func TestMarshalSparseOmitsDefaultSessionArchiveSettings(t *testing.T) {
	cfg := DefaultConfig()

	got, err := MarshalSparse(cfg)
	if err != nil {
		t.Fatalf("MarshalSparse: %v", err)
	}
	if strings.Contains(got, "[logging.session.archive]") {
		t.Fatalf("default archive settings should be omitted from sparse config:\n%s", got)
	}
}

func TestMarshalSparseIncludesNonDefaultSessionArchiveSettings(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Logging.Session.Archive.Enabled = true
	cfg.Logging.Session.Archive.Dir = filepath.Join(t.TempDir(), "archive")

	got, err := MarshalSparse(cfg)
	if err != nil {
		t.Fatalf("MarshalSparse: %v", err)
	}
	for _, want := range []string{
		"[logging.session.archive]",
		"enabled = true",
		`dir = "`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("sparse config missing %q:\n%s", want, got)
		}
	}
}

func boolPtr(v bool) *bool {
	return &v
}

func writeConfigFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
