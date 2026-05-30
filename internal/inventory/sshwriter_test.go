package inventory

import (
	"strings"
	"testing"

	"github.com/ntwrknrd/nssh/internal/config"
)

func TestGenerateProviderSSHConfigUsesInventoryHeader(t *testing.T) {
	content := generateProviderSSHConfig([]*ProviderHost{{
		Host:     "edge01",
		Patterns: []string{"edge01"},
		Group:    "custcbb",
		HostName: "edge01.custcbb.local",
		Username: "chris.jones",
	}}, "netbox-prod", "netbox", true)

	if !strings.HasPrefix(content, providerSSHConfigHeader) {
		t.Fatalf("missing provider header: %q", content)
	}
	if !strings.Contains(content, "# Provider: netbox-prod (netbox)") {
		t.Fatalf("missing provider comment: %q", content)
	}
	if !strings.Contains(content, "# Group: custcbb") {
		t.Fatalf("missing group comment: %q", content)
	}
	if !strings.Contains(content, "  User chris.jones\n") {
		t.Fatalf("missing user directive: %q", content)
	}
}

func TestGenerateProviderSSHConfigRendersPasswordAuthMode(t *testing.T) {
	content := generateProviderSSHConfig([]*ProviderHost{{
		Host:     "edge01",
		Patterns: []string{"edge01"},
		Group:    "network",
		HostName: "edge01.example.com",
		AuthMode: config.AuthModePassword,
	}}, "netbox-prod", "netbox", true)

	if !strings.Contains(content, "  PubkeyAuthentication no\n") {
		t.Fatalf("missing password auth pubkey override:\n%s", content)
	}
	if !strings.Contains(content, "  PreferredAuthentications keyboard-interactive,password\n") {
		t.Fatalf("missing preferred password auth directive:\n%s", content)
	}
	if strings.Contains(content, "  PasswordAuthentication no\n") {
		t.Fatalf("password auth mode rendered key-only directive:\n%s", content)
	}
}

func TestGenerateProviderSSHConfigRendersKeyAuthMode(t *testing.T) {
	content := generateProviderSSHConfig([]*ProviderHost{{
		Host:     "app01",
		Patterns: []string{"app01"},
		Group:    "servers",
		HostName: "app01.example.com",
		AuthMode: config.AuthModeKey,
	}}, "netbox-prod", "netbox", true)

	if !strings.Contains(content, "  PubkeyAuthentication yes\n") {
		t.Fatalf("missing key auth pubkey directive:\n%s", content)
	}
	if !strings.Contains(content, "  PasswordAuthentication no\n") {
		t.Fatalf("missing key auth password directive:\n%s", content)
	}
	if strings.Contains(content, "  PreferredAuthentications keyboard-interactive,password\n") {
		t.Fatalf("key auth mode rendered password directive:\n%s", content)
	}
}
