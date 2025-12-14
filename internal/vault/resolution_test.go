package vault

import (
	"testing"

	"github.com/ntwrknrd/nssh/internal/secret"
)

func TestResolveCredentialNoDefaultFallsBackToContext(t *testing.T) {
	mgr, _ := setupTestManager(t)

	// Create context with credential
	if err := mgr.CreateContext("work", "work_hosts", "", nil); err != nil {
		t.Fatalf("CreateContext: %v", err)
	}
	pw := secret.NewFromString("contextpass")
	if err := mgr.AddContextCredential("work", "contextuser", pw, false); err != nil {
		t.Fatalf("AddContextCredential: %v", err)
	}

	// Add host-specific credential (no default set)
	hostPw := secret.NewFromString("hostpass")
	if err := mgr.AddHostCredential("router1", "hostuser", hostPw); err != nil {
		t.Fatalf("AddHostCredential: %v", err)
	}

	// Resolve without username - should fall back to context (no default set)
	cred, err := mgr.ResolveCredential("router1", "work_hosts", "")
	if err != nil {
		t.Fatalf("ResolveCredential: %v", err)
	}
	if cred == nil {
		t.Fatal("expected credential, got nil")
	}
	if cred.Username != "contextuser" {
		t.Errorf("username = %q, want %q", cred.Username, "contextuser")
	}
	if cred.Source != "context" {
		t.Errorf("source = %q, want %q", cred.Source, "context")
	}

	// Verify password
	var gotPw string
	_ = cred.Password.UseString(func(s string) error {
		gotPw = s
		return nil
	})
	if gotPw != "contextpass" {
		t.Errorf("password = %q, want %q", gotPw, "contextpass")
	}
}

func TestResolveCredentialContextFallback(t *testing.T) {
	mgr, _ := setupTestManager(t)

	// Create context with credential (no host-specific)
	if err := mgr.CreateContext("work", "work_hosts", "", nil); err != nil {
		t.Fatalf("CreateContext: %v", err)
	}
	pw := secret.NewFromString("contextpass")
	if err := mgr.AddContextCredential("work", "contextuser", pw, false); err != nil {
		t.Fatalf("AddContextCredential: %v", err)
	}

	// Resolve without host credential - should fall back to context
	cred, err := mgr.ResolveCredential("router1", "work_hosts", "")
	if err != nil {
		t.Fatalf("ResolveCredential: %v", err)
	}
	if cred == nil {
		t.Fatal("expected credential, got nil")
	}
	if cred.Username != "contextuser" {
		t.Errorf("username = %q, want %q", cred.Username, "contextuser")
	}
	if cred.Source != "context" {
		t.Errorf("source = %q, want %q", cred.Source, "context")
	}
}

func TestResolveCredentialWithUsername(t *testing.T) {
	mgr, _ := setupTestManager(t)

	// Create context with credential
	if err := mgr.CreateContext("work", "work_hosts", "", nil); err != nil {
		t.Fatalf("CreateContext: %v", err)
	}
	pw := secret.NewFromString("contextpass")
	if err := mgr.AddContextCredential("work", "contextuser", pw, false); err != nil {
		t.Fatalf("AddContextCredential: %v", err)
	}

	// Add multiple host credentials
	hostPw1 := secret.NewFromString("hostpass1")
	if err := mgr.AddHostCredential("router1", "admin", hostPw1); err != nil {
		t.Fatalf("AddHostCredential: %v", err)
	}
	hostPw2 := secret.NewFromString("hostpass2")
	if err := mgr.AddHostCredential("router1", "backup", hostPw2); err != nil {
		t.Fatalf("AddHostCredential: %v", err)
	}

	// Resolve with specific username
	cred, err := mgr.ResolveCredential("router1", "work_hosts", "backup")
	if err != nil {
		t.Fatalf("ResolveCredential: %v", err)
	}
	if cred == nil {
		t.Fatal("expected credential, got nil")
	}
	if cred.Username != "backup" {
		t.Errorf("username = %q, want %q", cred.Username, "backup")
	}

	// Resolve with context username
	cred, err = mgr.ResolveCredential("router1", "work_hosts", "contextuser")
	if err != nil {
		t.Fatalf("ResolveCredential (context user): %v", err)
	}
	if cred == nil {
		t.Fatal("expected credential, got nil")
	}
	if cred.Username != "contextuser" {
		t.Errorf("username = %q, want %q", cred.Username, "contextuser")
	}
	if cred.Source != "context" {
		t.Errorf("source = %q, want %q", cred.Source, "context")
	}

	// Resolve with non-existent username
	cred, err = mgr.ResolveCredential("router1", "work_hosts", "nonexistent")
	if err != nil {
		t.Fatalf("ResolveCredential (nonexistent): %v", err)
	}
	if cred != nil {
		t.Error("expected nil for non-existent username")
	}
}

func TestResolveCredentialNoMatch(t *testing.T) {
	mgr, _ := setupTestManager(t)

	// No context, no host credentials
	cred, err := mgr.ResolveCredential("router1", "", "")
	if err != nil {
		t.Fatalf("ResolveCredential: %v", err)
	}
	if cred != nil {
		t.Error("expected nil when no credentials configured")
	}
}

func TestMatchesDomain(t *testing.T) {
	tests := []struct {
		hostname string
		domain   string
		want     bool
	}{
		{"server.example.com", "example.com", true},
		{"server.example.com", "server.example.com", true},
		{"deep.sub.example.com", "example.com", true},
		{"example.com", "example.com", true},
		{"notexample.com", "example.com", false},
		{"example.com.other.org", "example.com", false},
		{"server.other.com", "example.com", false},
		{"server", "example.com", false},
		{"server.example.com", "", false},
	}

	for _, tt := range tests {
		got := matchesDomain(tt.hostname, tt.domain)
		if got != tt.want {
			t.Errorf("matchesDomain(%q, %q) = %v, want %v",
				tt.hostname, tt.domain, got, tt.want)
		}
	}
}

func TestResolveCredentialWithDomain(t *testing.T) {
	mgr, _ := setupTestManager(t)

	// Create context with domain
	if err := mgr.CreateContext("work", "work_hosts", "example.com", nil); err != nil {
		t.Fatalf("CreateContext: %v", err)
	}
	pw := secret.NewFromString("domainpass")
	if err := mgr.AddContextCredential("work", "domainuser", pw, false); err != nil {
		t.Fatalf("AddContextCredential: %v", err)
	}

	// Resolve by domain match
	cred, err := mgr.ResolveCredentialWithDomain("server.example.com", "")
	if err != nil {
		t.Fatalf("ResolveCredentialWithDomain: %v", err)
	}
	if cred == nil {
		t.Fatal("expected credential, got nil")
	}
	if cred.Username != "domainuser" {
		t.Errorf("username = %q, want %q", cred.Username, "domainuser")
	}
	if cred.Source != "context" {
		t.Errorf("source = %q, want %q", cred.Source, "context")
	}

	// Non-matching domain
	cred, err = mgr.ResolveCredentialWithDomain("server.other.com", "")
	if err != nil {
		t.Fatalf("ResolveCredentialWithDomain (no match): %v", err)
	}
	if cred != nil {
		t.Error("expected nil for non-matching domain")
	}
}

func TestResolveCredentialWithDomainNoDefaultFallsBackToContext(t *testing.T) {
	mgr, _ := setupTestManager(t)

	// Create context with domain
	if err := mgr.CreateContext("work", "work_hosts", "example.com", nil); err != nil {
		t.Fatalf("CreateContext: %v", err)
	}
	pw := secret.NewFromString("domainpass")
	if err := mgr.AddContextCredential("work", "domainuser", pw, false); err != nil {
		t.Fatalf("AddContextCredential: %v", err)
	}

	// Add host-specific credential (no default set)
	hostPw := secret.NewFromString("hostpass")
	if err := mgr.AddHostCredential("server.example.com", "hostuser", hostPw); err != nil {
		t.Fatalf("AddHostCredential: %v", err)
	}

	// Without default set, should fall back to domain context
	cred, err := mgr.ResolveCredentialWithDomain("server.example.com", "")
	if err != nil {
		t.Fatalf("ResolveCredentialWithDomain: %v", err)
	}
	if cred == nil {
		t.Fatal("expected credential, got nil")
	}
	if cred.Username != "domainuser" {
		t.Errorf("username = %q, want %q", cred.Username, "domainuser")
	}
	if cred.Source != "context" {
		t.Errorf("source = %q, want %q", cred.Source, "context")
	}
}

func TestResolveCredentialWithDefault(t *testing.T) {
	mgr, _ := setupTestManager(t)

	// Create context with credential
	if err := mgr.CreateContext("work", "work_hosts", "", nil); err != nil {
		t.Fatalf("CreateContext: %v", err)
	}
	pw := secret.NewFromString("contextpass")
	if err := mgr.AddContextCredential("work", "contextuser", pw, false); err != nil {
		t.Fatalf("AddContextCredential: %v", err)
	}

	// Add multiple host-specific credentials
	hostPw1 := secret.NewFromString("adminpass")
	if err := mgr.AddHostCredential("router1", "admin", hostPw1); err != nil {
		t.Fatalf("AddHostCredential: %v", err)
	}
	hostPw2 := secret.NewFromString("backuppass")
	if err := mgr.AddHostCredential("router1", "backup", hostPw2); err != nil {
		t.Fatalf("AddHostCredential: %v", err)
	}

	// Set default to backup
	if err := mgr.SetHostDefaultCredential("router1", "backup"); err != nil {
		t.Fatalf("SetHostDefaultCredential: %v", err)
	}

	// Resolve without username - should use default (backup)
	cred, err := mgr.ResolveCredential("router1", "work_hosts", "")
	if err != nil {
		t.Fatalf("ResolveCredential: %v", err)
	}
	if cred == nil {
		t.Fatal("expected credential, got nil")
	}
	if cred.Username != "backup" {
		t.Errorf("username = %q, want %q", cred.Username, "backup")
	}
	if cred.Source != "host" {
		t.Errorf("source = %q, want %q", cred.Source, "host")
	}

	// Verify password
	var gotPw string
	_ = cred.Password.UseString(func(s string) error {
		gotPw = s
		return nil
	})
	if gotPw != "backuppass" {
		t.Errorf("password = %q, want %q", gotPw, "backuppass")
	}
}

func TestResolveCredentialDefaultNotFound(t *testing.T) {
	mgr, _ := setupTestManager(t)

	// Create context with credential
	if err := mgr.CreateContext("work", "work_hosts", "", nil); err != nil {
		t.Fatalf("CreateContext: %v", err)
	}
	pw := secret.NewFromString("contextpass")
	if err := mgr.AddContextCredential("work", "contextuser", pw, false); err != nil {
		t.Fatalf("AddContextCredential: %v", err)
	}

	// Add host-specific credential and set default
	hostPw := secret.NewFromString("adminpass")
	if err := mgr.AddHostCredential("router1", "admin", hostPw); err != nil {
		t.Fatalf("AddHostCredential: %v", err)
	}
	if err := mgr.SetHostDefaultCredential("router1", "admin"); err != nil {
		t.Fatalf("SetHostDefaultCredential: %v", err)
	}

	// Remove the credential (simulating a stale default)
	if _, err := mgr.RemoveHostCredential("router1", "admin"); err != nil {
		t.Fatalf("RemoveHostCredential: %v", err)
	}

	// Re-add a different credential
	hostPw2 := secret.NewFromString("backuppass")
	if err := mgr.AddHostCredential("router1", "backup", hostPw2); err != nil {
		t.Fatalf("AddHostCredential: %v", err)
	}

	// Resolve without username - default "admin" not found, should fall back to context
	cred, err := mgr.ResolveCredential("router1", "work_hosts", "")
	if err != nil {
		t.Fatalf("ResolveCredential: %v", err)
	}
	if cred == nil {
		t.Fatal("expected credential, got nil")
	}
	if cred.Username != "contextuser" {
		t.Errorf("username = %q, want %q", cred.Username, "contextuser")
	}
	if cred.Source != "context" {
		t.Errorf("source = %q, want %q", cred.Source, "context")
	}
}

func TestSetHostDefaultCredential(t *testing.T) {
	mgr, _ := setupTestManager(t)

	// Add host credential
	hostPw := secret.NewFromString("adminpass")
	if err := mgr.AddHostCredential("router1", "admin", hostPw); err != nil {
		t.Fatalf("AddHostCredential: %v", err)
	}

	// Set default
	if err := mgr.SetHostDefaultCredential("router1", "admin"); err != nil {
		t.Fatalf("SetHostDefaultCredential: %v", err)
	}

	// Get default
	defaultUser, err := mgr.GetHostDefaultCredential("router1")
	if err != nil {
		t.Fatalf("GetHostDefaultCredential: %v", err)
	}
	if defaultUser != "admin" {
		t.Errorf("default = %q, want %q", defaultUser, "admin")
	}

	// Clear default
	if err := mgr.SetHostDefaultCredential("router1", ""); err != nil {
		t.Fatalf("SetHostDefaultCredential (clear): %v", err)
	}

	// Get default should be empty
	defaultUser, err = mgr.GetHostDefaultCredential("router1")
	if err != nil {
		t.Fatalf("GetHostDefaultCredential: %v", err)
	}
	if defaultUser != "" {
		t.Errorf("default = %q, want empty", defaultUser)
	}
}

func TestSetHostDefaultCredentialErrors(t *testing.T) {
	mgr, _ := setupTestManager(t)

	// Try to set default for non-existent host
	err := mgr.SetHostDefaultCredential("nonexistent", "admin")
	if err == nil {
		t.Error("expected error for non-existent host")
	}

	// Add host credential
	hostPw := secret.NewFromString("adminpass")
	if err := mgr.AddHostCredential("router1", "admin", hostPw); err != nil {
		t.Fatalf("AddHostCredential: %v", err)
	}

	// Try to set default to non-existent username
	err = mgr.SetHostDefaultCredential("router1", "nonexistent")
	if err == nil {
		t.Error("expected error for non-existent username")
	}
}

func TestResolveCredentialWithDomainAndDefault(t *testing.T) {
	mgr, _ := setupTestManager(t)

	// Create context with domain
	if err := mgr.CreateContext("work", "work_hosts", "example.com", nil); err != nil {
		t.Fatalf("CreateContext: %v", err)
	}
	pw := secret.NewFromString("domainpass")
	if err := mgr.AddContextCredential("work", "domainuser", pw, false); err != nil {
		t.Fatalf("AddContextCredential: %v", err)
	}

	// Add host-specific credentials
	hostPw1 := secret.NewFromString("adminpass")
	if err := mgr.AddHostCredential("server.example.com", "admin", hostPw1); err != nil {
		t.Fatalf("AddHostCredential: %v", err)
	}
	hostPw2 := secret.NewFromString("backuppass")
	if err := mgr.AddHostCredential("server.example.com", "backup", hostPw2); err != nil {
		t.Fatalf("AddHostCredential: %v", err)
	}

	// Set default
	if err := mgr.SetHostDefaultCredential("server.example.com", "backup"); err != nil {
		t.Fatalf("SetHostDefaultCredential: %v", err)
	}

	// Resolve - should use host default
	cred, err := mgr.ResolveCredentialWithDomain("server.example.com", "")
	if err != nil {
		t.Fatalf("ResolveCredentialWithDomain: %v", err)
	}
	if cred == nil {
		t.Fatal("expected credential, got nil")
	}
	if cred.Username != "backup" {
		t.Errorf("username = %q, want %q", cred.Username, "backup")
	}
	if cred.Source != "host" {
		t.Errorf("source = %q, want %q", cred.Source, "host")
	}
}
