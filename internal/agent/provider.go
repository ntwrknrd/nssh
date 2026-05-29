//go:build linux || darwin

package agent

import "github.com/ntwrknrd/nssh/internal/session/mode"

// Provider abstracts cryptographic operations for different security modes.
// The agent delegates decryption to a Provider.
//
// Implementations:
//   - softwareProvider: Default, holds age X25519 identity in memory
type Provider interface {
	// Mode returns the security mode identifier.
	// Example: "software"
	Mode() string

	// Decrypt decrypts age-encrypted ciphertext using the provider's key.
	// Returns the plaintext on success.
	Decrypt(ciphertext []byte) ([]byte, error)

	// Recipient returns the age-compatible public key string for encryption.
	// This is used when encrypting new credentials for the vault.
	Recipient() string

	// Close zeroizes secrets and releases any resources held by the provider.
	// Must be called when the agent shuts down.
	Close() error
}

// ProviderMode constants for Provider.Mode() return values.
const (
	ModeSoftware = string(mode.Software)
	ModeCache    = "cache"
)
