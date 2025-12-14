//go:build !hardware

package agent

import (
	"crypto/ecdsa"
	"errors"
)

// ErrHardwareNotCompiled is returned when hardware provider functions are called
// but the binary was not built with the "hardware" build tag.
var ErrHardwareNotCompiled = errors.New("hardware support not compiled; build with -tags hardware")

// NewPIVProvider returns an error indicating hardware support is not compiled.
// To use PIV/YubiKey hardware tokens, build with: go build -tags hardware
func NewPIVProvider(configDir string, pinPrompt func() (string, error)) (*PIVProvider, error) {
	return nil, ErrHardwareNotCompiled
}

// NewFIDO2Provider returns an error indicating hardware support is not compiled.
// To use FIDO2/WebAuthn hardware keys, build with: go build -tags hardware
func NewFIDO2Provider() (Provider, error) {
	return nil, ErrHardwareNotCompiled
}

// PIVKeystore is the v2 multi-key format for piv.json.
type PIVKeystore struct {
	Version int      `json:"version"`
	Keys    []PIVKey `json:"keys"`
}

// PIVKey represents a single enrolled YubiKey.
type PIVKey struct {
	Serial    uint32 `json:"serial"`
	SlotKey   uint32 `json:"slot_key"`
	PublicKey []byte `json:"public_key"`
	Label     string `json:"label,omitempty"`
	Enrolled  string `json:"enrolled,omitempty"`
	Identity  []byte `json:"identity"`
}

// PIVMeta is the v1 single-key format (for migration).
// Defined in stub for type compatibility with init code.
type PIVMeta struct {
	Serial    uint32 `json:"serial"`
	SlotKey   uint32 `json:"slot_key"`
	PublicKey []byte `json:"public_key"`
}

// PIVProvider stub for type compatibility.
type PIVProvider struct{}

// Mode returns "piv" (stub implementation).
func (p *PIVProvider) Mode() string { return ModePIV }

// Decrypt returns an error (stub implementation).
func (p *PIVProvider) Decrypt(ciphertext []byte) ([]byte, error) {
	return nil, ErrHardwareNotCompiled
}

// Recipient returns empty string (stub implementation).
func (p *PIVProvider) Recipient() string { return "" }

// Close is a no-op (stub implementation).
func (p *PIVProvider) Close() error { return nil }

// EncryptWithPIV stub for non-hardware builds.
func EncryptWithPIV(pubKey *ecdsa.PublicKey, plaintext []byte) ([]byte, error) {
	return nil, ErrHardwareNotCompiled
}

// MarshalP256PublicKey stub for non-hardware builds.
func MarshalP256PublicKey(pub *ecdsa.PublicKey) []byte {
	return nil
}

// LoadPIVKeystore stub for non-hardware builds.
func LoadPIVKeystore(configDir string) (*PIVKeystore, error) {
	return nil, ErrHardwareNotCompiled
}

// SavePIVKeystore stub for non-hardware builds.
func SavePIVKeystore(configDir string, ks *PIVKeystore) error {
	return ErrHardwareNotCompiled
}

// SavePIVKeystoreToPath stub for non-hardware builds.
func SavePIVKeystoreToPath(path string, ks *PIVKeystore) error {
	return ErrHardwareNotCompiled
}

// AddPIVKey stub for non-hardware builds.
func AddPIVKey(configDir string, key PIVKey) error {
	return ErrHardwareNotCompiled
}

// RemovePIVKey stub for non-hardware builds.
func RemovePIVKey(configDir string, serial uint32) error {
	return ErrHardwareNotCompiled
}

// ListConnectedYubiKeys stub for non-hardware builds.
func ListConnectedYubiKeys() ([]uint32, error) {
	return nil, ErrHardwareNotCompiled
}
