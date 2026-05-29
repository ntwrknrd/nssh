package resolve

import (
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"
	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/secret"
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

func TestResolveTargetCredentialHostOverridesGroup(t *testing.T) {
	mgr := newTestManager(t)
	if err := mgr.SetGroupCredential("lab", "groupuser", secret.NewFromString("grouppass")); err != nil {
		t.Fatalf("set group credential: %v", err)
	}
	if err := mgr.AddHostCredential("edge01", "hostuser", secret.NewFromString("hostpass")); err != nil {
		t.Fatalf("add host credential: %v", err)
	}
	if err := mgr.SetHostDefaultCredential("edge01", "hostuser"); err != nil {
		t.Fatalf("set host default: %v", err)
	}

	cred, err := resolveTargetCredential(mgr, "edge01", "lab", "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if cred == nil {
		t.Fatal("expected credential")
	}
	if cred.Source != vault.CredSourceHost {
		t.Fatalf("source = %q, want host", cred.Source)
	}
	if cred.Username != "hostuser" {
		t.Fatalf("username = %q", cred.Username)
	}
}

func TestResolveTargetCredentialFallsBackToGroup(t *testing.T) {
	mgr := newTestManager(t)
	if err := mgr.SetGroupCredential("lab", "groupuser", secret.NewFromString("grouppass")); err != nil {
		t.Fatalf("set group credential: %v", err)
	}

	cred, err := resolveTargetCredential(mgr, "edge01", "lab", "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if cred == nil {
		t.Fatal("expected credential")
	}
	if cred.Source != vault.CredSourceGroup {
		t.Fatalf("source = %q, want group", cred.Source)
	}
	if cred.Username != "groupuser" {
		t.Fatalf("username = %q", cred.Username)
	}
}
