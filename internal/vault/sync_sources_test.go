package vault

import (
	"testing"
)

func TestSyncSourceCRUD(t *testing.T) {
	mgr, _ := setupTestManager(t)

	// Initially empty
	sources, err := mgr.ListSyncSources()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(sources) != 0 {
		t.Fatalf("expected 0 sources, got %d", len(sources))
	}

	// Get nonexistent
	sv, err := mgr.GetSyncSource("lab1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if sv != nil {
		t.Fatal("expected nil for nonexistent source")
	}

	// Set default credential
	if err := mgr.SetSyncSourceDefaultCredential("lab1", &Credential{
		Username: "admin",
		Password: "secret",
	}); err != nil {
		t.Fatalf("set default: %v", err)
	}

	sv, err = mgr.GetSyncSource("lab1")
	if err != nil {
		t.Fatalf("get after set: %v", err)
	}
	if sv == nil {
		t.Fatal("expected non-nil source")
	} else if sv.DefaultCredential == nil || sv.DefaultCredential.Username != "admin" {
		t.Errorf("default credential = %+v", sv.DefaultCredential)
	}

	// Set class credential
	if err := mgr.SetSyncSourceClassCredential("lab1", "ceos", &Credential{
		Username: "admin",
		Password: "ceos-pass",
	}); err != nil {
		t.Fatalf("set class: %v", err)
	}

	// Resolve class credential
	cred, err := mgr.GetSyncSourceCredential("lab1", "ceos")
	if err != nil {
		t.Fatalf("get credential: %v", err)
	}
	if cred == nil || cred.Password != "ceos-pass" {
		t.Errorf("class credential = %+v", cred)
	}

	// Resolve unknown class falls back to default
	cred, err = mgr.GetSyncSourceCredential("lab1", "unknown-kind")
	if err != nil {
		t.Fatalf("get credential fallback: %v", err)
	}
	if cred == nil || cred.Username != "admin" || cred.Password != "secret" {
		t.Errorf("fallback credential = %+v", cred)
	}

	// List
	sources, err = mgr.ListSyncSources()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(sources) != 1 || sources[0] != "lab1" {
		t.Errorf("sources = %v", sources)
	}

	// Delete
	if err := mgr.DeleteSyncSource("lab1"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	sv, err = mgr.GetSyncSource("lab1")
	if err != nil {
		t.Fatalf("get after delete: %v", err)
	}
	if sv != nil {
		t.Error("expected nil after delete")
	}
}

func TestOldVaultWithoutSyncSources(t *testing.T) {
	mgr, _ := setupTestManager(t)

	// Create a vault with only contexts (simulates old vault)
	if err := mgr.CreateContext("test", "conf.d/test", "", &Credential{
		Username: "user",
		Password: "pass",
	}); err != nil {
		t.Fatalf("create context: %v", err)
	}

	// Load should succeed and SyncSources should be initialized
	data, err := mgr.load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if data.SyncSources == nil {
		t.Error("SyncSources should be initialized even on old vaults")
	}

	// Sync operations should work on this vault
	sv, err := mgr.GetSyncSource("anything")
	if err != nil {
		t.Fatalf("get sync source on old vault: %v", err)
	}
	if sv != nil {
		t.Error("expected nil for nonexistent source")
	}
}
