//go:build hardware

package agent

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"testing"
)

// mockECDH implements ECDHSharedKey for testing without hardware.
type mockECDH struct {
	privateKey *ecdsa.PrivateKey
}

func (m *mockECDH) SharedKey(peer *ecdsa.PublicKey) ([]byte, error) {
	// Perform ECDH: shared = privateKey * peer.PublicKey
	sharedX, _ := peer.Curve.ScalarMult(peer.X, peer.Y, m.privateKey.D.Bytes())
	return sharedX.Bytes(), nil
}

func TestECIESRoundTrip(t *testing.T) {
	// Generate a P-256 key pair for testing
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	plaintext := []byte("AGE-SECRET-KEY-1QQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQ")

	// Encrypt with the public key
	ciphertext, err := EncryptWithPIV(&privKey.PublicKey, plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	// Verify ciphertext is larger than plaintext (ephemeral key + nonce + tag overhead)
	if len(ciphertext) <= len(plaintext) {
		t.Errorf("ciphertext length %d should be > plaintext length %d", len(ciphertext), len(plaintext))
	}

	// Decrypt with mock ECDH using the private key
	mock := &mockECDH{privateKey: privKey}
	decrypted, err := DecryptWithPIV(mock, ciphertext)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	if string(decrypted) != string(plaintext) {
		t.Errorf("decrypted = %q, want %q", decrypted, plaintext)
	}
}

func TestECIESEmptyPlaintext(t *testing.T) {
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	plaintext := []byte{}

	ciphertext, err := EncryptWithPIV(&privKey.PublicKey, plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	mock := &mockECDH{privateKey: privKey}
	decrypted, err := DecryptWithPIV(mock, ciphertext)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	if len(decrypted) != 0 {
		t.Errorf("decrypted length = %d, want 0", len(decrypted))
	}
}

func TestECIESInvalidCiphertext(t *testing.T) {
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	mock := &mockECDH{privateKey: privKey}

	tests := []struct {
		name string
		data []byte
	}{
		{"empty", []byte{}},
		{"too short", []byte("short")},
		{"truncated ephemeral key", make([]byte, 64)}, // needs 65 bytes
		{"missing nonce", make([]byte, 65)},           // needs nonce + ciphertext
		{"missing ciphertext", make([]byte, 65+12)},   // needs at least tag (16 bytes)
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DecryptWithPIV(mock, tc.data)
			if err == nil {
				t.Error("expected error for invalid ciphertext")
			}
		})
	}
}

func TestECIESInvalidEphemeralKey(t *testing.T) {
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	mock := &mockECDH{privateKey: privKey}

	// Create data with invalid ephemeral key (all zeros won't be on curve)
	data := make([]byte, 65+12+32)

	_, err = DecryptWithPIV(mock, data)
	if err == nil {
		t.Error("expected error for invalid ephemeral public key")
	}
}

func TestECIESWrongKey(t *testing.T) {
	// Generate two different key pairs
	privKey1, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key1: %v", err)
	}
	privKey2, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key2: %v", err)
	}

	plaintext := []byte("secret message")

	// Encrypt with key1's public key
	ciphertext, err := EncryptWithPIV(&privKey1.PublicKey, plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	// Try to decrypt with key2's private key
	mock := &mockECDH{privateKey: privKey2}
	_, err = DecryptWithPIV(mock, ciphertext)
	if err == nil {
		t.Error("expected error when decrypting with wrong key")
	}
}

func TestECIESCorruptedCiphertext(t *testing.T) {
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	plaintext := []byte("secret message")
	ciphertext, err := EncryptWithPIV(&privKey.PublicKey, plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	// Corrupt the ciphertext portion (after ephemeral key and nonce)
	if len(ciphertext) > 65+12+1 {
		ciphertext[65+12+1] ^= 0xFF
	}

	mock := &mockECDH{privateKey: privKey}
	_, err = DecryptWithPIV(mock, ciphertext)
	if err == nil {
		t.Error("expected error for corrupted ciphertext")
	}
}

func TestMarshalUnmarshalP256(t *testing.T) {
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	// Marshal the public key
	marshaled := MarshalP256PublicKey(&privKey.PublicKey)

	// Should be 65 bytes for uncompressed P-256 (0x04 || X || Y)
	if len(marshaled) != 65 {
		t.Errorf("marshaled length = %d, want 65", len(marshaled))
	}
	if marshaled[0] != 0x04 {
		t.Errorf("marshaled[0] = 0x%02x, want 0x04 (uncompressed)", marshaled[0])
	}

	// Unmarshal and verify
	recovered, err := UnmarshalP256PublicKey(marshaled)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if recovered.X.Cmp(privKey.PublicKey.X) != 0 || recovered.Y.Cmp(privKey.PublicKey.Y) != 0 {
		t.Error("recovered public key does not match original")
	}
}

func TestUnmarshalP256Invalid(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", []byte{}},
		{"too short", []byte{0x04, 0x01, 0x02}},
		{"wrong prefix", append([]byte{0x02}, make([]byte, 64)...)}, // compressed format
		{"invalid point", make([]byte, 65)},                         // all zeros
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := UnmarshalP256PublicKey(tc.data)
			if err == nil {
				t.Error("expected error for invalid data")
			}
		})
	}
}

func TestZeroize(t *testing.T) {
	data := []byte("sensitive data here")
	original := make([]byte, len(data))
	copy(original, data)

	zeroize(data)

	for i, b := range data {
		if b != 0 {
			t.Errorf("byte %d = %d, want 0", i, b)
		}
	}
}

func TestPIVProviderMode(t *testing.T) {
	// Create a minimal provider to test Mode()
	// Note: This doesn't actually connect to hardware
	p := &PIVProvider{}
	if got := p.Mode(); got != ModePIV {
		t.Errorf("Mode() = %q, want %q", got, ModePIV)
	}
}

func TestPIVAvailableHardware(t *testing.T) {
	// In hardware build, PIVAvailable should return true
	if !PIVAvailable() {
		t.Error("PIVAvailable() = false in hardware build, want true")
	}
}
