package credential

import (
	"path/filepath"
	"testing"

	"filippo.io/age"
	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/secret"
	"github.com/ntwrknrd/nssh/internal/vault"
)

func newTestManager(t *testing.T) *vault.Manager {
	t.Helper()
	tmpDir := t.TempDir()
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("generate test key: %v", err)
	}
	paths := &config.Paths{
		CredentialsFile: filepath.Join(tmpDir, "credentials.age"),
		ConfigDir:       tmpDir,
		DataDir:         tmpDir,
		StateDir:        tmpDir,
		BackupDir:       filepath.Join(tmpDir, "backups"),
	}
	mgr, err := vault.NewManager(vault.Provided(identity), vault.WithPaths(paths), vault.WithMaxBackups(5))
	if err != nil {
		t.Fatalf("create manager: %v", err)
	}
	return mgr
}

func TestAgeProviderHostCredentialRoundTrip(t *testing.T) {
	provider := NewAgeProvider(newTestManager(t))
	if err := provider.SetHost("edge01", &Record{Username: "admin", Secret: secret.NewFromString("hostpass")}); err != nil {
		t.Fatalf("set host: %v", err)
	}
	got, err := provider.GetHost("edge01")
	if err != nil {
		t.Fatalf("get host: %v", err)
	}
	if got == nil || got.Username != "admin" {
		t.Fatalf("host credential = %+v", got)
	}
	var password string
	if err := got.Secret.UseString(func(s string) error {
		password = s
		return nil
	}); err != nil {
		t.Fatalf("read secret: %v", err)
	}
	if password != "hostpass" {
		t.Fatalf("password = %q", password)
	}
}

func TestAgeProviderGroupCredentialRoundTrip(t *testing.T) {
	provider := NewAgeProvider(newTestManager(t))
	if err := provider.SetGroup("custcbb", &Record{Username: "netops", Secret: secret.NewFromString("grouppass")}); err != nil {
		t.Fatalf("set group: %v", err)
	}
	got, err := provider.GetGroup("custcbb")
	if err != nil {
		t.Fatalf("get group: %v", err)
	}
	if got == nil || got.Username != "netops" {
		t.Fatalf("group credential = %+v", got)
	}
	removed, err := provider.RemoveGroup("custcbb")
	if err != nil {
		t.Fatalf("remove group: %v", err)
	}
	if !removed {
		t.Fatal("expected group credential removal")
	}
	got, err = provider.GetGroup("custcbb")
	if err != nil {
		t.Fatalf("get removed group: %v", err)
	}
	if got != nil {
		t.Fatalf("expected removed group credential, got %+v", got)
	}
}
