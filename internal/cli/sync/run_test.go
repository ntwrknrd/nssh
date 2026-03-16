package sync

import (
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"
	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/vault"
)

const testAgeKey = `# created: 2025-11-29T19:01:23-05:00
# public key: age16g638l7m76qr7v4qwyhmnqt0yj5gzxxc740u34t2hh59rmc6av4qzajq74
AGE-SECRET-KEY-1K5WWGTS4U7ZTST0PN4E4SN3XK0YFLY9KZNN4H272FRYPE5G6M4ZQ7Y7ED6
`

func newTestManager(t *testing.T) *vault.Manager {
	t.Helper()
	tmpDir := t.TempDir()

	identities, err := age.ParseIdentities(strings.NewReader(testAgeKey))
	if err != nil {
		t.Fatalf("parse test key: %v", err)
	}
	identity := identities[0].(*age.X25519Identity)

	paths := &config.Paths{
		CredentialsFile: filepath.Join(tmpDir, "credentials.age"),
		ConfigDir:       tmpDir,
		DataDir:         tmpDir,
		StateDir:        tmpDir,
		BackupDir:       filepath.Join(tmpDir, "backups"),
	}

	mgr, err := vault.NewManager(
		vault.Provided(identity),
		vault.WithPaths(paths),
		vault.WithMaxBackups(5),
	)
	if err != nil {
		t.Fatalf("create manager: %v", err)
	}
	return mgr
}

func TestValidateRouteContexts(t *testing.T) {
	mgr := newTestManager(t)

	// Create "lab" context in vault
	if err := mgr.CreateContext("lab", "lab_hosts", "", nil); err != nil {
		t.Fatalf("create context: %v", err)
	}

	// Route referencing nonexistent "typo" context should fail
	sources := []config.SyncSourceConfig{
		{
			Name:     "test",
			Provider: "containerlab",
			Routes: []config.SyncRouteConfig{
				{Name: "bad", Context: "typo"},
			},
		},
	}

	err := validateRouteContexts(sources, mgr)
	if err == nil {
		t.Fatal("expected error for nonexistent context")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %s", err)
	}

	// Route referencing existing "lab" context should succeed
	sources = []config.SyncSourceConfig{
		{
			Name:     "test",
			Provider: "containerlab",
			Routes: []config.SyncRouteConfig{
				{Name: "good", Context: "lab"},
			},
		},
	}

	if err := validateRouteContexts(sources, mgr); err != nil {
		t.Fatalf("unexpected error for valid context: %v", err)
	}
}
