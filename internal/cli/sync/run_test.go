package sync

import (
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"
	"github.com/ntwrknrd/nssh/internal/cli/resolve"
	"github.com/ntwrknrd/nssh/internal/config"
	"github.com/ntwrknrd/nssh/internal/ssh/sshconfig"
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

func TestRemoteExecHostInfoAllowsKeyTransportWithVaultCredential(t *testing.T) {
	resolved := &resolve.ResolvedHost{
		Hostname: "nre-netlab01.custcbb.local",
		Username: "contextuser",
		HostEntry: &sshconfig.HostEntry{
			Properties: map[string]string{
				"user":                 "nre",
				"pubkeyauthentication": "yes",
				"identityfile":         "~/.ssh/1Password",
			},
		},
		Credential: &vault.ResolvedCredential{
			Username: "contextuser",
			Source:   vault.CredSourceContext,
		},
	}

	info, err := remoteExecHostInfo("nre-netlab01", resolved)
	if err != nil {
		t.Fatalf("expected key-capable host to be allowed, got error: %v", err)
	}
	if info.Target != "nre-netlab01" {
		t.Fatalf("target = %q, want %q", info.Target, "nre-netlab01")
	}
	if info.Hostname != "nre-netlab01.custcbb.local" {
		t.Fatalf("hostname = %q", info.Hostname)
	}
	if info.Username != "nre" {
		t.Fatalf("username = %q, want %q", info.Username, "nre")
	}
}

func TestRemoteExecHostInfoRejectsPasswordTransport(t *testing.T) {
	resolved := &resolve.ResolvedHost{
		Hostname: "router.example.com",
		Username: "admin",
		HostEntry: &sshconfig.HostEntry{
			Properties: map[string]string{
				"pubkeyauthentication":     "no",
				"preferredauthentications": "keyboard-interactive,password",
			},
		},
	}

	_, err := remoteExecHostInfo("router", resolved)
	if err == nil {
		t.Fatal("expected password-auth host to be rejected")
	}
	if !strings.Contains(err.Error(), "configured for password auth") {
		t.Fatalf("unexpected error: %v", err)
	}
}
