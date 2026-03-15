package sync

import (
	"strings"
	"testing"
)

func TestGenerateSSHConfig(t *testing.T) {
	hosts := []*ManagedHost{
		{
			Host:         "clab-dfz-core01",
			Patterns:     []string{"clab-dfz-core01", "dfz-core01"},
			HostName:     "172.20.0.2",
			ProxyJump:    "nre-netlab01",
			UsesPassword: true,
		},
		{
			Host:     "clab-dfz-spine01",
			Patterns: []string{"clab-dfz-spine01"},
			HostName: "172.20.0.3",
			Port:     2222,
		},
	}

	content := generateSSHConfig(hosts, "nre-netlab01", "containerlab")

	// Check header
	if !strings.HasPrefix(content, sshConfigHeader) {
		t.Error("missing header")
	}
	if !strings.Contains(content, "# Source: nre-netlab01 (containerlab)") {
		t.Error("missing source comment")
	}

	// Check first host
	if !strings.Contains(content, "Host clab-dfz-core01 dfz-core01") {
		t.Error("missing host line with patterns")
	}
	if !strings.Contains(content, "HostName 172.20.0.2") {
		t.Error("missing HostName")
	}
	if !strings.Contains(content, "ProxyJump nre-netlab01") {
		t.Error("missing ProxyJump")
	}
	if !strings.Contains(content, "PubkeyAuthentication no") {
		t.Error("missing PubkeyAuthentication")
	}
	if !strings.Contains(content, "PreferredAuthentications keyboard-interactive,password") {
		t.Error("missing PreferredAuthentications")
	}

	// Check second host has port
	if !strings.Contains(content, "Port 2222") {
		t.Error("missing Port")
	}
}

func TestCollectIncludeFiles(t *testing.T) {
	hosts := []*ManagedHost{
		{IncludeFile: "conf.d/sync_b"},
		{IncludeFile: "conf.d/sync_a"},
		{IncludeFile: "conf.d/sync_b"}, // duplicate
	}

	files := CollectIncludeFiles(hosts)
	if len(files) != 2 {
		t.Fatalf("expected 2 unique files, got %d: %v", len(files), files)
	}
	if files[0] != "conf.d/sync_a" || files[1] != "conf.d/sync_b" {
		t.Errorf("files = %v", files)
	}
}

func TestGenerateSSHConfigDefaultPort(t *testing.T) {
	hosts := []*ManagedHost{
		{
			Host:     "test",
			Patterns: []string{"test"},
			HostName: "10.0.0.1",
			Port:     22, // default, should be omitted
		},
	}

	content := generateSSHConfig(hosts, "src", "test")
	if strings.Contains(content, "Port") {
		t.Error("default port 22 should not be written")
	}
}

func TestGenerateSSHConfigNoPasswordDirectives(t *testing.T) {
	hosts := []*ManagedHost{
		{
			Host:         "no-pw",
			Patterns:     []string{"no-pw"},
			HostName:     "10.0.0.1",
			UsesPassword: false,
		},
	}

	content := generateSSHConfig(hosts, "src", "test")
	if strings.Contains(content, "PubkeyAuthentication") {
		t.Error("should not have PubkeyAuthentication when UsesPassword is false")
	}
}
