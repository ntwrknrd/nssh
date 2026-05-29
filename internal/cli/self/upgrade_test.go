package self

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/vault"
)

func TestMigrateConfigToInventoryMovesLegacyContextFilesIntoNsshD(t *testing.T) {
	tmp := t.TempDir()
	sshDir := filepath.Join(tmp, ".ssh")
	legacyDir := filepath.Join(sshDir, "conf.d")
	if err := os.MkdirAll(legacyDir, 0700); err != nil {
		t.Fatalf("create legacy dir: %v", err)
	}
	legacyFile := filepath.Join(legacyDir, "cbb_hosts")
	if err := os.WriteFile(legacyFile, []byte("Host edge01\n  HostName 192.0.2.1\n"), 0600); err != nil {
		t.Fatalf("write legacy file: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.Inventory = config.InventoryConfig{}
	paths := &config.Paths{SSHConfigDir: sshDir}
	contexts := []vault.ContextEntry{{
		Name:           "cbb",
		GitIncludeFile: "cbb_hosts",
		Domain:         "expedient.com",
	}}

	changed, err := migrateConfigToInventory(cfg, contexts, paths, func(move legacyGroupFileMove) (bool, error) {
		if move.Group != "cbb" {
			t.Fatalf("prompt group = %q", move.Group)
		}
		if move.Source != legacyFile {
			t.Fatalf("prompt source = %q", move.Source)
		}
		if filepath.Base(move.Destination) != "local_cbb.conf" {
			t.Fatalf("prompt destination = %q", move.Destination)
		}
		return true, nil
	})
	if err != nil {
		t.Fatalf("migrateConfigToInventory: %v", err)
	}
	if !changed {
		t.Fatalf("changed = false")
	}
	if got := cfg.Inventory.Group["cbb"].LocalFile; got != "local_cbb.conf" {
		t.Fatalf("local_file = %q", got)
	}
	if _, err := os.Stat(legacyFile); !os.IsNotExist(err) {
		t.Fatalf("legacy file still exists or stat failed: %v", err)
	}
	movedFile := filepath.Join(sshDir, "nssh.d", "local_cbb.conf")
	content, err := os.ReadFile(movedFile)
	if err != nil {
		t.Fatalf("read moved file: %v", err)
	}
	if !strings.Contains(string(content), "Host edge01") {
		t.Fatalf("moved content = %q", content)
	}
}

func TestMigrateConfigToInventoryAbortsWhenLegacyMoveDeclined(t *testing.T) {
	tmp := t.TempDir()
	sshDir := filepath.Join(tmp, ".ssh")
	legacyDir := filepath.Join(sshDir, "conf.d")
	if err := os.MkdirAll(legacyDir, 0700); err != nil {
		t.Fatalf("create legacy dir: %v", err)
	}
	legacyFile := filepath.Join(legacyDir, "cbb_hosts")
	if err := os.WriteFile(legacyFile, []byte("Host edge01\n"), 0600); err != nil {
		t.Fatalf("write legacy file: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.Inventory = config.InventoryConfig{}
	paths := &config.Paths{SSHConfigDir: sshDir}
	contexts := []vault.ContextEntry{{Name: "cbb", GitIncludeFile: "cbb_hosts"}}

	_, err := migrateConfigToInventory(cfg, contexts, paths, func(move legacyGroupFileMove) (bool, error) {
		return false, nil
	})
	if err == nil {
		t.Fatalf("expected decline error")
	}
	if !strings.Contains(err.Error(), "requires moving local inventory files") {
		t.Fatalf("error = %v", err)
	}
	if _, err := os.Stat(legacyFile); err != nil {
		t.Fatalf("legacy file changed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sshDir, "nssh.d", "local_cbb.conf")); !os.IsNotExist(err) {
		t.Fatalf("destination exists or stat failed: %v", err)
	}
}

func TestLoadConfigForUpgradeDoesNotInjectNewInventoryDefaults(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.toml")
	if err := os.WriteFile(path, []byte(`
[host.defaults]
default_context = "cbb"
`), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, _, err := loadConfigForUpgrade(path)
	if err != nil {
		t.Fatalf("loadConfigForUpgrade: %v", err)
	}
	if cfg.Inventory.DefaultGroup != "" {
		t.Fatalf("default_group = %q, want empty before migration", cfg.Inventory.DefaultGroup)
	}
	if len(cfg.Inventory.Group) != 0 {
		t.Fatalf("inventory groups = %+v, want none before migration", cfg.Inventory.Group)
	}

	changed, err := migrateConfigToInventory(cfg, nil, &config.Paths{SSHConfigDir: filepath.Join(tmp, ".ssh")}, nil)
	if err != nil {
		t.Fatalf("migrateConfigToInventory: %v", err)
	}
	if !changed {
		t.Fatal("changed = false")
	}
	if cfg.Inventory.DefaultGroup != "cbb" {
		t.Fatalf("default_group = %q, want cbb", cfg.Inventory.DefaultGroup)
	}
	if _, ok := cfg.Inventory.Group["default"]; ok {
		t.Fatalf("unexpected default group: %+v", cfg.Inventory.Group["default"])
	}
}

func TestLoadConfigForUpgradeReportsLegacySyncTable(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.toml")
	if err := os.WriteFile(path, []byte(`
[credential]
type = "age"

[inventory]
default_group = "default"

  [inventory.group.default]
  local_file = "local_default.conf"

[sync]
`), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, legacySyncConfig, err := loadConfigForUpgrade(path)
	if err != nil {
		t.Fatalf("loadConfigForUpgrade: %v", err)
	}
	if !legacySyncConfig {
		t.Fatal("legacySyncConfig = false, want true")
	}
	changed, err := migrateConfigToInventory(cfg, nil, &config.Paths{SSHConfigDir: filepath.Join(tmp, ".ssh")}, nil)
	if err != nil {
		t.Fatalf("migrateConfigToInventory: %v", err)
	}
	if changed {
		t.Fatal("migrateConfigToInventory changed config for reasons other than legacy sync table")
	}
}

func TestMigrateConfigToInventoryDoesNotInventLocalFileForMissingLegacyContext(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Inventory = config.InventoryConfig{DefaultGroup: "cbb", Group: map[string]config.GroupConfig{"cbb": {LocalFile: "local_cbb.conf"}}}
	paths := &config.Paths{SSHConfigDir: filepath.Join(tmp, ".ssh")}
	contexts := []vault.ContextEntry{{Name: "containerlab", GitIncludeFile: "containerlab"}}

	changed, err := migrateConfigToInventory(cfg, contexts, paths, nil)
	if err != nil {
		t.Fatalf("migrateConfigToInventory: %v", err)
	}
	if !changed {
		t.Fatal("changed = false")
	}
	group, ok := cfg.Inventory.Group["containerlab"]
	if !ok {
		t.Fatal("containerlab group missing")
	}
	if group.LocalFile != "" {
		t.Fatalf("containerlab local_file = %q, want empty", group.LocalFile)
	}
}
