package sshconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseFile(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()

	// Create a test SSH config file
	configContent := `# Test SSH config
Host *
  LogLevel ERROR

Host server
  HostName server.example.com
  User alice
  Port 22
  PubkeyAuthentication yes

Host router
  HostName router.example.com
  User admin
  Port 2222
  PubkeyAuthentication no
  PreferredAuthentications keyboard-interactive

Host switch legacy-switch
  HostName switch.example.com
  User netadmin
`

	configPath := filepath.Join(tmpDir, "config")
	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	parser := NewParserWithPaths(configPath, tmpDir, 5)
	cfg, err := parser.ParseFile(configPath)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	// Check header lines
	if len(cfg.HeaderLines) < 3 {
		t.Errorf("expected at least 3 header lines, got %d", len(cfg.HeaderLines))
	}

	// Check hosts
	if len(cfg.Hosts) != 3 {
		t.Fatalf("expected 3 hosts, got %d", len(cfg.Hosts))
	}

	// Check first host
	server := cfg.Hosts[0]
	if server.Host != "server" {
		t.Errorf("expected host 'server', got %q", server.Host)
	}
	if server.HostName != "server.example.com" {
		t.Errorf("expected hostname 'server.example.com', got %q", server.HostName)
	}
	if server.User() != "alice" {
		t.Errorf("expected user 'alice', got %q", server.User())
	}
	if server.UsesPassword() {
		t.Error("server should use key auth")
	}

	// Check router
	router := cfg.Hosts[1]
	if router.Host != "router" {
		t.Errorf("expected host 'router', got %q", router.Host)
	}
	if router.Port() != "2222" {
		t.Errorf("expected port '2222', got %q", router.Port())
	}
	if !router.UsesPassword() {
		t.Error("router should use password auth")
	}

	// Check switch with multiple patterns
	switchHost := cfg.Hosts[2]
	if len(switchHost.Patterns) != 2 {
		t.Errorf("expected 2 patterns, got %d", len(switchHost.Patterns))
	}
	if switchHost.Patterns[0] != "switch" || switchHost.Patterns[1] != "legacy-switch" {
		t.Errorf("unexpected patterns: %v", switchHost.Patterns)
	}
}

func TestFindHostByPattern(t *testing.T) {
	hosts := []*HostEntry{
		{Host: "server", Patterns: []string{"server", "srv"}},
		{Host: "router", Patterns: []string{"router"}},
	}

	// Test primary pattern
	if h := FindHostByPattern(hosts, "server"); h == nil || h.Host != "server" {
		t.Error("failed to find by primary pattern")
	}

	// Test secondary pattern
	if h := FindHostByPattern(hosts, "srv"); h == nil || h.Host != "server" {
		t.Error("failed to find by secondary pattern")
	}

	// Test case insensitive
	if h := FindHostByPattern(hosts, "SERVER"); h == nil || h.Host != "server" {
		t.Error("failed case-insensitive match")
	}

	// Test not found
	if h := FindHostByPattern(hosts, "nonexistent"); h != nil {
		t.Error("should not find nonexistent host")
	}
}

func TestFindInsertionIndex(t *testing.T) {
	hosts := []*HostEntry{
		{Host: "alpha"},
		{Host: "charlie"},
		{Host: "delta"},
	}

	tests := []struct {
		newHost  string
		expected int
	}{
		{"aaa", 0},     // Before alpha
		{"bravo", 1},   // Between alpha and charlie
		{"echo", 3},    // After delta
		{"Alpha", 1},   // Case insensitive, after alpha
		{"CHARLIE", 2}, // Case insensitive, after charlie
	}

	for _, tt := range tests {
		idx := FindInsertionIndex(hosts, tt.newHost)
		if idx != tt.expected {
			t.Errorf("FindInsertionIndex(%q) = %d, want %d", tt.newHost, idx, tt.expected)
		}
	}
}

func TestSortHosts(t *testing.T) {
	hosts := []*HostEntry{
		{Host: "charlie"},
		{Host: "Alpha"},
		{Host: "BRAVO"},
		{Host: "delta"},
	}

	SortHosts(hosts)

	expected := []string{"Alpha", "BRAVO", "charlie", "delta"}
	for i, h := range hosts {
		if h.Host != expected[i] {
			t.Errorf("position %d: got %q, want %q", i, h.Host, expected[i])
		}
	}
}

func TestRemoveHost(t *testing.T) {
	hosts := []*HostEntry{
		{Host: "server", Patterns: []string{"server", "srv"}},
		{Host: "router", Patterns: []string{"router"}},
		{Host: "switch", Patterns: []string{"switch"}},
	}

	// Remove by primary pattern
	result := RemoveHost(hosts, "router")
	if len(result) != 2 {
		t.Errorf("expected 2 hosts after removal, got %d", len(result))
	}

	// Remove by secondary pattern
	hosts = []*HostEntry{
		{Host: "server", Patterns: []string{"server", "srv"}},
		{Host: "router", Patterns: []string{"router"}},
	}
	result = RemoveHost(hosts, "srv")
	if len(result) != 1 {
		t.Errorf("expected 1 host after removal by secondary pattern, got %d", len(result))
	}
}

func TestCreateHostEntry(t *testing.T) {
	host := CreateHostEntry("server.example.com", "", "alice", 22, false, "/tmp/hosts")

	if host.Host != "server.example.com" {
		t.Errorf("unexpected host: %q", host.Host)
	}
	if host.HostName != "server.example.com" {
		t.Errorf("unexpected hostname: %q", host.HostName)
	}
	if host.User() != "alice" {
		t.Errorf("unexpected user: %q", host.User())
	}
	if host.UsesPassword() {
		t.Error("should use key auth")
	}

	// Check lines contain expected content (HostName always present, defaults to Host)
	joined := strings.Join(host.Lines, "")
	if !strings.Contains(joined, "Host server.example.com") {
		t.Error("missing Host directive")
	}
	if !strings.Contains(joined, "HostName server.example.com") {
		t.Error("missing HostName directive (should default to Host value)")
	}
	if !strings.Contains(joined, "PubkeyAuthentication yes") {
		t.Error("missing PubkeyAuthentication")
	}

	// Test password auth
	passHost := CreateHostEntry("router.example.com", "", "admin", 2222, true, "/tmp/hosts")
	if !passHost.UsesPassword() {
		t.Error("should use password auth")
	}
	if passHost.Port() != "2222" {
		t.Errorf("unexpected port: %q", passHost.Port())
	}

	// Test with address (IP) different from hostname (when DNS unavailable)
	ipHost := CreateHostEntry("switch.example.com", "192.168.1.10", "admin", 22, true, "/tmp/hosts")
	ipJoined := strings.Join(ipHost.Lines, "")
	if !strings.Contains(ipJoined, "Host switch.example.com") {
		t.Error("missing Host directive for IP host")
	}
	if !strings.Contains(ipJoined, "HostName 192.168.1.10") {
		t.Error("missing HostName with IP address")
	}
	if ipHost.HostName != "192.168.1.10" {
		t.Errorf("expected HostName to return IP, got %q", ipHost.HostName)
	}
}

func TestWriteFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config")

	parser := NewParserWithPaths(configPath, tmpDir, 5)

	cfg := &ParsedConfig{
		Path: configPath,
		HeaderLines: []string{
			"# Test config\n",
			"\n",
		},
		Hosts: []*HostEntry{
			CreateHostEntry("server.example.com", "", "alice", 22, false, configPath),
		},
	}

	if err := parser.WriteFile(cfg); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Read back and verify
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if !strings.Contains(string(content), "Host server.example.com") {
		t.Error("missing Host directive in written file")
	}
	if !strings.Contains(string(content), "HostName server.example.com") {
		t.Error("missing HostName directive (should default to Host value)")
	}

	// Check permissions
	info, _ := os.Stat(configPath)
	if info.Mode().Perm() != 0600 {
		t.Errorf("unexpected permissions: %o", info.Mode().Perm())
	}
}
