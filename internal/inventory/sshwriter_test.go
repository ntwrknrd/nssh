package inventory

import (
	"strings"
	"testing"
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
	if strings.Contains(content, "nssh sync") || strings.Contains(content, "Source:") {
		t.Fatalf("provider output still uses sync/source wording: %q", content)
	}
	if !strings.Contains(content, "# Group: custcbb") {
		t.Fatalf("missing group comment: %q", content)
	}
	if !strings.Contains(content, "  User chris.jones\n") {
		t.Fatalf("missing user directive: %q", content)
	}
}
