package sshconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindIncludeFiles(t *testing.T) {
	tmpDir := t.TempDir()
	sshDir := filepath.Join(tmpDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}

	// Create include files
	workHosts := filepath.Join(sshDir, "work_hosts")
	homeHosts := filepath.Join(sshDir, "homelab_hosts")

	for _, f := range []string{workHosts, homeHosts} {
		if err := os.WriteFile(f, []byte("# hosts file\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}

	// Create main config with includes (use actual paths for testing)
	configContent := `# Main SSH config
Include ` + workHosts + `
Include ` + homeHosts + `

Host *
  LogLevel ERROR
`

	configPath := filepath.Join(sshDir, "config")
	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatal(err)
	}

	parser := NewParserWithPaths(configPath, tmpDir, 5)
	includes, err := parser.FindIncludeFiles()
	if err != nil {
		t.Fatalf("FindIncludeFiles: %v", err)
	}

	if len(includes) != 2 {
		t.Fatalf("expected 2 includes, got %d: %v", len(includes), includes)
	}

	// Check paths
	found := make(map[string]bool)
	for _, inc := range includes {
		found[filepath.Base(inc)] = true
	}

	if !found["work_hosts"] {
		t.Error("missing work_hosts")
	}
	if !found["homelab_hosts"] {
		t.Error("missing homelab_hosts")
	}
}

func TestSplitIncludeTargets(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"~/.ssh/hosts", []string{"~/.ssh/hosts"}},
		{"~/.ssh/work ~/.ssh/home", []string{"~/.ssh/work", "~/.ssh/home"}},
		{`"~/.ssh/my hosts"`, []string{"~/.ssh/my hosts"}},
		{`'~/.ssh/other hosts' ~/.ssh/simple`, []string{"~/.ssh/other hosts", "~/.ssh/simple"}},
		{"   spaced   paths   ", []string{"spaced", "paths"}},
	}

	for _, tt := range tests {
		result := splitIncludeTargets(tt.input)
		if len(result) != len(tt.expected) {
			t.Errorf("splitIncludeTargets(%q): got %v, want %v", tt.input, result, tt.expected)
			continue
		}
		for i, v := range result {
			if v != tt.expected[i] {
				t.Errorf("splitIncludeTargets(%q)[%d]: got %q, want %q", tt.input, i, v, tt.expected[i])
			}
		}
	}
}

func TestGetAllHosts(t *testing.T) {
	tmpDir := t.TempDir()
	sshDir := filepath.Join(tmpDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}

	// Create include file with hosts
	workHosts := filepath.Join(sshDir, "work_hosts")
	workContent := `Host server
  HostName server.example.com
  User alice

Host router
  HostName router.example.com
  User admin
`
	if err := os.WriteFile(workHosts, []byte(workContent), 0600); err != nil {
		t.Fatal(err)
	}

	// Create main config
	configPath := filepath.Join(sshDir, "config")
	configContent := "Include " + workHosts + "\n"
	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatal(err)
	}

	parser := NewParserWithPaths(configPath, tmpDir, 5)
	hosts, err := parser.GetAllHosts()
	if err != nil {
		t.Fatalf("GetAllHosts: %v", err)
	}

	if len(hosts) != 2 {
		t.Fatalf("expected 2 hosts, got %d", len(hosts))
	}

	// Verify source file is set
	for _, h := range hosts {
		if h.SourceFile != workHosts {
			t.Errorf("host %s: expected source %s, got %s", h.Host, workHosts, h.SourceFile)
		}
	}
}

func TestFindHostWithLocation(t *testing.T) {
	tmpDir := t.TempDir()
	sshDir := filepath.Join(tmpDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}

	// Create include file
	hostsFile := filepath.Join(sshDir, "hosts")
	hostsContent := `Host server
  HostName server.example.com
  User alice
`
	if err := os.WriteFile(hostsFile, []byte(hostsContent), 0600); err != nil {
		t.Fatal(err)
	}

	// Create main config
	configPath := filepath.Join(sshDir, "config")
	configContent := "Include " + hostsFile + "\n"
	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatal(err)
	}

	parser := NewParserWithPaths(configPath, tmpDir, 5)

	// Find existing host
	host, cfg, err := parser.FindHostWithLocation("server")
	if err != nil {
		t.Fatalf("FindHostWithLocation: %v", err)
	}
	if host == nil {
		t.Fatal("host not found")
	}
	if cfg.Path != hostsFile {
		t.Errorf("expected path %s, got %s", hostsFile, cfg.Path)
	}

	// Find nonexistent host
	host, _, err = parser.FindHostWithLocation("nonexistent")
	if err != nil {
		t.Fatalf("FindHostWithLocation: %v", err)
	}
	if host != nil {
		t.Error("should not find nonexistent host")
	}
}
