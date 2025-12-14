//go:build linux || darwin

package agent

import "github.com/ntwrknrd/nssh/internal/session/mode"

// Provider abstracts cryptographic operations for different security modes.
// The agent delegates decryption to a Provider, enabling software-only and
// hardware-backed security modes.
//
// Implementations:
//   - softwareProvider: Default, holds age X25519 identity in memory (CGO=0 compatible)
//   - pivProvider: PIV/YubiKey hardware token (requires hardware build tag + CGO)
//   - fido2Provider: FIDO2/WebAuthn hardware key (requires hardware build tag + CGO)
//   - secureEnclaveProvider: macOS Secure Enclave (requires secureenclave build tag + CGO)
type Provider interface {
	// Mode returns the security mode identifier.
	// Examples: "software", "piv", "fido2", "secureenclave"
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
	ModeSoftware      = string(mode.Software)
	ModePIV           = string(mode.PIV)
	ModeFIDO2         = string(mode.FIDO2)
	ModeSecureEnclave = string(mode.SecureEnclave)
)
