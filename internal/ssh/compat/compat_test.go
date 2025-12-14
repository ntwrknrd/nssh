package compat

import (
	"testing"
)

func TestParseCompatibilityError(t *testing.T) {
	tests := []struct {
		name     string
		stderr   string
		expected []CompatType
	}{
		{
			name:     "no matching key exchange",
			stderr:   "Unable to negotiate with 192.168.1.1 port 22: no matching key exchange method found.",
			expected: []CompatType{CompatKex},
		},
		{
			name:     "no matching cipher",
			stderr:   "Unable to negotiate with 10.0.0.1 port 22: no matching cipher found.",
			expected: []CompatType{CompatCiphers},
		},
		{
			name:     "no matching mac",
			stderr:   "Unable to negotiate with host: no matching mac found.",
			expected: []CompatType{CompatMACs},
		},
		{
			name:     "no matching host key type",
			stderr:   "Unable to negotiate with 192.168.1.1: no matching host key type found. Their offer: ssh-rsa,ssh-dss",
			expected: []CompatType{CompatHostKey},
		},
		{
			name:     "multiple issues",
			stderr:   "no matching key exchange method found. Also no matching cipher found.",
			expected: []CompatType{CompatKex, CompatCiphers},
		},
		{
			name:     "no compat issues",
			stderr:   "Permission denied (publickey,password).",
			expected: nil,
		},
		{
			name:     "empty stderr",
			stderr:   "",
			expected: nil,
		},
		{
			name:     "case insensitive kex",
			stderr:   "NO MATCHING KEY EXCHANGE METHOD FOUND",
			expected: []CompatType{CompatKex},
		},
		{
			name:     "unable to negotiate format",
			stderr:   "unable to negotiate encryption: no matching cipher",
			expected: []CompatType{CompatCiphers},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseCompatibilityError(tt.stderr)
			if len(result) != len(tt.expected) {
				t.Errorf("ParseCompatibilityError() = %v, want %v", result, tt.expected)
				return
			}
			for i, ct := range result {
				if ct != tt.expected[i] {
					t.Errorf("ParseCompatibilityError()[%d] = %v, want %v", i, ct, tt.expected[i])
				}
			}
		})
	}
}

func TestIsAuthFailureAfterKex(t *testing.T) {
	tests := []struct {
		name     string
		stderr   string
		expected bool
	}{
		{
			name: "kex success then auth failure",
			stderr: `debug1: SSH2_MSG_KEX_DH_GEX_REQUEST sent
debug1: kex: algorithm: curve25519-sha256
debug1: kex: host key algorithm: ssh-ed25519
Permission denied (publickey,password).`,
			expected: true,
		},
		{
			name: "kex success then no more auth methods",
			stderr: `debug1: kex: algorithm: diffie-hellman-group14-sha256
No more authentication methods to try.`,
			expected: true,
		},
		{
			name:     "kex failure - no algorithm line",
			stderr:   "no matching key exchange method found",
			expected: false,
		},
		{
			name: "kex success - no auth failure",
			stderr: `debug1: kex: algorithm: curve25519-sha256
Authenticated to host using "password".`,
			expected: false,
		},
		{
			name:     "empty",
			stderr:   "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsAuthFailureAfterKex(tt.stderr)
			if result != tt.expected {
				t.Errorf("IsAuthFailureAfterKex() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestDidAuthSucceed(t *testing.T) {
	tests := []struct {
		name     string
		stderr   string
		expected bool
	}{
		{
			name:     "authenticated with password",
			stderr:   `Authenticated to server.example.com using "password".`,
			expected: true,
		},
		{
			name:     "authenticated via proxy",
			stderr:   `Authenticated to host (via proxy) using "publickey".`,
			expected: true,
		},
		{
			name:     "authenticated keyboard-interactive",
			stderr:   `Authenticated to 192.168.1.1 using "keyboard-interactive".`,
			expected: true,
		},
		{
			name:     "permission denied",
			stderr:   "Permission denied (publickey,password).",
			expected: false,
		},
		{
			name:     "empty",
			stderr:   "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DidAuthSucceed(tt.stderr)
			if result != tt.expected {
				t.Errorf("DidAuthSucceed() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestExtractAuthMethod(t *testing.T) {
	tests := []struct {
		name     string
		stderr   string
		expected string
	}{
		{
			name:     "password auth",
			stderr:   `Authenticated to server.example.com using "password".`,
			expected: "password",
		},
		{
			name:     "publickey auth",
			stderr:   `Authenticated to host using "publickey".`,
			expected: "publickey",
		},
		{
			name:     "keyboard-interactive",
			stderr:   `Authenticated to 10.0.0.1 using "keyboard-interactive".`,
			expected: "keyboard-interactive",
		},
		{
			name:     "via proxy",
			stderr:   `Authenticated to host (via jump-server) using "password".`,
			expected: "password",
		},
		{
			name:     "no auth method",
			stderr:   "Permission denied",
			expected: "",
		},
		{
			name:     "empty",
			stderr:   "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractAuthMethod(tt.stderr)
			if result != tt.expected {
				t.Errorf("ExtractAuthMethod() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestAllCompatTypes(t *testing.T) {
	types := AllCompatTypes()
	if len(types) != 4 {
		t.Errorf("AllCompatTypes() returned %d types, want 4", len(types))
	}

	expected := []CompatType{CompatKex, CompatMACs, CompatCiphers, CompatHostKey}
	for i, ct := range types {
		if ct != expected[i] {
			t.Errorf("AllCompatTypes()[%d] = %v, want %v", i, ct, expected[i])
		}
	}
}

func TestCompatConfigs(t *testing.T) {
	// Verify all compat types have valid configs
	for _, ct := range AllCompatTypes() {
		cfg, ok := CompatConfigs[ct]
		if !ok {
			t.Errorf("CompatConfigs missing entry for %v", ct)
			continue
		}

		if cfg.Name == "" {
			t.Errorf("CompatConfigs[%v].Name is empty", ct)
		}
		if cfg.Directive == "" {
			t.Errorf("CompatConfigs[%v].Directive is empty", ct)
		}
		if len(cfg.ConfigLines) == 0 {
			t.Errorf("CompatConfigs[%v].ConfigLines is empty", ct)
		}
		if len(cfg.ErrorPatterns) == 0 {
			t.Errorf("CompatConfigs[%v].ErrorPatterns is empty", ct)
		}
	}
}
