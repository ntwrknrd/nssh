package connector

import "testing"

func TestMatchPasswordPrompt(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"simple password:", "Password: ", true},
		{"password lowercase", "password: ", true},
		{"password uppercase", "PASSWORD: ", true},
		{"passcode", "Passcode: ", true},
		{"user at host", "user@host's password: ", true},
		{"password for user", "Password for admin: ", true},
		{"enter passphrase", "Enter passphrase for key '/home/user/.ssh/id_rsa': ", true},
		{"password required", "Password required", true},
		{"not a prompt", "Welcome to the server", false},
		{"password in middle", "Your password has expired", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchPasswordPrompt([]byte(tt.input))
			if got != tt.want {
				t.Errorf("matchPasswordPrompt(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestMatchAuthFailure(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"permission denied", "Permission denied", true},
		{"permission denied lowercase", "permission denied", true},
		{"auth failed", "Authentication failed.", true},
		{"try again", "Please try again", true},
		{"access denied", "Access denied", true},
		{"success message", "Welcome to Ubuntu", false},
		{"password prompt", "Password: ", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchAuthFailure([]byte(tt.input))
			if got != tt.want {
				t.Errorf("matchAuthFailure(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestMatchUnknownHost(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{
			"standard prompt",
			"The authenticity of host 'example.com (192.168.1.1)' can't be established.\nAre you sure you want to continue connecting (yes/no/[fingerprint])?",
			true,
		},
		{
			"partial prompt",
			"Are you sure you want to continue connecting",
			true,
		},
		{"not a prompt", "Connected to server", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchUnknownHost([]byte(tt.input))
			if got != tt.want {
				t.Errorf("matchUnknownHost(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestMatchHostKeyChanged(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{
			"standard warning",
			"@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@\n@ WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED! @",
			true,
		},
		{
			"lowercase",
			"warning: remote host identification has changed!",
			true,
		},
		{"not a warning", "Host key verification failed", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchHostKeyChanged([]byte(tt.input))
			if got != tt.want {
				t.Errorf("matchHostKeyChanged(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestExtractFingerprint(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantType string
		wantFP   string
	}{
		{
			"ED25519",
			"ED25519 key fingerprint is SHA256:abcdefghijklmnopqrstuvwxyz123456789ABCDEF.",
			"ED25519",
			"SHA256:abcdefghijklmnopqrstuvwxyz123456789ABCDEF",
		},
		{
			"RSA",
			"RSA key fingerprint is SHA256:XYZ123abc456def789/+ABCDEFghijklmnop=.",
			"RSA",
			"SHA256:XYZ123abc456def789/+ABCDEFghijklmnop=",
		},
		{
			"ECDSA",
			"ECDSA key fingerprint is SHA256:test1234567890abcdefghijklmnopqrstuv",
			"ECDSA",
			"SHA256:test1234567890abcdefghijklmnopqrstuv",
		},
		{
			"no fingerprint",
			"Some other text without fingerprint",
			"",
			"",
		},
		{
			"empty",
			"",
			"",
			"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotType, gotFP := extractFingerprint([]byte(tt.input))
			if gotType != tt.wantType {
				t.Errorf("extractFingerprint() keyType = %q, want %q", gotType, tt.wantType)
			}
			if gotFP != tt.wantFP {
				t.Errorf("extractFingerprint() fingerprint = %q, want %q", gotFP, tt.wantFP)
			}
		})
	}
}

func BenchmarkMatchPasswordPrompt(b *testing.B) {
	prompt := []byte("user@host's password: ")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		matchPasswordPrompt(prompt)
	}
}
