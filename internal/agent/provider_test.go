//go:build linux || darwin

package agent

import (
	"bytes"
	"testing"
)

func TestSoftwareProvider_Mode(t *testing.T) {
	identity := testIdentity(t)
	provider := NewSoftwareProvider(identity)

	if got := provider.Mode(); got != ModeSoftware {
		t.Errorf("Mode() = %q, want %q", got, ModeSoftware)
	}
}

func TestSoftwareProvider_Decrypt(t *testing.T) {
	identity := testIdentity(t)
	provider := NewSoftwareProvider(identity)

	plaintext := []byte("hello, world!")
	ciphertext := encryptTestData(t, identity.Recipient(), plaintext)

	got, err := provider.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}

	if !bytes.Equal(got, plaintext) {
		t.Errorf("Decrypt() = %q, want %q", got, plaintext)
	}
}

func TestSoftwareProvider_DecryptInvalid(t *testing.T) {
	identity := testIdentity(t)
	provider := NewSoftwareProvider(identity)

	// Try to decrypt garbage data
	_, err := provider.Decrypt([]byte("not valid ciphertext"))
	if err == nil {
		t.Error("Decrypt() expected error for invalid ciphertext, got nil")
	}
}

func TestSoftwareProvider_DecryptWrongKey(t *testing.T) {
	identity1 := testIdentity(t)
	identity2 := testIdentity(t)

	// Encrypt with identity1
	plaintext := []byte("secret message")
	ciphertext := encryptTestData(t, identity1.Recipient(), plaintext)

	// Try to decrypt with identity2
	provider := NewSoftwareProvider(identity2)
	_, err := provider.Decrypt(ciphertext)
	if err == nil {
		t.Error("Decrypt() expected error for wrong key, got nil")
	}
}

func TestSoftwareProvider_Recipient(t *testing.T) {
	identity := testIdentity(t)
	provider := NewSoftwareProvider(identity)

	got := provider.Recipient()
	want := identity.Recipient().String()

	if got != want {
		t.Errorf("Recipient() = %q, want %q", got, want)
	}

	// Verify it's a valid age public key format
	if len(got) == 0 {
		t.Error("Recipient() returned empty string")
	}
	if got[:4] != "age1" {
		t.Errorf("Recipient() = %q, should start with 'age1'", got)
	}
}

func TestSoftwareProvider_Close(t *testing.T) {
	identity := testIdentity(t)
	provider := NewSoftwareProvider(identity)

	// Close should not error
	if err := provider.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// After close, identity should be nil (can't directly verify zeroization
	// due to age library limitations, but we can verify the pointer is cleared)
	if provider.identity != nil {
		t.Error("Close() should set identity to nil")
	}
}

func TestProviderModeConstants(t *testing.T) {
	tests := []struct {
		name     string
		constant string
		want     string
	}{
		{"ModeSoftware", ModeSoftware, "software"},
		{"ModeCache", ModeCache, "cache"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.constant != tt.want {
				t.Errorf("%s = %q, want %q", tt.name, tt.constant, tt.want)
			}
		})
	}
}
