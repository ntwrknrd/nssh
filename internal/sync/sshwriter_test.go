package sync

import (
	"strings"
	"testing"

	"github.com/ntwrknrd/nssh/internal/ssh/compat"
)

func TestGenerateSSHConfig(t *testing.T) {
	hosts := []*ManagedHost{
		{
			Host:         "clab-dfz-core01",
			Patterns:     []string{"clab-dfz-core01", "dfz-core01"},
			HostName:     "172.20.0.2",
			Username:     "admin",
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
	if !strings.Contains(content, "User admin") {
		t.Error("missing User")
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
			Username:     "operator",
			UsesPassword: false,
		},
	}

	content := generateSSHConfig(hosts, "src", "test")
	if !strings.Contains(content, "User operator") {
		t.Error("missing User")
	}
	if !strings.Contains(content, "PubkeyAuthentication yes") {
		t.Error("missing key auth directive")
	}
	if !strings.Contains(content, "PasswordAuthentication no") {
		t.Error("missing password disable directive")
	}
}

func TestGenerateSSHConfigCompatFixes(t *testing.T) {
	hosts := []*ManagedHost{
		{
			Host:        "clab-dfz-core01",
			Patterns:    []string{"clab-dfz-core01"},
			HostName:    "172.20.0.2",
			CompatFixes: []compat.CompatType{compat.CompatKex, compat.CompatHostKey},
		},
	}

	content := generateSSHConfig(hosts, "src", "test")
	if !strings.Contains(content, "KexAlgorithms +diffie-hellman-group1-sha1") {
		t.Error("missing compat KexAlgorithms directive")
	}
	if !strings.Contains(content, "HostKeyAlgorithms +ssh-rsa,ssh-dss") {
		t.Error("missing compat HostKeyAlgorithms directive")
	}
}
