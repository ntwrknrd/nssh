package resolve

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"filippo.io/age"
	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/secret"
	intsync "github.com/ntwrknrd/nssh/internal/sync"
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

func TestResolveSyncContext(t *testing.T) {
	// Isolate state directory
	stateDir := t.TempDir()
	intsync.SetStateDir(stateDir)
	t.Cleanup(func() { intsync.SetStateDir("") })

	mgr := newTestManager(t)

	// Create vault contexts with credentials
	if err := mgr.CreateContext("lab", "lab_hosts", "", nil); err != nil {
		t.Fatalf("create lab context: %v", err)
	}
	if err := mgr.AddContextCredential("lab", "labuser", secret.NewFromString("labpass"), false); err != nil {
		t.Fatalf("add lab credential: %v", err)
	}
	if err := mgr.CreateContext("prod", "prod_hosts", "", nil); err != nil {
		t.Fatalf("create prod context: %v", err)
	}
	if err := mgr.AddContextCredential("prod", "produser", secret.NewFromString("prodpass"), false); err != nil {
		t.Fatalf("add prod credential: %v", err)
	}

	// Create sync source with class credential "ceos"
	if err := mgr.SetSyncSourceClassCredential("test-lab", "ceos", &vault.Credential{
		Username: "ceosadmin",
		Password: "ceospass",
	}); err != nil {
		t.Fatalf("set class credential: %v", err)
	}

	// Save sync state with two hosts in different contexts
	state := &intsync.SourceState{
		Version:  intsync.StateVersion,
		Source:   "test-lab",
		Provider: "containerlab",
		LastSync: time.Now().UTC(),
		Objects: map[string]*intsync.ManagedHost{
			"lab1/host-a": {
				ObjectID:        "lab1/host-a",
				Host:            "clab-host-a",
				Context:         "lab",
				HostName:        "172.20.0.2",
				CredentialClass: "ceos",
			},
			"lab1/host-b": {
				ObjectID:        "lab1/host-b",
				Host:            "clab-host-b",
				Context:         "prod",
				HostName:        "172.20.0.3",
				CredentialClass: "ceos",
			},
		},
	}
	if err := intsync.SaveSourceState(state); err != nil {
		t.Fatalf("save state: %v", err)
	}

	// Phase 1: Class credential takes priority over context
	cred := resolveSyncCredential(mgr, "clab-host-a", "")
	if cred == nil {
		t.Fatal("expected credential for host-a, got nil")
	} else if cred.Source != vault.CredSourceSyncClass {
		t.Errorf("phase 1: source = %q, want %q", cred.Source, vault.CredSourceSyncClass)
	}

	// Phase 2: Remove class credential -- context should win
	// Delete and recreate with non-matching class to keep sync source entry alive
	if err := mgr.DeleteSyncSource("test-lab"); err != nil {
		t.Fatalf("delete sync source: %v", err)
	}
	if err := mgr.SetSyncSourceClassCredential("test-lab", "other", &vault.Credential{
		Username: "placeholder",
		Password: "placeholder",
	}); err != nil {
		t.Fatalf("recreate sync source: %v", err)
	}

	// host-a should resolve to "lab" context credential.
	// Pass a non-empty username (simulating SSH config / defaults already
	// resolved) to verify context credential owns the username and is NOT
	// overwritten by the pre-resolved value.
	credA := resolveSyncCredential(mgr, "clab-host-a", "sshdefault")
	if credA == nil {
		t.Fatal("expected credential for host-a after class removal, got nil")
	} else {
		if credA.Source != vault.CredSourceSyncContext {
			t.Errorf("host-a: source = %q, want %q", credA.Source, vault.CredSourceSyncContext)
		}
		if credA.Username != "labuser" {
			t.Errorf("host-a: username = %q, want %q (context must own username)", credA.Username, "labuser")
		}
	}

	// host-b should resolve to "prod" context credential
	credB := resolveSyncCredential(mgr, "clab-host-b", "sshdefault")
	if credB == nil {
		t.Fatal("expected credential for host-b, got nil")
	} else {
		if credB.Source != vault.CredSourceSyncContext {
			t.Errorf("host-b: source = %q, want %q", credB.Source, vault.CredSourceSyncContext)
		}
		if credB.Username != "produser" {
			t.Errorf("host-b: username = %q, want %q (context must own username)", credB.Username, "produser")
		}

		// Different contexts must yield different credentials
		if credA.Username == credB.Username {
			t.Errorf("expected different usernames, both got %q", credA.Username)
		}
	}
}
