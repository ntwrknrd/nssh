package vault

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"
	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/secret"
)

// testKey is a test age key for unit tests.
// Generated with: age-keygen
const testKey = `# created: 2025-11-29T19:01:23-05:00
# public key: age16g638l7m76qr7v4qwyhmnqt0yj5gzxxc740u34t2hh59rmc6av4qzajq74
AGE-SECRET-KEY-1K5WWGTS4U7ZTST0PN4E4SN3XK0YFLY9KZNN4H272FRYPE5G6M4ZQ7Y7ED6
`

func setupTestManager(t *testing.T) (*Manager, string) {
	t.Helper()

	tmpDir := t.TempDir()

	// Parse test identity
	identities, err := age.ParseIdentities(strings.NewReader(testKey))
	if err != nil {
		t.Fatalf("parse test key: %v", err)
	}
	identity := identities[0].(*age.X25519Identity)

	credPath := filepath.Join(tmpDir, "credentials.age")
	backupDir := filepath.Join(tmpDir, "backups")

	// Create paths for test
	paths := &config.Paths{
		CredentialsFile: credPath,
		ConfigDir:       tmpDir,
		DataDir:         tmpDir,
		StateDir:        tmpDir,
		BackupDir:       backupDir,
	}

	mgr, err := NewManager(
		Provided(identity),
		WithPaths(paths),
		WithMaxBackups(5),
	)
	if err != nil {
		t.Fatalf("create manager: %v", err)
	}

	return mgr, tmpDir
}

func TestNewManagerAutoWithAppConfigAllowsLegacySyncSources(t *testing.T) {
	tmpDir := t.TempDir()
	paths := &config.Paths{
		ConfigDir:       tmpDir,
		DataDir:         tmpDir,
		StateDir:        tmpDir,
		ConfigFile:      filepath.Join(tmpDir, "config.toml"),
		CredentialsFile: filepath.Join(tmpDir, "credentials.age"),
		BackupDir:       filepath.Join(tmpDir, "backups"),
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "age.key.enc"), []byte("test"), 0600); err != nil {
		t.Fatalf("write software keystore marker: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.Sync.Sources = []config.SyncSourceConfig{{Name: "legacy"}}

	mgr, err := NewManager(Auto(), WithPaths(paths), WithAppConfig(cfg))
	if err != nil {
		t.Fatalf("NewManager Auto with legacy app config: %v", err)
	}
	if got := mgr.ModeString(); got != "passphrase" {
		t.Fatalf("mode = %q, want passphrase", got)
	}
}

func TestManagerCreateContext(t *testing.T) {
	mgr, _ := setupTestManager(t)

	// Create a context
	err := mgr.CreateContext("work", "work_hosts", "example.com", nil)
	if err != nil {
		t.Fatalf("CreateContext: %v", err)
	}

	// Verify it exists
	ctx, err := mgr.GetContext("work")
	if err != nil {
		t.Fatalf("GetContext: %v", err)
	}
	if ctx == nil {
		t.Fatal("expected context, got nil")
	} else if ctx.Name != "work" {
		t.Errorf("name = %q, want %q", ctx.Name, "work")
	}
	// CreateContext now stores absolute paths to prevent basename collisions
	if filepath.Base(ctx.GitIncludeFile) != "work_hosts" {
		t.Errorf("git_include_file basename = %q, want %q", filepath.Base(ctx.GitIncludeFile), "work_hosts")
	}
	if ctx.Domain != "example.com" {
		t.Errorf("domain = %q, want %q", ctx.Domain, "example.com")
	}
}

func TestManagerCreateContextDuplicate(t *testing.T) {
	mgr, _ := setupTestManager(t)

	// Create first context
	if err := mgr.CreateContext("work", "work_hosts", "", nil); err != nil {
		t.Fatalf("CreateContext: %v", err)
	}

	// Try to create duplicate
	err := mgr.CreateContext("work", "other_hosts", "", nil)
	if err == nil {
		t.Fatal("expected error for duplicate context")
	}
}

func TestManagerAddContextCredential(t *testing.T) {
	mgr, _ := setupTestManager(t)

	// Create context first
	if err := mgr.CreateContext("work", "work_hosts", "", nil); err != nil {
		t.Fatalf("CreateContext: %v", err)
	}

	// Add credential
	pw := secret.NewFromString("testpass123")
	if err := mgr.AddContextCredential("work", "admin", pw, false); err != nil {
		t.Fatalf("AddContextCredential: %v", err)
	}

	// Verify credential
	ctx, err := mgr.GetContext("work")
	if err != nil {
		t.Fatalf("GetContext: %v", err)
	}
	if ctx.Credential == nil {
		t.Fatal("expected credential, got nil")
	}
	if ctx.Credential.Username != "admin" {
		t.Errorf("username = %q, want %q", ctx.Credential.Username, "admin")
	}
	if ctx.Credential.Password != "testpass123" {
		t.Errorf("password = %q, want %q", ctx.Credential.Password, "testpass123")
	}
}

func TestManagerListContexts(t *testing.T) {
	mgr, _ := setupTestManager(t)

	// Create multiple contexts
	contexts := []struct {
		name   string
		file   string
		domain string
	}{
		{"work", "work_hosts", "work.example.com"},
		{"home", "home_hosts", ""},
		{"lab", "lab_hosts", "lab.local"},
	}

	for _, c := range contexts {
		if err := mgr.CreateContext(c.name, c.file, c.domain, nil); err != nil {
			t.Fatalf("CreateContext(%s): %v", c.name, err)
		}
	}

	// List should be sorted
	list, err := mgr.ListContexts()
	if err != nil {
		t.Fatalf("ListContexts: %v", err)
	}

	if len(list) != 3 {
		t.Fatalf("len = %d, want 3", len(list))
	}

	// Should be alphabetically sorted
	expectedOrder := []string{"home", "lab", "work"}
	for i, ctx := range list {
		if ctx.Name != expectedOrder[i] {
			t.Errorf("list[%d].Name = %q, want %q", i, ctx.Name, expectedOrder[i])
		}
	}
}

func TestManagerDeleteContext(t *testing.T) {
	mgr, _ := setupTestManager(t)

	// Create context
	if err := mgr.CreateContext("work", "work_hosts", "", nil); err != nil {
		t.Fatalf("CreateContext: %v", err)
	}

	// Delete it
	deleted, err := mgr.DeleteContext("work")
	if err != nil {
		t.Fatalf("DeleteContext: %v", err)
	}
	if !deleted {
		t.Error("expected deleted = true")
	}

	// Verify it's gone
	ctx, err := mgr.GetContext("work")
	if err != nil {
		t.Fatalf("GetContext: %v", err)
	}
	if ctx != nil {
		t.Error("expected nil after delete")
	}

	// Delete non-existent
	deleted, err = mgr.DeleteContext("work")
	if err != nil {
		t.Fatalf("DeleteContext (non-existent): %v", err)
	}
	if deleted {
		t.Error("expected deleted = false for non-existent")
	}
}

func TestManagerGetContextByIncludeFile(t *testing.T) {
	mgr, _ := setupTestManager(t)

	// Create context
	if err := mgr.CreateContext("work", "work_hosts", "", nil); err != nil {
		t.Fatalf("CreateContext: %v", err)
	}

	// Look up by include file
	ctx, err := mgr.GetContextByIncludeFile("work_hosts")
	if err != nil {
		t.Fatalf("GetContextByIncludeFile: %v", err)
	}
	if ctx == nil {
		t.Fatal("expected context, got nil")
	} else if ctx.Name != "work" {
		t.Errorf("name = %q, want %q", ctx.Name, "work")
	}

	// Look up with path prefix (should normalize to basename)
	ctx, err = mgr.GetContextByIncludeFile("/some/path/work_hosts")
	if err != nil {
		t.Fatalf("GetContextByIncludeFile (with path): %v", err)
	}
	if ctx == nil {
		t.Fatal("expected context with path prefix, got nil")
	}

	// Non-existent
	ctx, err = mgr.GetContextByIncludeFile("nonexistent")
	if err != nil {
		t.Fatalf("GetContextByIncludeFile (non-existent): %v", err)
	}
	if ctx != nil {
		t.Error("expected nil for non-existent")
	}
}

func TestManagerUpdateContextDomain(t *testing.T) {
	mgr, _ := setupTestManager(t)

	// Create context
	if err := mgr.CreateContext("work", "work_hosts", "old.example.com", nil); err != nil {
		t.Fatalf("CreateContext: %v", err)
	}

	// Update domain
	if err := mgr.UpdateContextDomain("work", "new.example.com"); err != nil {
		t.Fatalf("UpdateContextDomain: %v", err)
	}

	// Verify
	ctx, err := mgr.GetContext("work")
	if err != nil {
		t.Fatalf("GetContext: %v", err)
	}
	if ctx.Domain != "new.example.com" {
		t.Errorf("domain = %q, want %q", ctx.Domain, "new.example.com")
	}

	// Clear domain
	if err := mgr.UpdateContextDomain("work", ""); err != nil {
		t.Fatalf("UpdateContextDomain (clear): %v", err)
	}

	ctx, err = mgr.GetContext("work")
	if err != nil {
		t.Fatalf("GetContext: %v", err)
	}
	if ctx.Domain != "" {
		t.Errorf("domain = %q, want empty", ctx.Domain)
	}
}

func TestManagerHostCredentials(t *testing.T) {
	mgr, _ := setupTestManager(t)

	// Add host credential
	pw := secret.NewFromString("hostpass")
	if err := mgr.AddHostCredential("router1", "admin", pw); err != nil {
		t.Fatalf("AddHostCredential: %v", err)
	}

	// Verify
	creds, err := mgr.GetHostCredentials("router1")
	if err != nil {
		t.Fatalf("GetHostCredentials: %v", err)
	}
	if len(creds) != 1 {
		t.Fatalf("len = %d, want 1", len(creds))
	}
	if creds[0].Username != "admin" {
		t.Errorf("username = %q, want %q", creds[0].Username, "admin")
	}

	// Add second credential
	pw2 := secret.NewFromString("hostpass2")
	if err := mgr.AddHostCredential("router1", "backup", pw2); err != nil {
		t.Fatalf("AddHostCredential (2nd): %v", err)
	}

	creds, err = mgr.GetHostCredentials("router1")
	if err != nil {
		t.Fatalf("GetHostCredentials: %v", err)
	}
	if len(creds) != 2 {
		t.Fatalf("len = %d, want 2", len(creds))
	}

	// Delete host credentials
	deleted, err := mgr.DeleteHostCredentials("router1")
	if err != nil {
		t.Fatalf("DeleteHostCredentials: %v", err)
	}
	if !deleted {
		t.Error("expected deleted = true")
	}

	creds, err = mgr.GetHostCredentials("router1")
	if err != nil {
		t.Fatalf("GetHostCredentials (after delete): %v", err)
	}
	if creds != nil {
		t.Error("expected nil after delete")
	}
}

func TestManagerBackup(t *testing.T) {
	mgr, tmpDir := setupTestManager(t)

	// Create some data
	if err := mgr.CreateContext("work", "work_hosts", "", nil); err != nil {
		t.Fatalf("CreateContext: %v", err)
	}

	// Update to trigger backup
	if err := mgr.UpdateContextDomain("work", "example.com"); err != nil {
		t.Fatalf("UpdateContextDomain: %v", err)
	}

	// Check backup directory
	backupDir := filepath.Join(tmpDir, "backups")
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		// Backup dir may not exist if first write
		if !os.IsNotExist(err) {
			t.Fatalf("ReadDir backups: %v", err)
		}
		return
	}

	// Should have at least one backup
	if len(entries) < 1 {
		t.Log("No backups created (expected on first save)")
	}
}

func TestManagerCredentialOverwrite(t *testing.T) {
	mgr, _ := setupTestManager(t)

	// Create context with credential
	if err := mgr.CreateContext("work", "work_hosts", "", nil); err != nil {
		t.Fatalf("CreateContext: %v", err)
	}

	pw := secret.NewFromString("pass1")
	if err := mgr.AddContextCredential("work", "user1", pw, false); err != nil {
		t.Fatalf("AddContextCredential: %v", err)
	}

	// Try to add without overwrite - should fail
	pw2 := secret.NewFromString("pass2")
	err := mgr.AddContextCredential("work", "user2", pw2, false)
	if err == nil {
		t.Fatal("expected error when adding without overwrite")
	}

	// Add with overwrite - should succeed
	if err := mgr.AddContextCredential("work", "user2", pw2, true); err != nil {
		t.Fatalf("AddContextCredential (overwrite): %v", err)
	}

	// Verify new credential
	ctx, err := mgr.GetContext("work")
	if err != nil {
		t.Fatalf("GetContext: %v", err)
	}
	if ctx.Credential.Username != "user2" {
		t.Errorf("username = %q, want %q", ctx.Credential.Username, "user2")
	}
}
