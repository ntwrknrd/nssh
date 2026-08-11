package config

import "testing"

func TestDefaultCredentialRef(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		target   string
		group    bool
		want     string
	}{
		{name: "sops host", provider: "sops", target: "edge01", want: "hosts.edge01.password"},
		{name: "sops group", provider: "sops", target: "local/default", group: true, want: "groups.local.default.password"},
		{name: "1password host", provider: "op-expedient", target: "edge01", want: "nssh host edge01"},
		{name: "1password group", provider: "op-expedient", target: "netbox-prod/custcbb", group: true, want: "nssh group netbox-prod/custcbb"},
		{name: "bitwarden host", provider: "bw-lab", target: "edge01", want: "nssh host edge01"},
		{name: "bitwarden group", provider: "bw-lab", target: "lab01/lab", group: true, want: "nssh group lab01/lab"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DefaultCredentialRef(tt.provider, tt.target, tt.group); got != tt.want {
				t.Fatalf("DefaultCredentialRef(%q, %q, %t) = %q, want %q", tt.provider, tt.target, tt.group, got, tt.want)
			}
		})
	}
}
