//go:build linux || darwin

package agent

import (
	"bytes"
	"io"

	"filippo.io/age"
)

// SoftwareProvider implements Provider using an in-memory age X25519 identity.
// This is the default provider, compatible with CGO=0 builds.
type SoftwareProvider struct {
	identity *age.X25519Identity
}

// NewSoftwareProvider creates a new software provider with the given identity.
func NewSoftwareProvider(identity *age.X25519Identity) *SoftwareProvider {
	return &SoftwareProvider{
		identity: identity,
	}
}

// Mode returns "software".
func (p *SoftwareProvider) Mode() string {
	return ModeSoftware
}

// Decrypt decrypts age-encrypted ciphertext using the X25519 identity.
func (p *SoftwareProvider) Decrypt(ciphertext []byte) ([]byte, error) {
	r, err := age.Decrypt(bytes.NewReader(ciphertext), p.identity)
	if err != nil {
		return nil, err
	}
	return io.ReadAll(r)
}

// Recipient returns the age public key string for encryption.
func (p *SoftwareProvider) Recipient() string {
	return p.identity.Recipient().String()
}

// Close zeroizes the identity. After calling Close, the provider cannot be used.
//
// Note: Go's age library doesn't expose the raw key bytes for zeroing, so we
// set the reference to nil to allow garbage collection. The actual memory
// zeroing happens when the identity was created from memguard-protected memory
// in the spawn process.
func (p *SoftwareProvider) Close() error {
	p.identity = nil
	return nil
}

// Ensure SoftwareProvider implements Provider at compile time.
var _ Provider = (*SoftwareProvider)(nil)
